# Plugin Guide — building a game

A game on this engine is a set of plugins. A plugin can ship its **own schema,
content types, services, HTTP/WS routes, events, and idle mechanics** without
touching the engine or platform. This guide walks through it using the city
builder ([`backend/plugins/city`](../backend/plugins/city)) — a complete game
assembled from just `core` + `city`, with no characters, combat, or Rust stat
library.

## 1. The skeleton

```go
type Plugin struct{ plugin.Base }  // Base gives no-op Start/Stop

func (*Plugin) Meta() plugin.Meta {
    return plugin.Meta{Name: "city", Requires: []string{"core"}}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
    reg := ic.Registry()
    db := registry.MustResolve[*database.DB](reg, "db") // provided by core
    // … build services, register routes …
    return nil
}
```

Register it in a `main`:

```go
k := kernel.New(cfg, log)
k.Register(core.NewPlugin(), &city.Plugin{})
k.Run(ctx)
```

`core` provides the shared infrastructure services (`db`, `hub`, `msgRouter`,
`cache`, `session`, `eventDecoders`). It is **domain-agnostic** — it imports no
game code — so a game only pulls in what it registers.

## 2. Ship your own schema (`Migrator`)

Put versioned SQL under the plugin and implement `Migrator`. The engine collects
it before `Init` and the persistence plugin applies it, namespaced by module —
no edits to `platform/database`.

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS

func (*Plugin) Migrations() plugin.MigrationSource {
    return plugin.MigrationSource{Module: "city", FS: fs.FS(migrationsFS), Dir: "migrations"}
}
```

`migrations/000001_create_city.up.sql` creates the game's tables. On boot you'll
see `Applying migration module=city version=1`.

## 3. Define your own content type

The engine has a generic content registry. Declare a type it knows nothing
about, load your embedded JSON through it, and read it back type-safely.

```go
type BuildingDef struct { ID, Name, Resource string; BasePerMin, PerLevel float64; Cost map[string]int64 }

registry.DefineContent[BuildingDef](reg, "city_building")
reg.LoadContent("city_building", id, rawJSON)          // per entry
def, ok := registry.Content[BuildingDef](reg, "city_building", "gold_mine")
```

Designers tune `content/buildings.json`; no code changes.

## 4. Reuse the idle accrual framework

Implement `idle.Producer` for your mechanic. The city's buildings scale their
output with their level:

```go
type buildingProducer struct{ def BuildingDef }
func (buildingProducer) Unlocked(idle.State) bool { return true }
func (p buildingProducer) Produce(elapsedMin float64, s idle.State, _ func() float64) idle.Output {
    rate := p.def.BasePerMin + p.def.PerLevel*s.Get("level")
    var o idle.Output; o.Add(p.def.Resource, int64(rate*elapsedMin)); return o
}
```

Then, on "collect", clamp the elapsed window and run the producers:

```go
elapsedMin, ok := idle.Window(time.Since(lastTick).Seconds(), minSec, capSec)
// run each built producer, merge Output, credit resources, advance the tick
```

The RPG uses the same contract for combat stages (scale on power), lifeskills
(scale on skill level), and generators — and adds a periodic online tick.

## 5. Expose services and routes

Provide your service (optionally with a type-safe key) and register routes on
the plugin-scoped mux — they are automatically gated on the plugin's activation.

```go
var ServiceKey = registry.NewKey[*Service]("city")
registry.ProvideKey(reg, ServiceKey, svc)

mux := ic.Mux()
mux.HandleFunc("POST /api/v1/city/{owner}/found", p.foundHandler)
mux.HandleFunc("GET /api/v1/city/{owner}", p.stateHandler)
```

For WebSocket messages, resolve `*socket.MessageRouter` and use
`HandleOwned(type, "city", handler)` so the type is gated on activation.

## 6. React to events (optional)

Subscribe to domain events on the plugin-scoped bus. Publishing goes through the
outbox for exactly-once delivery when done inside a repository transaction.

```go
ic.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx, ev) { /* … */ })
```

## What you did *not* touch

Building the city game required **no changes** to `sdk/` (engine) or
`backend/platform/`. It boots without the Rust stat library because it never
registers the `stat` plugin. That is the whole point: the engine is a stable SDK,
and games live entirely in plugins.

## Reference

- Engine internals & extension points → [ARCHITECTURE.md](ARCHITECTURE.md)
- The SDK module → [../sdk/README.md](../sdk/README.md)
- Living, code-grounded wiki → [wiki/](wiki/)
