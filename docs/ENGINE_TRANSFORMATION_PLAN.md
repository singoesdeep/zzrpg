# zzrpg → Idle Online RPG Backend Engine — Master Architecture Plan

> **Status:** Architecture proposal (no code changed yet).
> **Author:** Principal Architect orchestration pass.
> **Scope:** Transform the current single-game monolith into a *plugin-first, event-driven, data-driven, transport/DB-independent* Idle Online RPG Backend Engine.
> **Rule followed:** Every claim below is grounded in the actual code with `file:line` references. No speculation about code that does not exist.

---

## 0. TL;DR

The codebase today is a **well-built single game**, not an engine. It has clean vertical slices (`handler/service/repository`), a working embedded Rust stat core, and graceful-degradation infra (Redis/Postgres). But every game *rule* is compiled Go, wiring is a 434-line hand-written `main.go`, the event bus knows exactly 2 events, and `combat` transitively depends on 6 other domains. To become an engine we must invert three things:

1. **Rules → Data.** Class stats, loot, formulas, mobs, offline gains are hardcoded → move to content packs + a formula DSL.
2. **Wiring → Plugin graph.** `main.go` manual DI → declarative plugin lifecycle with a service registry.
3. **Point-to-point calls → Events + Extension points.** `combat` calling `loot`/`quest`/`inventory` directly → publish domain events, resolve behavior through hooks.

---

## 1. Current Architecture Analysis

### 1.1 What exists (confirmed from code)

| Layer | Location | Notes |
|---|---|---|
| Entry / wiring | `cmd/server/main.go` (434 lines) | Manual DI, inline business logic, hardcoded WS switch |
| Domains | `internal/{auth,character,combat,inventory,items,loot,quests}` | Each = `handler.go` + `service.go` + `repository.go` |
| Transport | `internal/socket/*`, HTTP via `net/http` mux in `main.go` | WS + REST both bound directly to services |
| Stat core | `internal/statclient/*` → Rust `zzstat` via FFI | Clean adapter, has fallback path |
| Infra | `pkg/{cache,config,httpx,logger}`, `internal/database` | pgx pool, Redis, slog, middleware |
| Events | `internal/events/events.go` (~70 lines) | Global singleton bus, 2 event types |

### 1.2 Structural strengths (keep these)

- **Consumer-defined interfaces** exist in places (`CharacterService`, `LootRepository`, `cache.Cache`) — good DIP seed.
- **Graceful degradation** is idiomatic: Redis down → `cache.Noop{}` (`main.go:102`), statclient down → fallback formula (`character/service.go` step 4, `combat/combat.go` step 3).
- **Concurrency correctness is taken seriously**: per-character `keyedMutex` (`inventory/service.go`), `randMu` around `*rand.Rand` (`loot/service.go`), `DeductHPAndReserveKill` for exactly-once kill credit (`combat/combat.go` step 4).
- **Cache-aside done right**: `loot.NewCachedRepository` decorator (`main.go:113-114`).

### 1.3 Critical anti-patterns (must fix to become an engine)

| # | Problem | Evidence | Impact |
|---|---|---|---|
| A1 | **God `main.go`** — wiring + business rules + transport routing in one function | offline-gains math `main.go:161-196`; WS `switch` `main.go:124-265` | No extensibility; a plugin can't add a message type or a rule |
| A2 | **`combat` is a God service** coupling 6 domains | imports at `combat/combat.go:6-13`; kill→loot→quest→inventory chain in `ExecuteAttack` | Combat can't be swapped/extended; changing loot breaks combat |
| A3 | **Rules hardcoded, not data** | class stats `character/service.go:52-63`; dummy mob `9999/def40/hp1000` `combat/combat.go`; table IDs `"dummy_drops"/"player_drops"`; loot fallback `"dragon_sword_0"` `loot/service.go`; level-up `+2` | A game designer must edit Go + redeploy |
| A4 | **Event bus is a toy** | `internal/events/events.go` — 2 types, fire-and-forget goroutines, no ordering/persistence/replay/error handling, payload is `any` | Can't drive an idle economy or plugin reactions |
| A5 | **Circular domain dependency** | `charService.SetEquipmentProvider(invService)` `main.go:94`; setter mutates service post-construction | Symptom of wrong aggregate boundaries |
| A6 | **Transport & DB not abstracted** | pgx in every repo; WS/HTTP hand-wired in `main.go` | Not transport-/DB-independent as required |
| A7 | **Single-node assumptions** | `keyedMutex` (`inventory/service.go` comment), in-memory `SessionRegistry`, global event bus | Blocks horizontal scale to 100k players |

---

## 2. Engine Transformation Strategy

The transformation is an **inversion of control at three levels**, executed incrementally (Strangler-Fig — never a big-bang rewrite):

```
BEFORE:  main() → constructs → services → call each other directly → hardcoded rules
AFTER:   Kernel → loads Plugins → register into Registry → react to Events → resolve rules from Content/DSL
```

**Guiding boundary:** *Engine core owns mechanisms; plugins own policy; content packs own data.*

---

## 3. Target Folder Structure

```
backend/
  cmd/
    server/            # thin: build kernel, load plugin set, Run()
    enginectl/         # NEW CLI: scaffold plugin, validate content, migrate, run
  engine/              # NEW — the reusable engine (import path: .../backend/engine)
    kernel/            # lifecycle, plugin manager, ordered start/stop
    plugin/            # Plugin interface, context, capability & version types
    registry/          # typed service + content-type registry
    bus/               # event bus abstraction + in-proc & redis-stream impls
    content/           # content pack loader (json/yaml), schema validation
    script/            # formula DSL / expr evaluator
    transport/         # transport abstraction (http, ws) + router registration
    persistence/       # Store/UnitOfWork abstraction; pgx + memory adapters
    contracts/         # shared value objects & event payload structs (no logic)
  plugins/             # NEW — bundled "default game" plugins (were internal/*)
    auth/  character/  inventory/  items/  loot/  quests/  combat/  idle/
  content/             # NEW — data packs (the game, as data)
    classes/*.yaml  loot/*.yaml  mobs/*.yaml  quests/*.yaml  formulas/*.yaml
  internal/            # shrinks: only truly-private engine internals
  pkg/                 # keep: config, logger, httpx, cache (become engine deps)
```

`internal/*` domains are **not deleted** — they are *lifted* into `plugins/*` and refactored to implement the `Plugin` interface. Migration is slice-by-slice.

---

## 4. Engine Core (the Kernel)

Minimal, game-agnostic. Contains **zero** RPG concepts (no "character", no "loot").

**Kernel responsibilities:** config, structured logger, clock (`func() time.Time` — injectable for idle/tests), the `Registry`, the `EventBus`, the `Transport` set, ordered plugin lifecycle, graceful shutdown (reuse existing signal handling `main.go:418-433`).

```go
// engine/kernel
type Kernel struct {
    cfg      config.Config
    log      *slog.Logger
    clock    func() time.Time
    reg      registry.Registry
    bus      bus.EventBus
    plugins  []plugin.Plugin  // topologically sorted
}

func (k *Kernel) Register(p plugin.Plugin) *Kernel { /* collect */ }
func (k *Kernel) Run(ctx context.Context) error {
    ordered := topoSort(k.plugins)          // by declared dependencies (solves A5)
    for _, p := range ordered { p.Init(initCtx) }   // register services/content/routes
    resolveDeps(ordered)                    // now all services present in registry
    for _, p := range ordered { p.Start(runCtx) }   // background loops (hub, idle ticker)
    <-ctx.Done()
    for i := len(ordered)-1; i>=0; i-- { ordered[i].Stop(shutdownCtx) } // reverse order
}
```

**cmd/server/main.go becomes ~30 lines:** build kernel, `Register` the plugin set, `Run`.

---

## 5. Plugin System Design

### 5.1 The Plugin contract

```go
// engine/plugin
type Plugin interface {
    Meta() Meta                       // name, version, provided/required capabilities
    Init(ctx InitContext) error       // register services, content types, routes, subscriptions
    Start(ctx RunContext) error       // start background work (optional)
    Stop(ctx context.Context) error   // graceful teardown (optional)
}

type Meta struct {
    Name         string
    Version      semver.Version
    Requires     []Dependency          // {Name, VersionConstraint} — resolved before Init
    Provides     []Capability          // e.g. "loot.roller", "combat.resolver"
    Consumes     []Capability          // optional hooks it will look up
}

type InitContext interface {
    Registry() registry.Registry
    Bus()      bus.EventBus
    Content()  content.Loader
    Router()   transport.Router        // register HTTP/WS handlers
    Config()   config.Section          // plugin-scoped config namespace
    Logger()   *slog.Logger
}
```

### 5.2 Init/Start/Stop maps directly onto today's code

- `Init` = what `main.go:64-116` does per domain (construct repo+service, put in registry).
- `Start` = `go hub.Run()` (`main.go:119`), and the future idle ticker.
- `Stop` = the `defer` closers (`db.Close`, `statClient.Close`, cache close).

### 5.3 Hot-reload verdict

**Do NOT pursue Go `plugin` (.so) hot-reload.** Rationale: Go's `plugin` package requires identical build flags/versions, is Linux-fragile, cannot be unloaded, and interacts badly with the existing cgo/FFI `statclient`. Recommended model:

1. **Primary: compiled-in plugins** (interface-based, registered in `cmd/server`). Simple, type-safe, fast. This is what 95% of engine users need — they compose a binary from plugin packages.
2. **Hot-reloadable = content + scripts, not code.** Content packs (Section 8) and formula DSL (Section 8.4) *are* reloadable at runtime via a registry `Reload()` + file watcher. This gives designers live iteration without the `.so` hazards.
3. **Future/optional: out-of-process plugins** over the transport/event boundary (gRPC or Redis Streams) for untrusted or polyglot plugins — this is the real "sandbox" answer, not in-process sandboxing which Go can't safely provide.

---

## 6. Registry System

A typed runtime registry replaces manual DI *and* enables content extensibility.

```go
// engine/registry — service side (generic, Go 1.26 generics)
func Provide[T any](r Registry, name string, svc T)
func Resolve[T any](r Registry, name string) (T, error)   // typed lookup, collision-safe

// content-type side: plugins register *kinds* of content + their loaders/handlers
type ContentType struct {
    Kind      string                        // "loot_table", "class", "mob", "quest"
    Schema    json.RawMessage               // for validation
    Register  func(id string, raw []byte) error
}
func (r Registry) DefineContentType(ct ContentType)
func (r Registry) Lookup(kind, id string) (any, bool)      // e.g. Lookup("loot_table","dummy_drops")
```

**Immediately kills these hardcodes:** `"dummy_drops"`/`"player_drops"` (`combat/combat.go`), class switch (`character/service.go:52-63`), `9999` dummy mob — all become `registry.Lookup(kind, id)`.

Collision handling: last-writer-wins is rejected; `Provide` on an existing name returns an error surfaced at kernel boot (fail fast).

---

## 7. Event System Redesign

### 7.1 Gaps in current bus (`internal/events/events.go`)

- Only `EventItemEquipped` / `EventItemUnequipped` exist.
- `Publish` spawns `go func` per handler → no ordering, no backpressure, no at-least-once, panics only logged (`events.go` recover).
- Payloads are `any`, type-asserted at subscriber (`main.go:269`) → fragile.
- No persistence → **cannot replay for idle/offline catch-up**, which is the heart of an idle game.

### 7.2 Target design

```go
// engine/bus
type Event interface { Name() string }          // typed events, not `any`
type EventBus interface {
    Publish(ctx context.Context, ev Event) error
    Subscribe(name string, h Handler) Subscription
}
```

Two implementations behind one interface:
- **In-proc** (dev/single-node): improved version of today's bus + synchronous option for ordering-sensitive reactions.
- **Redis Streams** (production/multi-node): durable, consumer groups, replayable, at-least-once — reuses the Redis already wired (`main.go:103`).

Add a **transactional outbox** in the persistence layer so state change + event emit are atomic (no lost loot/quest events on crash).

### 7.3 Domain event catalog (extracted from actual state changes)

| Event | Producer (today) | Consumers |
|---|---|---|
| `CharacterCreated` | `character/service.go Create` | quests (starter), analytics |
| `ItemEquipped` / `ItemUnequipped` | `inventory/service.go MoveItem` | character (recalc stats — `main.go:268-288`) |
| `MobKilled` / `PlayerKilled` | `combat/combat.go` (killedNow) | loot, quests, achievements |
| `LootDropped` | `loot` roll in combat/offline | inventory, notifications |
| `QuestProgressed` / `QuestCompleted` | `quests/service.go` | rewards, character |
| `CharacterLeveledUp` | `character/service.go AddRewards` | stat recalc, notifications |
| `GoldChanged` / `ExpGained` | `AddRewards` | idle economy, analytics |
| `CharacterLoggedIn` / `LoggedOut` | `main.go` SELECT_CHARACTER / disconnect | idle catch-up trigger |
| `OfflineGainsGranted` | `main.go:198-215` | notifications |
| `CombatDamage` | `combat` | transport broadcast (`main.go:259-263`) |

**Taxonomy:** Commands (`ExecuteAttack`, `MoveItem`) and Queries (`GetInventory`) stay direct calls into services; **Domain Events** (above) become the async reaction backbone; **Integration Events** (e.g. cross-node) ride Redis Streams. Sync events = stat recalc (must finish before next read); async = loot/quest/analytics.

---

## 8. Data-Driven Systems

### 8.1 Decision matrix

| System | Verdict | Format | Rationale |
|---|---|---|---|
| Stat, Class base stats | **Data** | YAML | Hardcoded `character/service.go:52-63` today |
| Formula (damage, offline, level curve) | **Data + DSL** | DSL exprs in YAML | Designer-editable math; see 8.4 |
| Loot | **Data** | YAML (already DB-JSONB) | Kill `"dummy_drops"` hardcode |
| Item | **Data** (already) | DB JSONB / YAML | `item_definitions` already data-driven ✔ |
| NPC / Mob | **Data** | YAML | Dummy `9999` hardcoded in `combat` |
| Quest | **Data** | YAML | Objectives already dynamic; externalize triggers |
| Skill, Talent, Passive, Upgrade | **Data + DSL** | YAML + formula refs | Effects = modifier lists + expr |
| Currency, Craft/Recipe, Building, Pet, Guild, Achievement | **Data** | YAML | Pure content, register at load |
| Dungeon, WorldEvent | **Data + registry** | YAML + scheduler plugin | Content + timed triggers |
| AI | **Data (behavior tree) + plugin** | YAML BT, Go executor | Simple idle AI = data; complex = plugin |
| Combat, Idle/Offline progression | **Plugin + DSL** | Go plugin, DSL formulas | Mechanism in code, numbers in data |

### 8.2 Hardcoded rules to externalize (with source)

| Hardcode | Location | Target |
|---|---|---|
| Class base stats | `character/service.go:52-63` | `content/classes/*.yaml` |
| Offline gold/exp formula | `main.go:161-165` | `content/formulas/offline.yaml` (DSL) |
| Offline loot rate/caps (`0.50`, cap 10, 86400) | `main.go:154-196` | `content/formulas/offline.yaml` |
| Dummy mob (`9999`, def 40, HP 1000, lvl 10) | `combat/combat.go` | `content/mobs/dummy.yaml` |
| Loot table IDs `dummy_drops`/`player_drops` | `combat/combat.go`, `loot/service.go` | `content/loot/*.yaml` + registry |
| Loot fallback item `dragon_sword_0` | `loot/service.go RollLoot` | remove; content-only |
| Level-up `+2` base stats | `character` level logic | `content/formulas/leveling.yaml` |
| WS message types (`CHAT`/`SELECT_CHARACTER`/`COMBAT_ATTACK`) | `main.go:124-265` | plugin-registered message handlers |

### 8.3 Loader

`engine/content` walks `content/`, validates each file against the `ContentType.Schema` registered by the owning plugin, and calls its `Register`. Fail-fast on schema errors at boot; `Reload()` for live iteration (Section 5.3).

### 8.4 Formula / scripting recommendation

**Recommend a small embedded expression evaluator (e.g. an `expr`-style DSL), NOT Lua**, for v1. Rationale: designer formulas here are pure arithmetic over named stats (`10 + STR*0.5`, damage curves) — an expression DSL is sandboxed-by-construction, allocation-light, trivially testable, and has no GIL/embedding overhead. Reserve a Lua/WASM plugin for a later version *if* full behavioral scripting (custom AI, event scripts) is demanded. Formula example:

```yaml
# content/formulas/offline.yaml
gold_per_min: "10 + STR * 0.5"
exp_per_min:  "15 + INT * 0.8"
loot_roll_chance: 0.50
max_hours: 24
```

---

## 9. Extension Points (Hooks)

Map every current hardcoded decision to a named hook resolved via registry:

| Hook | Replaces | Signature (sketch) |
|---|---|---|
| `combat.DamageResolver` | Rust call + Go fallback in `combat/combat.go` | `Resolve(ctx, Attacker, Defender, Skill) DamageResult` |
| `combat.OnKill` | inline loot/quest chain in `ExecuteAttack` | event `MobKilled` → subscribers |
| `loot.Roller` | `loot/service.go RollLoot` | `Roll(ctx, tableID) []Drop` |
| `reward.Calculator` | `AddRewards` gold/exp math | DSL-driven |
| `idle.GainsCalculator` | `main.go:161-196` | `Compute(elapsed, stats) Gains` |
| `character.StatRecalc` | `RecalculateStats` | already a service — expose as hook |
| `transport.MessageHandler` | WS `switch` `main.go:124-265` | plugins register `(type → handler)` |

---

## 10. Public API Design

Stable surface a plugin author programs against (the *only* thing they import from `engine/`):

```go
// what a plugin author touches
engine/plugin   → Plugin, Meta, InitContext, RunContext
engine/registry → Provide/Resolve/DefineContentType/Lookup
engine/bus      → Event, EventBus, Handler, Subscription
engine/content  → Loader, ContentType, Schema helpers
engine/transport→ Router (RegisterHTTP, RegisterMessage), Context
engine/contracts→ shared value objects & event payloads
```

Versioned with semver; `engine/contracts` is the compatibility contract. Internals (`kernel`, adapters) are **not** part of the public API.

---

## 11. Production Architecture (100k+ concurrent)

| Concern | Today | Target |
|---|---|---|
| Session state | in-mem `SessionRegistry` | Redis-backed, sharded by character ID |
| Per-char locks | `keyedMutex` single-node (`inventory/service.go`) | pg advisory lock / `SELECT … FOR UPDATE` (already noted in code comment) |
| Event fan-out | in-proc goroutines | Redis Streams consumer groups |
| Loot RNG | global `randMu` (`loot/service.go`) | per-goroutine `rand.Rand` or `math/rand/v2` (no lock) |
| Idle ticks | none (computed on login) | dedicated scheduler/worker plugin, batched |
| Transport | single process WS hub | horizontal WS nodes + shared pub/sub for broadcast |
| DB | single pgx pool | read replicas for catalog, outbox for events |
| Stateless app | mostly, except registries | make app nodes stateless; state in Redis/PG |

Deploy shape: N stateless engine nodes (behind LB), Redis (streams + cache + sessions), Postgres (primary + replicas), optional dedicated idle-worker deployment running the same binary with only idle/scheduler plugins enabled.

---

## 12. Refactoring Backlog (Critical → Low)

**Critical (unblocks everything):**
- C1. Introduce `engine/kernel` + `Plugin` interface; convert `main.go` wiring into `Init` calls (keep behavior identical). Solves A1 first step.
- C2. Add typed `registry`; move service construction into it. Solves A5 by declaring deps, not setters.
- C3. Replace the 2-event bus with the `EventBus` interface + in-proc impl (behavior-compatible). Solves A4 foundation.

**High:**
- H1. Extract content loader; move class stats (`character/service.go:52-63`) and loot tables to `content/`. 
- H2. Break `combat` God service: emit `MobKilled` instead of calling loot/quest/inventory directly (A2).
- H3. Formula DSL; externalize offline-gains (`main.go:161-196`) and leveling.
- H4. Register WS message handlers via plugins (remove `main.go` switch).

**Medium:**
- M1. Persistence abstraction (`Store`/`UnitOfWork`) over pgx (A6, DB-independence).
- M2. Transport abstraction (HTTP+WS behind `Router`).
- M3. Redis-Streams event impl + transactional outbox.

**Low:**
- L1. `enginectl` CLI (scaffold plugin, validate content).
- L2. `math/rand/v2` to drop `randMu`.
- L3. Move remaining hardcodes (dummy mob, fallback item).

---

## 13. Roadmap

- **MVP (Engine v0.1):** C1–C3 + H1. Same game runs, but wired as plugins over a kernel, class stats + loot are data. Proves the model.
- **v1 (Engine v1.0):** H2–H4 + M1–M2 + DSL. `combat` decoupled via events; transport/DB abstracted; a developer can add a stat/loot/mob/message with zero core edits. First "build your own idle RPG" milestone.
- **v2:** M3 (Redis Streams + outbox) + production scale-out (Section 11) + `enginectl` + content hot-reload.
- **v3:** Out-of-process/polyglot plugins (gRPC boundary), behavior-tree AI, Lua/WASM scripting option, marketplace-style content packs.

---

## 14. Risk Analysis

| Risk | Severity | Mitigation |
|---|---|---|
| Big-bang rewrite stalls | High | Strangler-fig: kernel wraps existing services; migrate one slice at a time, tests green each step (`tests/integration_test.go`, 729 lines, is the safety net) |
| FFI `statclient` + plugin model friction | Med | Keep statclient as a compiled-in plugin; never `.so`-reload it |
| Event ordering bugs when moving from direct calls to async | Med | Classify sync vs async explicitly (Section 7.3); keep stat-recalc sync |
| DSL scope creep → reinventing Lua | Med | Freeze DSL to arithmetic for v1; gate scripting to v3 |
| Redis becomes SPOF at scale | Med | Cluster + graceful degradation already a codebase habit (`cache.Noop`) |
| Losing correctness guarantees (exactly-once kill, per-char locks) during refactor | High | Port `DeductHPAndReserveKill` and locking semantics verbatim into the new hooks; add regression tests before moving them |

---

## 15. Technical Debt Analysis

| Debt | Location | Interest (why it hurts) |
|---|---|---|
| Business logic in `main.go` | `main.go:124-265`, `:161-215` | Untestable, un-extensible; blocks plugin model |
| `combat` fan-out coupling | `combat/combat.go:6-13` | Any content change risks combat; hard to test in isolation |
| Post-construction setter DI | `main.go:94` (`SetEquipmentProvider`) | Hidden temporal coupling; mutable service graph |
| `any`-typed event payloads | `events.go`, `main.go:269` | Runtime type asserts, no compile safety |
| Single-node state | `SessionRegistry`, `keyedMutex` | Correct today, wrong at horizontal scale (comments already flag it) |
| Hardcoded magic values | class stats, `9999`, `dummy_drops`, `dragon_sword_0`, `+2`, `86400` | Every balance change = code change + redeploy |
| No idle scheduler | offline gains only computed on login (`main.go`) | Not a true "always-on" idle economy |

---

## Appendix — Evidence index (key file:line references)

- Manual DI + inline rules: `cmd/server/main.go:36-434` (offline `:161-196`, WS switch `:124-265`, event subs `:268-288`, circular-dep setter `:94`).
- Toy event bus: `internal/events/events.go` (whole file, 2 event types).
- Combat God service: `internal/combat/combat.go` (imports `:6-13`; dummy `9999`; table IDs; kill chain).
- Hardcoded class stats: `internal/character/service.go:52-63`; fallback stat path in `RecalculateStats`.
- Inventory single-node lock + equip rules: `internal/inventory/service.go` (`keyedMutex`, `validateEquipmentRequirements`).
- Loot RNG lock + hardcoded fallback: `internal/loot/service.go` (`randMu`, `dummy_drops`, `dragon_sword_0`).
- Cache-aside decorator (good pattern to reuse): `main.go:113-114`, `internal/loot/` cached repository.
