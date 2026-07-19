# zzrpg → Idle Online RPG Backend Engine — Master Architecture Plan

> **Status:** Architecture proposal (no code changed yet).
> **Author:** Principal Architect synthesis of three parallel expert-panel reviews — (1) Idiomatic Go / Architecture / DDD, (2) Engine Core / Plugin System, (3) Event-Driven / Data-Driven. All findings reconciled into one plan; conflicts resolved in favor of the incremental (Strangler-Fig) path.
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
- **A data-driven formula engine already exists**: the embedded `zzstat` core is JSON-native — a phase/rule stat-transform pipeline plus `EvaluateCombat(formulaJSON)` (`zzstat/bindings/go/zzstat.go:153-293`). The engine does **not** need to build a formula DSL; it needs to *feed* zzstat from JSON content (see §8.4). This materially shrinks the data-driven work.

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
| A8 | **Gameplay state trapped in the transport layer** | `socket.SessionRegistry` is the source of truth for HP/MP/death (`socket/session.go:21-27`, `var globalRegistry`), while Postgres holds level/gold/derived stats | Two systems of record diverge on restart; the missing "Encounter/CombatSession" aggregate lives in `socket`, not a domain |
| A9 | **`Modifier` concept duplicated 3×** | `character.EquipmentModifier` (`character.go:53-59`), `statclient.Modifier` (`client.go:23-29`), `items.StatModifier` — same concept, 3 shapes, manual field-by-field translation at every boundary | No shared kernel; every new content type (skills/buffs/auras) re-implements it |
| A10 | **Inconsistent ID types** | `character` uses `int64`, `inventory`/`combat`/`quests` use `int32` → casts at nearly every cross-package call (`combat.go:96,131,193,207`) | Systemic modeling bug; blocks a proper `CharacterID` value type |
| A11 | **Anemic domain model + logic in repositories** | `Character` is a data struct with no behavior; leveling *orchestration* runs inside a SQL tx in `character/repository.go:195-265`; reward math in `main.go:161-164` and `combat.go` | Domain layer not independently testable/reusable — fatal for an "engine" |
| A12 | **Process-global singletons** | `events.globalBus` (`events.go:28-30`), `socket.globalRegistry` (`session.go:21-23`) | Prevents multiple isolated game-worlds / clean tests / sharding |
| A13 | **Silent economy data loss** | Swallowed errors on reward/inventory writes: `main.go:190`, `combat.go:207,216`, `quests/service.go:135,146` — no log, no retry | Players silently short-changed in production; unacceptable for an engine |

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
    Requires     []Dependency          // HARD deps — absence is fatal; resolved before Init
    Optional     []Dependency          // SOFT deps — influence load order only, may be absent
    Provides     []Capability          // e.g. "stat.resolver", "equipment.modifiers"
    Consumes     []Capability          // capabilities/hooks it will look up
}
// Dependency.Capability=true → depends on ANY plugin providing that capability
// at a compatible semver range, not a named plugin. (Lets `combat` require
// capability:stat.resolver, satisfied by statclient OR a mock OR a pure-Go impl.)

// InitContext (a.k.a. RegistrationContext) is the plugin's SOLE channel to the
// engine during Init. It is where main.go's imperative wiring (main.go:64-122)
// and hook subscriptions (main.go:268-288) become declarative registrations.
type InitContext interface {
    Logger() *slog.Logger              // child logger tagged with plugin name
    Config() config.Section            // namespaced: cfg.Sub("combat")
    Clock()  Clock                     // injectable time source — replaces time.Now() @ main.go:153,173

    Registry() registry.Registry
    ProvideService(name string, svc any)
    RequireService(name string, into any) error      // typed resolve into a *pointer
    ProvideCapability(c Capability, impl any)
    RequireCapability(name string, into any) error    // LAZY-resolved accessor (see circular-dep fix)

    Subscribe(evt string, h bus.Handler)              // replaces events.Global().Subscribe @ main.go:268
    RegisterHook(point string, priority int, fn any) error
    RegisterMessageHandler(msgType string, h transport.MessageHandler) // replaces WS switch @ main.go:124-265
    RegisterHTTPRoute(method, pattern string, h http.Handler)          // replaces mux.Handle @ main.go:330+
    RegisterMigrations(fs fs.FS)                       // plugin owns its schema
    RegisterContentType(ct content.ContentTypeDescriptor)
}
```

**cmd/server/main.go collapses to ~15 lines** — build engine, `Register(...)` the plugin set, `Run(ctx)`; the engine resolves the graph, runs Init/Start in topological order, blocks on signal, and Stops in reverse order (absorbing the scattered `defer` closes at `main.go:55,74-78,108`).

### 5.4 Solving the circular dependency (`charService ↔ invService`)

Today: `charService` built with `nil` provider (`main.go:83`), then patched via `charService.SetEquipmentProvider(invService)` (`main.go:94`). The cycle is *construction-time only* — at the interface level, `character` needs a narrow `EquipmentProvider` (`GetEquippedModifiers`), `inventory` needs `CharacterService`. Three engine-native fixes, best-last:

- **(C) Mediator slot [ship first — behavior-identical]:** kernel owns an `EquipmentProvider` registry slot; `inventory` fills it in `Start`, `character` reads it in `Start`. Mechanically like today's setter but the *engine* owns timing, not `main`. Deletes the smell from `main.go`.
- **(B) Event-mediated:** `character` never calls `inventory`; it subscribes to `ItemEquipped` and `inventory` pushes modifier snapshots in the payload. Removes the read-back edge; heavier `RecalculateStats` refactor.
- **(A) Capability lazy-binding [target]:** `character` holds no provider field; during recalc it resolves `RequireCapability("equipment.modifiers")` — an accessor bound at first use (post-`Start`), so no construction edge exists. `SetEquipmentProvider` is deleted. Because `Init` forbids cross-plugin *calls* (registration only), the cycle can never re-form.

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

**Collision handling — two policies.** *Service/content-type* names must be unique: `Provide` on an existing name is a hard error surfaced at kernel boot, naming both contending plugins (fail fast). *Capabilities*, by contrast, are **allowed** to have multiple providers — that is their purpose — resolved by policy: (1) explicit config pin (`capabilities.stat.resolver = "statclient"`), else (2) highest semver, else (3) error if ambiguous with no default. This generalizes the existing `statClient != nil ? real : fallback` branch (`character/service.go:120-128`, `combat.go:163`) into a first-class, deterministic mechanism.

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

### 7.3 Complete domain event catalog

The current bus covers only **2 of ~18** real domain events; the other 16 happen today via direct synchronous calls (this is what couples `combat → loot → inventory → character`). **(NEW)** = emits nothing today.

| Event | Payload | Producer (file:line) | Consumers |
|---|---|---|---|
| `ItemEquipped` / `ItemUnequipped` | CharacterID, Item | `inventory/service.go:175-194` | stat recalc `main.go:268-288` |
| `CharacterLoggedIn` **(NEW)** | CharacterID, LastActiveAt | `main.go:140-150` | session start; offline-gains trigger; presence |
| `CharacterLoggedOut` **(NEW)** | CharacterID | `main.go:366-371` | `UpdateLastActive` + `EndSession` |
| `OfflineGainsGranted` **(NEW)** | CharacterID, Elapsed, Gold, Exp, LeveledUp, Loot | `main.go:199-214` | client packet; event-sourced replay |
| `CombatAttackResolved` **(NEW)** | Attacker/Defender, Damage, IsCrit, HP, IsDead | `combat.go:224-234` | client `COMBAT_DAMAGE`; analytics; achievements |
| `CharacterDamaged` **(NEW)** | CharacterID, Amount, NewHP, IsDead | `session.go:66-88` | threat/aggro; on-damage passives; UI |
| `MobKilled` / `PlayerKilled` **(NEW)** | KillerID, VictimID, VictimType, LootTableID | `combat.go:177-197` (`killedNow`) | loot roll; quest progress; achievements; death penalty |
| `LootDropped` **(NEW)** | CharacterID, TableID, Items[] | `combat.go:201`, `loot/service.go:40` | inventory add; gold add; UI |
| `ItemAddedToInventory` **(NEW)** | CharacterID, ItemDefID, Qty, Slot | `inventory/service.go:83-113` | collect-item quests; achievements |
| `GoldChanged` **(NEW)** | CharacterID, Delta, NewGold | `character/repository.go:214-223` | currency ledger; achievements; anti-cheat audit |
| `ExperienceGained` **(NEW)** | CharacterID, Amount | `character/repository.go:216` | progression UI; analytics |
| `CharacterLeveledUp` **(NEW)** | CharacterID, Old/NewLevel, StatGains | `character/repository.go:216-257` | stat recalc; unlock talents/skills; broadcast |
| `StatsRecalculated` **(NEW)** | CharacterID, DerivedStats | `character/service.go:132` | session HP/MP refresh; UI |
| `QuestAccepted` / `QuestProgressed` / `QuestCompleted` **(NEW)** | CharacterID, QuestID, Step/Rewards | `quests/service.go:82,120,128-147` | reward grant; achievements; follow-up unlock |
| `RewardsGranted` **(NEW)** | CharacterID, Gold, Exp, Source | `character/service.go:135-147` | currency ledger; leveling; UI |

### 7.4 Command / Query / Event taxonomy + sync-vs-async rule

- **Commands** (`ExecuteAttack` `combat.go:73`, `MoveItem`, `AcceptQuest`, `AddRewards`) → **sync**, transactional, stay direct calls / HTTP handlers.
- **Queries** (`GetByID`, `GetInventory`, `ListLootTables`) → **sync**, cacheable (loot already read-through cached `main.go:113-114`).
- **Domain Events** (table above) → **async** for reactions (achievements, analytics, follow-on quests); **sync/transactional** only where correctness demands it.
- **Integration Events** (WS packets `COMBAT_DAMAGE`/`OFFLINE_GAINS`, FFI to Rust) → async outbound fan-out; FFI is sync (C ABI).

**The rule:** anything that must be atomic with a state write (loot/gold grant on kill, quest reward, gating stat recalc) is **sync-by-outbox** — written to the outbox in the *same DB transaction*, dispatched after commit. Everything else (projections, achievements, presence, client notifications) is **async post-commit**. Combat already hand-rolls exactly-once via `DeductHPAndReserveKill`/`killedNow` (`session.go:66-88`, `combat.go:177-189`) — the event layer *generalizes* that idempotency (keyed on `EventID`) instead of re-implementing it per feature.

### 7.5 Transactional outbox + replay (typed)

Fixes the crash-window where `inventory/service.go` publishes *after* the DB write (a crash drops the recalculation), and combat's 4-5 non-atomic writes (`combat.go:199-221`).

1. Command handler writes the state change **and** the event row in one tx (e.g. the `AddRewards` tx at `character/repository.go:196-259` also inserts `CharacterLeveledUp`).
2. A relay polls the `outbox` table (or Postgres `LISTEN/NOTIFY`) → dispatcher.
3. Dispatcher fans out to in-proc handlers **and** Redis Streams (already wired via `pkg/cache`/`main.go:100-109`) with consumer groups + `XACK` (at-least-once) + a `-dead` stream (DLQ), replacing the recover-and-drop at `events.go:61-64`.
4. **Replay for idle catch-up:** persist events in an append-only `event_log`; on `CharacterLoggedIn`, replay/aggregate missed ticks from `LastActiveAt` (`main.go:153`) instead of the inline offline formula loop (`main.go:160-196`).

```go
type DomainEvent interface{ EventType() EventType }   // typed — replaces `Payload any` @ events.go:18

type Envelope struct {
    ID         EventID       // idempotency key
    Type       EventType
    Stream     StreamID      // ordering key, e.g. "character:42"
    Sequence   uint64        // monotonic within Stream (ExpGained→LeveledUp→Recalc never reorder)
    OccurredAt time.Time
    Payload    DomainEvent
}
type Outbox interface {
    Append(ctx context.Context, tx pgx.Tx, ev DomainEvent, s StreamID) error // enlisted in caller's tx
}
type Subscriber interface {
    On(t EventType, h func(ctx context.Context, e Envelope) error) // Handler returns error → retry/DLQ
}
type Dispatcher interface {
    Publish(ctx context.Context, e Envelope) error
    Replay(ctx context.Context, s StreamID, since uint64) ([]Envelope, error) // idle catch-up
}
```

Vs today's `events.go`: `Handler` returns `error` (retry/DLQ vs the swallow at `events.go:66`); payloads typed; per-`Stream` ordering; durable in-tx `Append`; `Replay`.

---

## 8. Data-Driven Systems

### 8.1 Decision matrix

| System | Verdict | Format | Rationale |
|---|---|---|---|
| Stat (derived) | **Data → zzstat** | JSON transforms | zzstat pipeline already supports it; coeffs just hardcoded in glue (`client.go:173-183`) |
| Class base stats | **Data** | YAML | Hardcoded `character/service.go:52-63` today |
| Formula (damage/stat) | **Data → zzstat** | JSON (`EvaluateCombat` + transforms) | zzstat is the JSON formula engine; §8.4 |
| Formula (economy: offline, XP curve) | **Data (+ expr)** | JSON constants / `expr` | Outside zzstat's stat/combat domain |
| Loot | **Data** (already) | DB JSONB | Only table IDs hardcoded at call sites |
| Item | **Data** (already) | DB JSONB / YAML | `item_definitions` already data-driven ✔ |
| NPC / Mob | **Data** | YAML | Dummy `9999` hardcoded in `combat` |
| Quest | **Data** (already) | DB JSONB | Objectives dynamic; externalize triggers/tags |
| Skill, Talent, Passive, Upgrade | **Data → zzstat modifiers** | YAML + JSON transforms | Effects = zzstat modifier lists (reuse `Modifier` shape) |
| Currency, Craft/Recipe, Building, Pet, Guild, Achievement | **Data** | YAML | Pure content, register at load |
| Dungeon, WorldEvent | **Data + registry** | YAML + scheduler plugin | Content + timed triggers |
| AI | **Data (behavior tree) + plugin** | YAML BT, Go executor | Simple idle AI = data; complex = plugin |
| Combat, Idle/Offline progression | **Plugin + Data** | Go plugin; zzstat/expr formulas | Mechanism in code, numbers in data |

### 8.2 Hardcoded rules to externalize (with source)

| # | Hardcode | Location | Target |
|---|---|---|---|
| 1 | Offline gold formula `elapsed/60*(10+STR*0.5)` | `main.go:163` | formula expr; constants in idle-config |
| 2 | Offline exp formula `elapsed/60*(15+INT*0.8)` | `main.go:164` | formula expr; constants in idle-config |
| 3 | Offline caps (10s min, 86400 max, 10 rolls, 50% drop, 1%/min) | `main.go:154,156-158,168-175` | idle-config data file |
| 4 | Loot table IDs `dummy_drops`/`player_drops` | `main.go:176`, `combat.go:191-195` | NPC-def field `loot_table_id`; remove literals |
| 5 | `dummy_drops` fallback + item `dragon_sword_0` baked in code | `loot/service.go:44-49` | seed row in `loot_tables`; delete fallback |
| 6 | Mob stats (`lvl 10, def 40, dex 10, HP 1000`, id `9999`) | `combat.go:103-108` | NPC/Mob definition table |
| 7 | Kill→quest target mapping (`"wolf"`, `"player"`) | `combat.go:193,196` | NPC-def `quest_tag`; carried on `MobKilled` |
| 8 | Class starting stats (WARRIOR/MAGE/ASSASSIN/SURA) | `character/service.go:52-63` | `content/classes/*.yaml` |
| 9 | `StatGainPerLevel = 2` | `character/leveling.go:4` | progression config; per-class growth |
| 10 | Level-up applies to fixed `{STR,INT,DEX,CON}` | `character/leveling.go:40` | class growth table (per-stat gains) |
| 11 | XP curve `level*level*100` | `character/leveling.go:9` | progression config (curve params / expr) |
| 12 | Derived-stat coeffs hardcoded in glue (`HP=CON*15`,`ATTACK=STR*2+DEX*0.5`,`CRIT=5`) + Go fallback copy | `statclient/client.go:173-183`, `character/stats.go:13-19` | **JSON stat-formula pack → zzstat transforms** (one source, kills the Go/Rust copy) |
| 13 | Combat hit/dodge + 70–99% clamp reimplemented in Go, **bypassing zzstat** | `statclient/client.go:220-231` | **route via `zzstat.EvaluateCombat(formulaJSON)`** |
| 14 | Crit `1.5`, variance `±10%` in the same Go bypass path | `statclient/client.go:255-263` | **combat JSON formula → `EvaluateCombat`** |
| 15 | WS message types (`CHAT`/`SELECT_CHARACTER`/`COMBAT_ATTACK`) | `main.go:124-265` | plugin-registered message handlers |

> Items/quests/loot **payloads** are already externalized (JSONB, migrations `000003/000005/000006`). The residual hardcoding is *call-site coupling* (#4, #7) and *formulas/constants* (#1-3, #8-14).

### 8.3 Loader

`engine/content` walks `content/`, validates each file against the `ContentType.Schema` registered by the owning plugin, and calls its `Register`. Fail-fast on schema errors at boot; `Reload()` for live iteration (Section 5.3).

### 8.4 Formula / scripting recommendation

**Primary decision: the engine already ships a data-driven, JSON-native formula substrate — the embedded `zzstat` Rust core — and the plan is to USE it, not build a competing DSL.** This was the missing insight in the first pass. Verified in the bindings (`github.com/singoesdeep/zzstat/bindings/go/zzstat.go`):

- A **data-driven stat/modifier pipeline**: `RegisterConstantSource` / `RegisterMapSource(map[string]float64)` plus phase/rule transforms `RegisterAdditive` / `RegisterMultiplicative` / `RegisterScaling` / `RegisterClamp` / `RegisterConditional*Transform` (`zzstat.go:153-283`), with a rule vocabulary (`Override/Additive/Multiplicative/Min/Max/MinMax`, `zzstat.go:196-201`). This *is* a designer-tunable derived-stat engine.
- A **JSON combat-formula evaluator**: `EvaluateCombat(formulaJSON string, attacker, defender, rng) (float64, error)` (`zzstat.go:293`). Combat math is meant to be supplied as JSON, not code.

**The real gap is the Go glue, not a missing engine.** Today `statclient` under-uses zzstat and hardcodes what should be data:
- `Calculate()` registers the derived-stat formulas with **coefficients baked into Go** — `RegisterScalingTransform("HP", …, "CON", 15.0)`, `ATTACK=STR*2+DEX*0.5`, `CRIT_RATE=5` (`statclient/client.go:173-183`). These constants belong in a JSON stat-formula pack fed into `RegisterScalingTransform` at load, not literals.
- `CalculateDamage()` **bypasses zzstat entirely** and reimplements hit/dodge/crit/±10%-variance in pure Go with hardcoded constants (`statclient/client.go:220-267`) — `EvaluateCombat`/`formulaJSON` is **never called** (grep-confirmed). This duplicate Go path is exactly the drift risk `stats.go:8-11` warns about.

**Recommendation (revised):**
1. **Stat & combat formulas → zzstat, sourced from JSON content.** Move the hardcoded coefficients (#12 stat coeffs, #13 hit/dodge, #14 crit/variance) into `content/formulas/*.json` and feed them to zzstat's transform registration and `EvaluateCombat(formulaJSON)`. Route `CalculateDamage` **through** `EvaluateCombat` so there is one formula source, killing the Go/Rust drift. zzstat's compiled Rust pipeline also fits the per-attack hot path (`combat.go:162`) better than any interpreted DSL.
2. **Non-stat economy math → a light expression evaluator (`github.com/expr-lang/expr`) OR plain config constants.** Offline gains (#1-2), XP curve (#11), loot rates/caps (#3) are outside zzstat's stat/combat domain; keep them as JSON constants where trivial, and reach for `expr` only where a real expression is needed. Do **not** introduce `expr` for stat/combat math — that is zzstat's job.
3. **Lua/WASM** stays reserved for control-flow behavior (boss AI, world-event scripts) only — never numeric formulas.

Net effect: this **reduces** engine scope. The formula/stat data-driven layer is a *wiring-and-content* task (load JSON → zzstat), not a build-a-DSL task. Content examples:

```json
// content/formulas/derived_stats.json  (fed to zzstat transforms)
{ "HP": [{"scale_from":"CON","factor":15.0}],
  "ATTACK": [{"scale_from":"STR","factor":2.0},{"scale_from":"DEX","factor":0.5}],
  "CRIT_RATE": [{"constant":5.0}] }
```
```json
// content/formulas/combat.json  (passed to zzstat EvaluateCombat as formulaJSON)
{ "hit": "clamp(1 - (def.DEX*0.2 + def.dodge)/(atk.level*1.5) + atk.acc, 0.70, 0.99)",
  "crit_multiplier": 1.5, "variance": 0.10 }
```
```yaml
// content/formulas/offline.yaml  (economy math — expr or constants, NOT zzstat)
gold_per_min: "10 + STR * 0.5"
exp_per_min:  "15 + INT * 0.8"
loot_roll_chance: 0.50
max_hours: 24
```

---

## 9. Extension Points (Hooks)

**Two deliberately distinct mechanisms.** A **hook** is synchronous, priority-ordered, and *returns a value* — for **computing** a rule (damage, gains). An **event** is async, fire-and-forget — for **reacting** to a fact (quest progress on kill). The existing equip→recalc flow (`main.go:268`) is correctly an event and stays one; the kill→loot/quest chain currently mis-implemented as direct calls (`combat.go:193-217`) splits into a `MobKilled` *event* (reactions) plus reward *hooks* (computation).

Map every current hardcoded decision to a named hook/event resolved via registry:

| Extension point | Kind | Replaces (file:line) | Signature (sketch) |
|---|---|---|---|
| `capability:stat.resolver` | capability | Rust call, swappable (`combat.go:144-162`, `character/service.go:120`) | `Calculate/CalculateDamage(ctx, req)` |
| `hook:combat.damage.calc` | hook | Go fallback damage `atk-def,min1` (`combat.go:165-172`) | `func(ctx, DamageCtx) DamageResult` |
| `hook:combat.target.resolve` | hook | dummy `9999/def40/hp1000` (`combat.go:103-120`) | `func(ctx, id) (TargetStats, bool)` |
| `hook:combat.death.rewards` | hook | table selection `dummy_drops`/`player_drops` (`combat.go:191-197`) | `func(ctx, DeathCtx) RewardBundle` |
| `event:MobKilled` | event | inline quest tags `wolf`/`player` (`combat.go:193,196`) | quests/achievements subscribe |
| `hook:loot.roll` | hook | RNG + `rate<10000` algo (`loot/service.go:54-66`) | `func(ctx, LootRollCtx) []Drop` |
| `hook:reward.apply` | hook | gold-vs-item dispatch (`combat.go:205-219`, `main.go:179-191`) | `func(ctx, DroppedItem) error` |
| `hook:offline.gains.calc` | hook | offline formula (`main.go:161-164`) | `func(ctx, OfflineCtx) OfflineReward` |
| `hook:offline.loot.roll` | hook | offline loot (`main.go:166-195`) | `func(ctx, OfflineCtx) []Drop` |
| `hook:character.create.stats` | hook | class base stats (`character/service.go:52-63`) | data-driven `class_def` |
| `hook:stat.recalc.modifiers` | hook | equip modifier assembly (`character/service.go:94-116`) | contribute `[]Modifier` |
| `event:CharacterLeveledUp` | event | level-up recalc (`character/service.go:142`) | already event-shaped |
| `RegisterMessageHandler` | transport | WS `switch` (`main.go:124-265`) | per-type, per-plugin |

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
- H3. Externalize formulas to JSON: **feed zzstat from content** (stat coeffs `client.go:173-183` → JSON transforms; route `CalculateDamage` through `zzstat.EvaluateCombat` — deletes the Go bypass at `client.go:220-267`); economy math (offline `main.go:161-196`, XP curve) as config/`expr`.
- H4. Register WS message handlers via plugins (remove `main.go` switch).

- H5. **Unify the `Modifier` concept** into one shared-kernel type (kills the 3× duplication A9); introduce a single `CharacterID` value type (A10) to end the `int32/int64` casts.
- H6. **Move interface ownership to consumers** — `combat`/`quests` declare the minimal interfaces they need instead of importing producers' full service surface (prerequisite for the plugin/hook layer).

**Medium:**
- M1. Persistence abstraction (`Store`/`UnitOfWork`) over pgx (A6, DB-independence); pull leveling orchestration out of `character/repository.go:195-265` into an aggregate method (A11).
- M2. Transport abstraction (HTTP+WS behind `Router`); lift the `CombatSession`/`Encounter` aggregate out of `socket` into a domain package (A8).
- M3. Redis-Streams event impl + transactional outbox + `event_log` replay; add `log.Error` on the swallowed economy writes first (A13).
- M4. **De-globalize** `events.globalBus` and `socket.globalRegistry` into engine-owned instances (A12) — unlocks isolated test/sharded worlds.

**Low:**
- L1. `enginectl` CLI (scaffold plugin, validate content).
- L2. `math/rand/v2` to drop `randMu` (`loot/service.go`).
- L3. Move remaining hardcodes (dummy mob, fallback item); evict-on-idle for `keyedMutex` map growth.

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
| `Modifier` duplicated 3× | `character.EquipmentModifier`, `statclient.Modifier`, `items.StatModifier` | Every new content type re-implements + translates it |
| ID type chaos | `int32` vs `int64` character IDs across packages | Casts everywhere; blocks a typed `CharacterID` |
| Gameplay state in transport | `socket.SessionRegistry` (`session.go:21-27`) | Two systems of record; lost on restart; blocks scale-out |
| Anemic model + logic in repos | `character/repository.go:195-265` leveling in a SQL tx | Domain not testable/reusable in isolation |
| Global singletons | `events.globalBus`, `socket.globalRegistry` | No isolated worlds / clean tests / sharding |
| Silent economy loss | swallowed errors `combat.go:207,216`, `main.go:190` | Players short-changed with no log/retry/alert |
| zzstat under-used / bypassed | stat coeffs hardcoded in glue (`client.go:173-183`); `CalculateDamage` reimplements combat in Go, never calls `EvaluateCombat` | Duplicate Go/Rust formula paths → the drift risk `stats.go:8-11` warns of; wastes an already-data-driven asset |

---

## Appendix — Evidence index (key file:line references)

- Manual DI + inline rules: `cmd/server/main.go:36-434` (offline `:161-196`, WS switch `:124-265`, event subs `:268-288`, circular-dep setter `:94`).
- Toy event bus: `internal/events/events.go` (whole file, 2 event types).
- Combat God service: `internal/combat/combat.go` (imports `:6-13`; dummy `9999`; table IDs; kill chain).
- Hardcoded class stats: `internal/character/service.go:52-63`; fallback stat path in `RecalculateStats`.
- Inventory single-node lock + equip rules: `internal/inventory/service.go` (`keyedMutex`, `validateEquipmentRequirements`).
- Loot RNG lock + hardcoded fallback: `internal/loot/service.go` (`randMu`, `dummy_drops`, `dragon_sword_0`).
- Cache-aside decorator (good pattern to reuse): `main.go:113-114`, `internal/loot/` cached repository.
