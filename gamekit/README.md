# gamekit — a batteries-included game framework

`gamekit` ships the **fundamental game systems as a rich, hooked core** — not a
hollow kernel plus a pile of a-la-carte plugins. You get working entities,
stats, progression, inventory, an economy, a relation graph, and a system
scheduler out of the box, each with hook points so plugins **extend or replace**
behaviour without the core knowing about them.

The toolkits are deliberately **genre-neutral**. A character, a city, a mob, and
an RTS unit are all just *entities* of a different `Kind`; stats are optional; the
combat "engine" is a game-side system a city-builder simply never registers. Use
the toolkits you need, ignore the rest, and swap engines via plugins and hooks —
the same core builds an RPG, an RTS, or a city-builder.

> Design rationale: [`../docs/FRAMEWORK_DESIGN.md`](../docs/FRAMEWORK_DESIGN.md).
> Worked game exercising every toolkit live: `../backend/plugins/gamedemo`.

## Assemble the core in one call

```go
k := kit.New(kit.Deps{
    Store:    db.Store,                          // sdk store seam (Postgres)
    Hooks:    h,                                 // sdk hooks
    Bus:      bus,                               // sdk event bus (for EventSystems)
    Formulas: byKind,                            // stat derivation per entity kind
    Curve:    progression.Curve{Base: 50, Exp: 2},
})
// k.Entities, k.Stats, k.Progression, k.Inventory, k.Economy, k.Relations,
// k.World, k.Composer, k.Scheduler are ready to use.
```

`kit.New` wires the entity repo, the built-in component stores and their
services, the world, a composer pre-registered with the built-in component
initializers, and the system scheduler. Ship the standard schema with
`kit.MigrationSource()`. Then add your game's **own** components, systems, and
hooks on top.

## Toolkits

| Package | What it is | Hook seams |
|---|---|---|
| `entity` | Bare identity (`ID`, `Kind`, `OwnerID`); all game data lives in components. Owner/kind queries. | — |
| `component` | Typed component stores (`Store[T]`), in-mem or JSONB, indexed for `world` queries. | — |
| `world` | Intersects component indexes: "every entity with `production`", typed `With[T]` iteration. | — |
| `stats` | Base stats + derived stats via a pluggable `StatResolver` (pure-Go `FormulaResolver`, no native lib needed). | `HookDerive` |
| `progression` | XP and levels over a `Curve`; grants may cascade level-ups. | `HookXP`, `HookLevelUp` |
| `inventory` | Item stacks with add/remove/has for loot and crafting sinks. | `HookAddItem` |
| `economy` | Wallets of named currencies with affordability-checked earn/spend. | `HookEarn`, `HookSpend` |
| `relation` | Typed directed edges between entities (contains, member-of, equips…), queryable both ways. | — |
| `system` | `TickSystem` (interval + offline catch-up) and `EventSystem`, driven by a `Scheduler`. | — |
| `idle` | Idle-accrual base: an Assignment component + Engine + ready TickSystem that drives developer-supplied Activities (`engine/idle.Producer`) and routes their Output into economy/progression/inventory. Buildings/lifeskills/stages are plugins, not core. | `HookOutput` |
| `template` | Data-driven spawning: a JSON template says which components a `Kind` has and their defaults. | — |
| `kit` | Batteries-included assembly of all of the above + standard schema. | — |

Everything routes through the sdk `hooks` package, so a plugin can filter a value
(`AddFilter`/`ApplyFilters`) or react to an event (`AddAction`/`DoAction`) at any
seam above — the mechanism the whole framework's extensibility is built on.

## The pattern: core + your game

`gamedemo` is the reference. It:

- **composes entities from JSON templates** (`warrior`, `goblin`, `city`,
  `house`) — designers define "what a thing is" in data, not Go;
- adds its **own** components (`Health`, `Resources`, `Production`) and its **own**
  systems (a production `TickSystem`; a `Combat` resolver — an example the core
  does not impose, so a city-builder omits it);
- wires cross-toolkit reactions purely through **hooks**: a level-up grants a
  trophy (progression → inventory), a kill grants XP (combat → progression → …),
  a damage filter adds a weapon bonus;
- shows the **RTS/city-builder** shape too: a city *contains* buildings via the
  `relation` graph, each building producing independently, and spends from a
  `wallet` via `economy`.

A game that wants none of combat, stats, or inventory simply never touches those
toolkits — the core imposes no game concepts on your entities.
