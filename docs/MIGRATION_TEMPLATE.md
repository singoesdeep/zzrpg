# Migrating a domain subsystem onto gamekit

> Derived from the **idlekit pilot** (`backend/plugins/idlekit`), which rebuilt
> the idle subsystem's core accrual on gamekit and ran it live in `cmd/server`
> alongside the legacy `idle` plugin. This is the repeatable recipe for porting
> the remaining subsystems (loot, quests, crafting, combat, …) one at a time.

## The shape of a legacy subsystem

Each old subsystem is a **bespoke service** (`game/<domain>`) plus a **plugin**
(`plugins/<domain>`) that owns tables, an ad-hoc ticker or event handlers, and
its own persistence. idle was: `idle.Service` + `WalletRepo`/`AssignmentRepo`/…
+ a hand-rolled `runTicker` goroutine + an `online` set.

## What it maps to on gamekit

| Legacy piece | gamekit primitive |
|---|---|
| The domain aggregate (a character, a quest log) | an **entity** (`entity.Repo`) — `Kind` names it |
| Per-aggregate state tables (`idle_wallet`, …) | **component stores** (`component.Store[T]`, JSONB) |
| A bespoke `Service.Accrue`/`Tick` loop | a **`system.TickSystem`** (interval + offline catch-up) |
| A hand-rolled `time.Ticker` + online set | the **`system.Scheduler`** (`Run`/`TickAll`/`Catchup`) |
| Event-driven reactions (on login, on kill) | an **`EventSystem`**, or a hook `Action` |
| A resource/gold wallet repo | the **`economy`** toolkit (`Earn`/`Spend`, hooked) |
| Cross-aggregate links (party, contains) | the **`relation`** toolkit |
| Reward/boost interception | a **hook filter** (`AddFilter`/`ApplyFilters`) |
| Assembling all of the above | **`kit.New`** (one call) |

The whole legacy plugin collapses to: `kit.New` + a component or two + a
`TickSystem`/`EventSystem` + hooks. idlekit is ~180 lines vs the old idle stack's
service + repos + ticker.

## The two bridges (the part that needs care)

A subsystem doesn't live alone — it reads from and writes to the rest of the
game. Porting it incrementally means bridging to the still-legacy neighbours:

1. **Read bridge — legacy → gamekit.** idlekit reads the live character's derived
   stats and level (`character.CharacterService.GetByID`) and turns them into a
   gamekit `Producer` component's rate. Pattern: on the triggering event, refresh
   the entity's components from the authoritative legacy aggregate.

2. **Write bridge — gamekit → legacy.** The full port reflects gamekit results
   back (e.g. `character.AddRewards(gold, exp)`). The pilot deliberately **skips**
   this: it accrues into a *separate* gamekit wallet so it can run alongside the
   legacy idle **without double-crediting**, purely for behaviour comparison. Turn
   the write bridge on only when the legacy subsystem is being retired.

### Mirroring an aggregate as an entity

gamekit entity IDs are `BIGSERIAL` — they won't equal a character's ID. The
pilot maps the two by creating the mirror entity with `OwnerID = characterID`
and a dedicated `Kind` (`"idlekit"`), then looking it up with
`Entities.ListByOwner(charID)` filtered by kind (`ensureEntity`, serialised so
concurrent logins don't double-create). A dedicated mapping table also works; the
owner-field trick avoids one.

## Running side by side (non-breaking)

The pilot ships the gamekit standard tables it needs with `CREATE TABLE IF NOT
EXISTS` in its own migration module, mounts under its own routes
(`/api/v1/idlekit/...`), and credits its own wallet. So it's **additive** — the
live game is untouched, and you compare the gamekit accrual against the legacy
one before committing to the swap.

## Porting checklist

1. Identify the aggregate → pick an entity `Kind`; write `ensureEntity`.
2. Move each state table to a `component.Store[T]`; register it on `kit.World`.
3. Rewrite the core loop as a `TickSystem` (or `EventSystem`); `AddTick` it.
4. Wire reactions through hooks instead of direct calls.
5. Read bridge: refresh components from the legacy aggregate on the trigger event.
6. Mount additively (own routes, own module migration, own wallet) and compare.
7. Only once parity holds: enable the write bridge, move the routes over, and
   retire the legacy plugin.

## Framework, not catalog

The idle swap surfaced the key principle for the whole port: **gamekit provides
the mechanism + integration; developers provide the content as plugins.** The
legacy `game/idle` baked stages, lifeskills, and buildings into the core. The
gamekit `idle` toolkit instead ships the accrual Engine, the Assignment
component, the TickSystem, hooks, and the Output router — and takes the concrete
activities as `engine/idle.Producer` implementations registered on a shared
registry (exposed via `registry.Provide("idleActivities", …)`). A buildings
plugin, a lifeskills plugin, etc. each register their own producers without
touching idlekit. Port a subsystem by extracting its *framework* into gamekit and
leaving its *content* as plugins.

## A second pattern: swap the engine, keep the contract

idle had few, controllable consumers, so the pilot could delete the legacy
plugin outright. **loot** has wide fan-in (`combat`, `killreward`,
`character.events`, `tests/integration_test.go`) where forcing every consumer
onto a new type immediately is unnecessary risk for no behavioural gain — the
roll *mechanism* (weighted probability, RNG concurrency-safety) was already
genre-agnostic; only the *types* (`LootTable`/`DroppedItem`) are RPG-flavoured.

So for a widely-depended-on subsystem: extract the mechanism into a gamekit
toolkit taking its inputs as **funcs, not concrete stores** (mirrors
`gidle.Deps.StateFor`/`Apply`) — here, `EntriesFor(ctx, tableID) ([]Entry,
error)` instead of owning a `Repo`. Then rewrite the legacy package's service to
build a gamekit `Roller`/`Engine` internally and translate types at the
boundary, keeping every exported name and behaviour identical. **The existing,
untouched tests are the parity proof** — they exercise the public contract, so
if they pass unmodified, the swap is behaviourally invisible to every consumer.
Nobody importing `game/loot` needs to change; the roll algorithm underneath is
now gamekit's.

Use this pattern (swap the engine, keep the contract) when a subsystem has many
consumers and its types are already reasonable; use the idle pattern (delete and
rebuild) when consumers are few and the legacy shape itself is the problem.

## Status

- **idle → idlekit: SWAPPED (delete-and-rebuild).** Legacy `plugins/idle` and
  `game/idle` deleted; `idlekit` on `gamekit/idle` owns idle end to end in
  `cmd/server` — write bridge on, same `/characters/{id}/idle/*` contract.
  `backend/plugins/buildings` proves the framework/content split: it registers
  its own Producers on idlekit's shared registry and injects its own inputs via
  `gamekit/idle.HookState` — zero changes to idlekit.
- **loot → gamekit/loot: SWAPPED (engine-only).** `gamekit/loot.Roller` now does
  the weighted-roll math and RNG; `game/loot` is a thin adapter (types, DB/cache
  persistence, admin CRUD, its own `HookRoll` for back-compat) — combat,
  killreward, character events, and existing tests are untouched and still pass.
- **Next**: quests, combat — pick delete-and-rebuild or engine-only per the fan-in
  test above.
