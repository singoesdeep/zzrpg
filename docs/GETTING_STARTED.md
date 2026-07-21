# Getting Started — build your own game on gamekit

This walks a new developer through building a small, complete game on gamekit
from scratch: a "critter farm" — spawn critters from a template, they idle-earn
food, and a plugin you write afterwards adds a second building type without
touching any of the code from this guide. It's deliberately not an RPG, to make
the point: gamekit doesn't know or care what game you're building.

For the fuller worked example (stats, progression, inventory, combat, a
relation graph, an economy) read `backend/plugins/gamedemo` alongside this
guide. For porting an *existing* game onto gamekit, see
[MIGRATION_TEMPLATE.md](MIGRATION_TEMPLATE.md) instead.

## 0. Set up the module

A game is its own Go module that depends on `gamekit` (which depends on `sdk`)
via `replace` directives if you're working inside this monorepo, or as a normal
module dependency once gamekit is published. You need Postgres reachable via
`DATABASE_URL` — gamekit's built-in components are JSONB-backed Postgres stores.

```go
// go.mod
require github.com/singoesdeep/zzrpg/gamekit v0.0.0
replace github.com/singoesdeep/zzrpg/gamekit => ../gamekit
replace github.com/singoesdeep/zzrpg/sdk => ../sdk
```

## 1. Assemble the core with `kit.New`

One call gives you entities, stats, progression, inventory, economy, a world,
a template composer, and a system scheduler — the batteries:

```go
package critterfarm

import (
    "github.com/singoesdeep/zzrpg/gamekit/kit"
    "github.com/singoesdeep/zzrpg/gamekit/progression"
    "github.com/singoesdeep/zzrpg/sdk/engine/plugin"
)

type Plugin struct {
    plugin.Base
    kit *kit.Kit
}

func (*Plugin) Meta() plugin.Meta {
    return plugin.Meta{Name: "critterfarm", Requires: []string{"core"}}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
    reg := ic.Registry()
    db := registry.MustResolve[*database.DB](reg, "db")

    p.kit = kit.New(kit.Deps{
        Store: db.Store,
        Hooks: ic.Hooks(),
        Bus:   ic.Bus(),
        Curve: progression.Curve{Base: 50, Exp: 2}, // xp to level N = 50*N^2
        // Formulas: nil is fine — you don't need derived stats for this game.
    })
    return nil
}
```

Ship the standard schema so `entities`/`entity_wallet`/etc exist:

```go
func (*Plugin) Migrations() plugin.MigrationSource {
    return kit.MigrationSource() // module "gamekit" — shared, idempotent
}
```

A game that adds its own components ships an *additional* migration for its own
tables (step 2) alongside this one, exactly like `backend/plugins/gamedemo`
does.

## 2. Define your own component

A critter has a `Diet` — which resource it produces — that isn't one of
gamekit's built-ins. Components are just `component.Store[T]` over a JSONB
table:

```go
type Diet struct {
    Resource   string  `json:"resource"`
    RatePerMin float64 `json:"rate_per_min"`
}
```

```sql
-- migrations/000001_critterfarm.up.sql
CREATE TABLE IF NOT EXISTS entity_diet (entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE, data JSONB NOT NULL DEFAULT '{}');
```

```go
dietStore := component.NewJSONStore[Diet](db.Store, "diet", "entity_diet")
p.kit.World.Register(dietStore)  // so systems can query "which entities have a diet"
```

## 3. Spawn from a data-driven template

Register the component with the composer so a template can declare it, then
load a template pack (a designer edits this JSON with no code changes):

```go
p.kit.Composer.RegisterComponent("diet", template.Init(dietStore))

templates := map[string]map[string]json.RawMessage{
    "chicken": {"diet": json.RawMessage(`{"resource":"eggs","rate_per_min":2}`)},
}
p.kit.Composer.LoadTemplates(templates)

chicken, _ := p.kit.Composer.Spawn(ctx, "chicken", ownerID)
```

## 4. Make it idle: a `TickSystem`

A system runs on an interval with automatic offline catch-up. It only needs
`Name`, `Interval`, `Query` (which components it needs), and `Tick`:

```go
type dietSystem struct {
    diet   component.Store[Diet]
    wallet *economy.Service
}

func (dietSystem) Name() string              { return "diet" }
func (dietSystem) Interval() time.Duration   { return time.Minute }
func (dietSystem) Query() []string           { return []string{"diet"} }

func (s dietSystem) Tick(ctx context.Context, id int64, _ *world.World, elapsed time.Duration) error {
    d, ok, err := s.diet.Get(ctx, id)
    if err != nil || !ok {
        return err
    }
    _, err = s.wallet.Earn(ctx, id, d.Resource, int64(d.RatePerMin*elapsed.Minutes()))
    return err
}

// in Init, after building dietStore:
p.kit.Scheduler.AddTick(dietSystem{diet: dietStore, wallet: p.kit.Economy})
```

Start it in `Start`:

```go
func (p *Plugin) Start(rc plugin.RunContext) error {
    p.kit.Scheduler.Run(rc.Context())
    return nil
}
```

That's an idle game: spawn a chicken, wait, its eggs accrue in `p.kit.Economy`,
with offline catch-up for free.

## 5. Extend via hooks — the part that matters for other developers

Every gamekit toolkit threads a value through a named `Filter` chain (adjust
before applying) or fires an `Action` chain (react after). This is how *your*
plugin extends without editing the plugin above. Say another developer wants a
"double eggs on weekends" event:

```go
hooks.AddFilter(h, economy.HookEarn, 10, func(ctx context.Context, c economy.Change) economy.Change {
    if c.Currency == "eggs" && isWeekend() {
        c.Amount *= 2
    }
    return c
})
```

That's it — no change to `critterfarm`. This is the same seam
`gamekit/idle.HookState` uses in `backend/plugins/buildings`: a *content*
plugin injecting its own inputs into a *framework* plugin's computation without
the framework knowing the content plugin exists. Read that pairing
(`backend/plugins/idlekit` + `backend/plugins/buildings`) once you're past this
guide — it's the fullest worked example of the pattern.

## 6. Register and run

```go
// cmd/critterfarm/main.go
k := kernel.New(cfg, log)
k.Register(core.NewPlugin(), &critterfarm.Plugin{})
k.Run(ctx)
```

`core` gives you `db`/`hub`/`cache`/etc — see [PLUGIN_GUIDE.md](PLUGIN_GUIDE.md)
for what it provides and the raw sdk plugin mechanics (routes, events, content
registry) gamekit itself is built on.

## Where to go from here

| You want | Look at |
|---|---|
| Stats, combat, an economy, a relation graph — all wired together | `backend/plugins/gamedemo` |
| An idle/activity system + a content plugin extending it via hooks | `backend/plugins/idlekit` + `backend/plugins/buildings` |
| The full toolkit reference | [`gamekit/README.md`](../gamekit/README.md) |
| Porting an existing (non-gamekit) game onto gamekit | [MIGRATION_TEMPLATE.md](MIGRATION_TEMPLATE.md) |
| The design rationale behind the whole framework | [FRAMEWORK_DESIGN.md](FRAMEWORK_DESIGN.md) |
