# Plugin Guide — the sdk substrate a plugin runs on

> **Building a game?** This document is about the low-level plugin mechanics —
> how anything (infrastructure or game logic) hooks into the kernel. If you're
> building actual game systems, start with
> [gamekit/README.md](../gamekit/README.md) and
> [GETTING_STARTED.md](GETTING_STARTED.md) instead — gamekit gives you entities,
> stats, economy, and the rest of the toolkits, all pre-wired on top of the
> mechanics below. `core`, `auth`, and `items` are examples of plugins built
> directly on this layer, because they're infrastructure, not game mechanics;
> `idlekit`, `buildings`, and `backend/plugins/gamedemo` are examples built on
> gamekit instead.

A plugin can ship its **own schema, content types, services, HTTP/WS routes,
events, and idle mechanics** without touching the engine or platform. This guide
walks through the mechanics using `backend/plugins/gamedemo` — a complete game
built on gamekit, with no characters, combat, or Rust stat library.

## 1. The skeleton

```go
type Plugin struct{ plugin.Base }  // Base gives no-op Start/Stop

func (*Plugin) Meta() plugin.Meta {
    return plugin.Meta{Name: "gamedemo", Requires: []string{"core"}}
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
k.Register(core.NewPlugin(), &gamedemo.Plugin{})
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
    return plugin.MigrationSource{Module: "gamedemo", FS: fs.FS(migrationsFS), Dir: "migrations"}
}
```

If you're on gamekit, `kit.MigrationSource()` ships the standard schema
(entities + built-in components) for you — your plugin's own migration only
needs to add its own tables.

## 3. Define your own content type

The engine has a generic content registry. Declare a type it knows nothing
about, load your embedded JSON through it, and read it back type-safely. This is
what `gamekit/template.Composer` and `backend/plugins/crafting`'s recipe pack
are built on.

```go
type BuildingDef struct { ID, Name, Resource string; BasePerMin, PerLevel float64; Cost map[string]int64 }

registry.DefineContent[BuildingDef](reg, "my_building")
reg.LoadContent("my_building", id, rawJSON)          // per entry
def, ok := registry.Content[BuildingDef](reg, "my_building", "gold_mine")
```

Designers tune a JSON content pack; no code changes.

## 4. Reuse the idle accrual framework

The raw mechanism is `sdk/engine/idle`: implement `idle.Producer` — elapsed
minutes + state in, an opaque `Output` (ledger + drops) out — and drive it
through `idle.Window` for the offline-catch-up clamp. **On gamekit, you don't
touch this directly** — `gamekit/idle.Engine` + `TickSystem` already do the
window/producer/apply wiring and route Output into economy/progression/
inventory; see `backend/plugins/buildings` for a worked Producer plugin
(registers on the shared registry, injects its own inputs via `HookState`) and
`gamekit/README.md`'s `idle` row.

## 5. Expose services and routes

Provide your service (optionally with a type-safe key) and register routes on
the plugin-scoped mux — they are automatically gated on the plugin's activation.

```go
var ServiceKey = registry.NewKey[*Service]("gamedemo")
registry.ProvideKey(reg, ServiceKey, svc)

mux := ic.Mux()
mux.HandleFunc("POST /api/v1/demo/spawn/{kind}", p.spawn)
mux.HandleFunc("GET /api/v1/demo/entity/{id}", p.getEntity)
```

For WebSocket messages, resolve `*socket.MessageRouter` and use
`HandleOwned(type, "gamedemo", handler)` so the type is gated on activation.

## 6. React to events (optional)

Subscribe to domain events on the plugin-scoped bus. Publishing goes through the
outbox for exactly-once delivery when done inside a repository transaction.

```go
ic.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx, ev) { /* … */ })
```

## Worked examples of a third-party plugin

`backend/examples/plugins/{xpboost,achievements}` are standalone,
unregistered example plugins extending *this repo's specific RPG* through
hooks/events/routes (not gamekit) — a hook filter + veto + event subscription
in one (`xpboost`), and a purely event-driven stateful plugin (`achievements`).
See [their README](../backend/examples/plugins/README.md).

## What you did *not* touch

Building `gamedemo` required **no changes** to `sdk/` (engine) or `gamekit/`
(framework). It boots without the Rust stat library because it never registers
the `stat` plugin and gamekit's stats toolkit uses a pure-Go resolver. That is
the whole point: the engine and the framework are stable, versioned
dependencies, and a game lives entirely in its own plugins.

## Reference

- Building actual game systems → [gamekit/README.md](../gamekit/README.md),
  [GETTING_STARTED.md](GETTING_STARTED.md)
- Porting an existing game onto gamekit → [MIGRATION_TEMPLATE.md](MIGRATION_TEMPLATE.md)
- Engine internals & extension points → [ARCHITECTURE.md](ARCHITECTURE.md)
- The SDK module → [../sdk/README.md](../sdk/README.md)
- Living, code-grounded wiki → [wiki/](wiki/)
