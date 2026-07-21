# Game Framework Design (gamekit)

> **Status:** design proposal, not yet implemented. This document captures the
> agreed direction for turning the codebase into a **batteries-included game
> framework** whose toolkits are general enough to build an RPG, an RTS, or a
> city-builder — by using some toolkits, ignoring others, and swapping engines
> via plugins and hooks.

## 1. Vision

The framework ships the **fundamental game systems as a rich, hooked core**, not
as a hollow kernel plus a pile of a-la-carte plugins. A game author gets working
toolkits out of the box (entities, stats, inventory, progression, activity/tick
engines) and **extends or replaces** them through pervasive hook points and
plugins.

Crucially, the toolkits are **general and composable**:

- A **character** is an entity with stats + inventory + progression components.
- A **city** is an entity with a resources component and a production system —
  and *no* combat.
- A **stat-less** entity is legal; or stats can drive city-development bonuses,
  unit armor, anything.
- The **combat engine is one System among many** — a city-builder drops it and
  writes a production System instead.

So it is an "RPG framework," but "an RTS can be an RPG": the same primitives,
used differently.

## 2. Layers & modules

Three importable Go modules, dependencies pointing down only:

```
game/      a specific game — registers gamekit systems + its own plugins/content
   │
gamekit/   github.com/singoesdeep/zzrpg/gamekit  (NEW)
   │       entity · component · stats · inventory · progression · world/query ·
   │       system + scheduler · hook catalog · content loaders · realtime (opt.)
   │
sdk/       github.com/singoesdeep/zzrpg/sdk  (existing substrate)
           kernel · plugin · registry · bus · hooks · store · outbox · admin · accrual math
```

- **sdk** stays the game-agnostic substrate (plugin lifecycle, DI, event bus,
  hook engine, persistence seam, outbox). The low-level accrual math
  (`Window`, `Producer`) stays here as a primitive.
- **gamekit** is the new game-framework layer: the entity-component model, the
  generic stat engine, the built-in component toolkits, and the **System**
  abstraction + scheduler. This is "the core" the user wants filled in.
- **game** modules compose gamekit toolkits, register their own Systems /
  components / content, and hook in.

## 3. Entity–Component model (ECS-flavoured)

Data (components) is separated from behaviour (Systems).

### Entity
A minimal identity row; everything else is a component.

```go
type Entity struct {
    ID      int64
    Kind    string // template name: "character", "city", "goblin", "worker"
    OwnerID int64  // optional account owner (0 = world-owned)
}
```

### Component
A component is **typed data attached to an entity**, backed by its own table.
Components are optional and composable — an entity has exactly the components it
was given.

```go
// A ComponentType is registered with the framework; it knows how to load/save
// its data for an entity. Games and plugins add new component types + tables.
type ComponentType[T any] struct {
    Name string                 // "stats", "inventory", "resources", "production"
    Repo ComponentRepo[T]       // load/save/delete over store.Store
}
```

Built-in component types (gamekit): `stats`, `inventory`, `progression`,
`position` (optional). Games add more (`resources`, `production`, `building`).

### Composition
Which components an entity has is **data-driven** by its template (§7): a
template declares its components and their defaults. Code may also attach/detach
components at runtime through the entity service.

### Persistence — component tables
`entities` plus one relational table per component, keyed by `entity_id`:

```sql
entities(id, kind, owner_id, created_at)
entity_stats(entity_id PK, base_stats JSONB, derived_stats JSONB)
entity_inventory(entity_id, slot, item_id, qty, ...)         -- one row per slot
entity_progression(entity_id PK, level, xp)
entity_resources(entity_id, resource_id, amount)             -- wallet
-- game-added, via plugin migrations:
entity_production(entity_id, kind, level, last_tick)
```

Each toolkit/plugin owns its component table through the existing per-module
migration mechanism (`plugin.Migrator`).

## 4. Stat / attribute engine

A **fully generic** attribute engine, attachable to any entity (or not).

- `stats` component holds a `base_stats` map of arbitrary named attributes
  (`STR`, `HP`, `population`, `production_rate`, `armor`, …).
- **Derived** stats are computed by a formula pack (the existing zzstat resolver
  becomes a pluggable `StatResolver`; a game without native math uses a pure-Go
  resolver). Formulas are content-driven per entity kind.
- Stats are consumed anywhere via hooks: a `HookStatsDerive` filter lets plugins
  inject auras/buffs; systems read derived stats to scale output (combat power,
  city bonuses, unit strength).

```go
type StatResolver interface {
    Derive(ctx context.Context, kind string, base map[string]float64) (map[string]float64, error)
}
```

## 5. The System abstraction (the generalised "engine")

A **System** is a unit of game behaviour that runs over entities. It generalises
today's idle `Producer` so combat, city production, idle accrual, AI, etc. are
all Systems of two flavours (a System may be both):

```go
// TickSystem runs periodically and on offline catch-up. elapsed covers the real
// time since its last run for the given entity (idle-style accrual).
type TickSystem interface {
    Name() string
    Interval() time.Duration
    Query() []string                    // component names this system needs
    Tick(ctx context.Context, e Entity, w World, elapsed time.Duration) error
}

// EventSystem reacts to a domain event (e.g. an attack, an item equip).
type EventSystem interface {
    Name() string
    On() string                         // event name it subscribes to
    Handle(ctx context.Context, ev bus.Event, w World) error
}
```

- **World / queries** — component-indexed, ECS-style:
  `w.Query("production")` returns entities having that component; a system
  iterates them. Generic helpers: `world.With[ProductionComponent](w)`.
- **Scheduler** — the framework runs registered `TickSystem`s at their
  intervals and dispatches `EventSystem`s on their events. A tick computes
  `elapsed` from the entity/system's last-run timestamp, so **offline and online
  are unified**: on load or on interval, the same `Tick` runs with the real
  elapsed time (the current idle offline+online logic, generalised).

**Examples as Systems**
- Combat → `EventSystem` on `ATTACK`: reads attacker/defender `stats`, applies a
  `HookCombatDamage` filter, mutates HP, emits `KILLED`.
- Idle stage/lifeskill/generator → `TickSystem` over entities with an `activity`
  or `production` component; output scales with stats/skill/building level.
- City production → a `TickSystem` a city-builder writes instead of combat.

## 6. Extension: hooks everywhere + events

Two complementary mechanisms, both first-class:

- **Hooks (synchronous)** — named `Filter`s (transform a value mid-flow) and
  `Action`s (ordered side effects / veto gates), instrumented at *every*
  meaningful point. Plugins `AddFilter`/`AddAction` to customise behaviour
  without forking. gamekit exports a documented **hook catalog** of constants.
- **Events (asynchronous)** — the `bus`, for cross-system reactions and
  cross-node fanout; written through the outbox for exactly-once when inside a
  transaction.

Proposed hook catalog (initial):

| Hook | Kind | Point |
|------|------|-------|
| `entity.create` | Action | after an entity is created |
| `entity.component.attach` | Action | a component attached |
| `stats.derive` | Filter | derived-stat map before it's stored |
| `progression.xp` | Filter | xp amount before applying |
| `progression.levelup` | Action | on each level gained |
| `inventory.additem` | Filter/gate | before an item enters inventory |
| `inventory.equip` / `unequip` | Action | on equip changes |
| `combat.damage` | Filter | computed damage before it lands |
| `combat.kill` | Action | on a killing blow |
| `loot.roll` | Filter | a loot result before it's granted |
| `system.tick` | Action | around each system tick (metrics/gating) |
| `reward.grant` | Filter | gold/exp/resources before crediting |
| `craft.recipe` | Filter/gate | before a craft is consumed |

Rule of thumb: **Filter** when a plugin should *change a number/decision*;
**Action** when it should *react or veto*; **Event** when reaction is async /
cross-system. Swapping a whole engine = don't register the built-in System,
register your own.

## 7. Data-driven content

Heavily data-driven so designers work in JSON, not Go:

- **Entity templates** (`content/entities/*.json`) — `kind → {components, defaults}`.
  e.g. `warrior` → stats(STR 15, CON 15…) + inventory(slots) + progression(lvl 1);
  `lumber_mill_city` → resources + production(...).
- **Stat formulas** — derived-stat packs per kind.
- **System params** — activity/production/recipe/loot tables.

All loaded through the generic content registry (`registry.DefineContent[T]`),
so a plugin declares its own content type without touching shared code.

## 8. Realtime (optional)

WebSocket transport is an **optional gamekit system**, not always-on. A
multiplayer RPG opts in (WS hub, message router, per-connection auth); a
single-player city-builder can omit it and use plain HTTP. The transport layer
stays domain-free (injected authenticator, generic message router).

## 9. What ships in gamekit v1

- **entity** — entity service + `entities` table + templates + composition.
- **component** — component-type registry + repo seam.
- **stats** — generic attribute engine + pluggable `StatResolver`.
- **inventory** — slots, items, equip, item definitions.
- **progression** — xp/level curves (data-driven).
- **world** — component-indexed queries.
- **system** — `TickSystem`/`EventSystem` + the scheduler (offline catch-up).
- **hooks** — the hook-catalog constants + helpers.
- **content** — loaders/helpers over the registry.
- **realtime** (optional) — WS hub, router, authenticator seam.

Everything else (a specific combat engine, a specific idle activity set, quests,
crafting, achievements, chat) is a **plugin/feature** on top — the "extension"
tier the user wants plugins to occupy.

## 10. Worked examples (same toolkits, different games)

**RPG** (`cmd/rpg`): register gamekit + realtime. Templates give characters
`stats + inventory + progression`. A **combat EventSystem** and idle
**TickSystems** (stages/lifeskills/generators) are plugins. Crafting, quests,
loot are feature plugins hooking into `reward.grant`, `loot.roll`, etc.

**City-builder** (`cmd/city`): register gamekit, **no combat, no realtime**. A
city is an entity with a `resources` component; a **production TickSystem**
grows resources (optionally scaled by a `stats` component representing city
development). Buildings are entities or a component. Same entity/stats/tick
machinery — combat simply never registered.

Both prove the thesis: **the core is full of toolkits; the game chooses which to
use and swaps engines via Systems + hooks.**

## 11. Rollout (greenfield)

Per the decision, build fresh (referencing the current code as a reference
implementation), not an in-place migration:

1. **gamekit module** — entity/component/stats/inventory/progression/world/
   system/hooks/content, with tests, standalone.
2. **RPG game** — rebuild on gamekit: character template, combat System, idle
   Systems, crafting/quests/loot plugins.
3. **City game** — rebuild on gamekit: city template, production System.
4. Retire the old `backend/plugins/*` domain-plugin structure once both games
   run on gamekit.

## 12. Open items (to settle during implementation)

- **Auth/accounts** — proposed as an optional gamekit system (entities may have
  an `OwnerID`); single-player games can skip it.
- **Exact hook catalog** — §6 is a starting set; finalise per toolkit.
- **Component query performance** — component-indexed queries need indexes;
  large worlds may want an in-memory index cache (out of scope for v1).
- **Transactional System application** — a tick that spans multiple component
  tables should apply within one `WithinTx`; define the unit-of-work boundary.
- **Naming** — `gamekit` module path and package names to confirm.
