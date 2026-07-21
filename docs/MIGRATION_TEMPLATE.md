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

## Status

- **idlekit**: pilot done — core offline/online accrual on gamekit, live in
  `cmd/server`, unit-tested (`plugin_test.go`), read bridge on, write bridge off.
- **Next**: loot, quests, crafting, combat — same recipe; enable write bridges and
  retire legacy plugins subsystem by subsystem.
