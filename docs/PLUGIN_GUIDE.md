# zzrpg Plugin Author Guide

This guide is for developers extending zzrpg. A **plugin** adds behaviour to the
engine — new reactions, gameplay tweaks, endpoints, content — **without modifying
the core**. It is the WordPress-style extension model, in Go.

> Plugin API version: **`plugin.APIVersion`** (semver). A minor bump adds
> extension points; a major bump changes the surface you depend on.

---

## 1. What a plugin is

A plugin implements `plugin.Plugin`:

```go
type Plugin interface {
    Meta() Meta                 // identity + hard dependencies (by name)
    Init(InitContext) error     // register services, routes, events, hooks
    Start(RunContext) error     // begin background work (optional)
    Stop(context.Context) error // tear down (optional)
}
```

Embed `plugin.Base` for no-op `Start`/`Stop` if you only need `Init`. The kernel
topologically sorts plugins by `Meta().Requires`, then runs `Init` (all deps
initialised), then `Start`, then `Stop` in reverse.

`Init` receives an `InitContext`, your channel to the engine:

| Method | Use |
|---|---|
| `Registry()` | typed DI: `registry.Provide[T]` / `registry.Resolve[T]` services |
| `Bus()` | the typed event bus — **react** to events (async) |
| `Hooks()` | the hook registry — **participate** in flows (sync filters/actions) |
| `Mux()` | register HTTP routes |
| `Config()`, `Logger()`, `Context()` | config, structured logger, lifecycle ctx |

Plugins can also optionally implement `plugin.AdminDescribor` to register administrative metadata and UI views dynamically in the Admin Dashboard (`/admin`):

```go
type AdminDescribor interface {
    AdminInfo() AdminInfo // Title, Description, Icon, Category, Endpoints
}
```

---

## 2. The four extension mechanisms

### a) Services (dependency injection)
Provide a service other plugins can resolve, or resolve one you depend on:

```go
svc := registry.MustResolve[character.CharacterService](ic.Registry(), "character")
registry.Provide(ic.Registry(), "myservice", myService)
```

### b) Events — react (async, fire-and-forget)
Subscribe to a domain event to run side effects after it happens. Events never
block the producer and are broadcast across nodes.

```go
ic.Bus().Subscribe(combat.EventMobKilled, func(_ context.Context, ev bus.Event) {
    if k, ok := ev.(combat.MobKilled); ok {
        log.Info("kill", "killer", k.KillerID, "victim", k.VictimID)
    }
})
```

### c) Hooks — participate (sync, ordered)
Hooks run *inside* a flow so you can modify a value or gate an action:

- **Filter** — transform a value threaded through the chain:
  ```go
  hooks.AddFilter(ic.Hooks(), character.HookRewards, 10,
      func(_ context.Context, r character.RewardsFilter) character.RewardsFilter {
          r.Gold *= 2 // double-gold event
          return r
      })
  ```
- **Action** — ordered side effects; return an error to **abort/veto**:
  ```go
  hooks.AddAction(ic.Hooks(), combat.HookPreAttack, 10,
      func(_ context.Context, a combat.PreAttack) error {
          if inSafeZone(a.DefenderID) {
              return errors.New("peaceful zone")
          }
          return nil
      })
  ```

`priority` is ascending (10 is a common default); ties keep registration order.
Hooks are **panic-isolated** and **nil-safe**.

### d) Content packs
Tunable game data is embedded JSON in `backend/content/` (class stats, derived-
stat coefficients, mob defs, the combat formula, idle economy, loot tables). Edit
these to add classes/mobs/loot with no code.

**Override without recompiling:** set `ZZRPG_CONTENT_DIR=/path/to/packs`. Any pack
file present there (by its relative path, e.g. `mobs/mobs.json`) is loaded instead
of the embedded default; files you don't provide fall back to the embedded pack.
So an operator or plugin can ship just the packs it changes.

---

## 3. Extension-point catalog

### Hooks

| Hook | Kind | Value (modify) | When |
|---|---|---|---|
| `combat.pre_attack` | action/veto | `combat.PreAttack{AttackerID, DefenderID, SkillID}` | before an attack resolves |
| `combat.damage` | filter | `combat.DamageFilter{…, Damage}` | after damage computed, before it lands |
| `loot.roll` | filter | `loot.LootRoll{TableID, Items}` | after a table is rolled, before return |
| `character.rewards` | filter | `character.RewardsFilter{CharacterID, Gold, Exp}` | before gold/exp are applied |
| `character.stats_recalc` | filter | `character.StatsRecalcFilter{CharacterID, DerivedStats}` | after derived stats recomputed, before cached (auras/buffs) |
| `quest.accept` | action/veto | `quests.QuestAccept{CharacterID, QuestID}` | on accept, after the level check (prerequisites, locks) |
| `quest.progress` | filter | `quests.QuestProgressFilter{…, Amount}` | before progress is applied (scale progress) |

### Events (subscribe via `Bus()`)

| Domain | Events |
|---|---|
| combat | `CombatAttackResolved`, `MobKilled`, `PlayerKilled`, `CharacterDamaged` |
| character | `RewardsGranted`, `CharacterLeveledUp`, `StatsRecalculated`, `CharacterLoggedIn`, `CharacterLoggedOut`, `OfflineGainsGranted` |
| quests | `QuestAccepted`, `QuestProgressed`, `QuestCompleted` |
| inventory | `ItemEquipped`, `ItemUnequipped`, `ItemAddedToInventory` |
| loot | `LootDropped` |

Each event type has a `Name()` and a matching `Event<Name>` string constant, and
implements `bus.Event`. Type-assert the payload in your handler.

---

## 4. A complete example

Two tested reference plugins under `backend/examples/plugins/` show different
patterns — copy whichever fits:

- [`xpboost`](../backend/examples/plugins/xpboost/plugin.go) — **hook-driven**: a
  rewards filter (double gold), a pre-attack veto (protect a target), a
  `MobKilled` subscription, and an HTTP route.
- [`achievements`](../backend/examples/plugins/achievements/plugin.go) — **purely
  event-driven & stateful**: it uses no hooks, only subscribes to `MobKilled` /
  `QuestCompleted` / `CharacterLeveledUp`, keeps per-character progress, **provides
  its Tracker as a service** other plugins can resolve, and exposes a read
  endpoint.

### Testing your plugin

Use `plugin/plugintest` to run a plugin's `Init` in isolation over real engine
primitives — no kernel needed (the `net/http/httptest` pattern for plugins):

```go
h := plugintest.New()
registry.Provide(h.Registry(), "character", mockCharSvc) // satisfy Requires
if err := h.Init(&myplugin.Plugin{}); err != nil { t.Fatal(err) }

// Inspect what it registered:
out := hooks.ApplyFilters(h.Hooks(), ctx, character.HookRewards, character.RewardsFilter{Gold: 100})
// ... assert out.Gold, publish to h.Bus(), serve h.Mux(), etc.
```

---

## 5. Registering your plugin

Add it to the kernel's plugin list (in `cmd/server`), declaring its dependencies
in `Meta().Requires`:

```go
k := kernel.New(cfg, log)
k.Register(
    &corePlugin{}, authPlugin{}, itemsPlugin{},
    &characterPlugin{}, inventoryPlugin{}, lootPlugin{},
    questsPlugin{}, combatPlugin{},
    &idlePlugin{},                    // built-in standalone idle gains
    &xpboost.Plugin{ProtectedID: 1}, // your custom plugin
)
k.Run(ctx)
```

The kernel orders plugins after their declared dependencies automatically (`Meta().Requires`). Because plugins communicate asynchronously via the event bus and synchronously via hooks, omitting a plugin (e.g. omitting `&idlePlugin{}` for an RTS or non-idle game) leaves the rest of the engine running seamlessly. Plugins are **compiled-in** (statically linked, fully type-safe) when building the server.

---

## 6. Best practices

- **Don't block in event handlers** — they run async; keep them fast or hand off.
- **Keep filters pure and cheap** — they run synchronously on the request path.
- **Use priorities** to order against other plugins; don't assume you're alone.
- **Fail safe** — a returning-error action is a deliberate veto; a *panic* is
  isolated and logged, and the flow continues as if your hook did nothing.
- **Declare `Requires`** for every producer whose hooks/events/services you use,
  so ordering is guaranteed.
- **Pin `plugin.APIVersion`** if you distribute your plugin.
