# Architecture

zzrpg is three layers, each a separate Go module, dependencies pointing strictly
downward:

```
backend/   THIS game: plugins (composition), game/ (RPG-specific domains),      (backend module)
           platform/ (socket, session, statclient, database), cmd/ (binaries)
   │
gamekit/   the batteries-included GAME framework: entity, component, world,     (gamekit module)
           stats, progression, inventory, economy, relation, system, template,
           idle, loot, quest, vitals, kit — genre-neutral toolkits with hook
           seams. See gamekit/README.md.
   │
sdk/       the game-AGNOSTIC engine substrate: kernel, plugin, registry, bus,   (sdk module)
           hooks, admin, outbox, store. Contains zero game concepts — it has
           no idea what an "entity" or "hit points" is.
```

The kernel (sdk) contains **zero game concepts**; it drives a plugin lifecycle
and owns only infrastructure primitives (DI registry, event bus, hook pipeline,
HTTP router, outbox). **gamekit** sits on top of it and provides the actual game
systems as a rich, hooked core — this is the layer a new game builds on. A
plugin can be written directly against the sdk (as `core`/`auth`/`items` are,
because they're infrastructure, not game mechanics) or against gamekit (as
`idlekit`/`buildings`/`gamedemo` are, because they're game systems) — pick the
lowest layer that has what you need.

**Building your own game?** Start with [gamekit/README.md](../gamekit/README.md)
and [GETTING_STARTED.md](GETTING_STARTED.md), not this document — this one is
about the sdk substrate gamekit itself is built on. `backend/plugins/gamedemo`
is the worked example exercising every gamekit toolkit; `backend/cmd/server` is
this repo's own RPG, also built on gamekit (see
[MIGRATION_TEMPLATE.md](MIGRATION_TEMPLATE.md) for how its subsystems moved
onto gamekit's toolkits over time).

## The kernel and the plugin lifecycle

`kernel.New(cfg, log)` builds the game-agnostic primitives (registry, event bus,
hooks, HTTP mux, metrics). Plugins are added with `Register` and driven by
`Run`:

1. **Topological sort** — plugins declare hard dependencies by name in
   `Meta().Requires`; the kernel orders them and fails fast on a missing
   dependency or a cycle.
2. **Migrations** — the kernel collects each plugin's `MigrationSource` and
   publishes them so the persistence plugin applies core + plugin schema.
3. **Init** (in order) — a plugin registers services, content, routes, and event
   subscriptions. When a plugin's `Init` runs, every plugin it requires has
   already initialised.
4. **Start** (in order) — background work begins (hub loop, tickers, the outbox
   relay). All services are present by now.
5. Serve HTTP until the context is cancelled, then **Stop** in reverse order.

```go
type Plugin interface {
    Meta() Meta
    Init(InitContext) error
    Start(RunContext) error
    Stop(context.Context) error
}
```

`InitContext` is a plugin's sole channel to the engine: `Registry()`, `Bus()`,
`Hooks()`, `Mux()`, `Config()`, `Logger()`, `Context()`.

## Extension points

Everything a plugin needs is reached through the context or a resolved service.

| Concern | Mechanism |
|---------|-----------|
| **Services (DI)** | `registry.Provide`/`Resolve`, or type-safe `registry.Key[T]` + `ProvideKey`/`ResolveKey`. |
| **HTTP routes** | `InitContext.Mux()` returns a plugin-scoped `Router`; routes are gated on the plugin's activation (503 when disabled). |
| **WebSocket messages** | `socket.MessageRouter.HandleOwned(type, owner, handler)`; owned types are skipped while the owner is deactivated. |
| **Events** | `Bus().Subscribe(name, handler)` / `Publish` — async, panic-isolated, with cross-node fanout over Redis Streams. The bus is plugin-scoped, so a deactivated plugin's subscriptions are suppressed. |
| **Hooks** | Synchronous `Filters` (transform a value mid-flow) and `Actions` (ordered side effects / veto gates). |
| **Database schema** | Implement `plugin.Migrator` → `MigrationSource{Module, FS, Dir}`; the plugin owns its versioned migrations, namespaced by module. |
| **Content** | `registry.DefineContent[T](reg, kind)` + `reg.LoadContent(kind, id, raw)` + `registry.Content[T]` — declare a data-driven content type the engine knows nothing about. |
| **Idle progression** | Implement `idle.Producer` (turns elapsed time + state into output) and drive it with the accrual framework. |
| **Admin UI** | Implement `admin.Describor` to appear in the Admin Dashboard; `admin.StateManager` toggles activation at runtime. |

## The activation gate

Plugins can be enabled/disabled at runtime from the Admin Dashboard, and the
kernel enforces it uniformly so no plugin has to check its own state:

- **HTTP** — the plugin-scoped `Mux()` wraps handlers; a deactivated plugin's
  routes return **503**.
- **Events** — the plugin-scoped `Bus()` suppresses that plugin's
  subscriptions.
- **WebSocket** — the message router skips message types owned by a deactivated
  plugin.

## Persistence and messaging

- **Store seam** — repositories depend on `store.Store`/`Querier` (satisfied by
  both the pool and a transaction), so a method runs standalone or inside
  `WithinTx` and is fakeable in tests.
- **Transactional outbox** — domain events are written to an `outbox` table in
  the same transaction as the state change; a relay publishes them to the bus,
  eliminating dual-write inconsistency. An `event_log` supports replay.
- **Migrations** — plain numbered SQL, applied on boot, tracked per module in
  `schema_migrations(module, version)`.

## Idle accrual framework (`engine/idle`)

A game-agnostic engine for "what happened while away". A `Producer` turns
`elapsedMinutes + State` into an opaque `Output` (a numeric ledger + drops); the
game maps that onto its systems. `gamekit/idle` is the integration layer built
on this — an Engine + Assignment component + TickSystem that routes a
Producer's Output into gamekit's economy/progression/inventory toolkits (see
`gamekit/README.md`). This repo's RPG uses it for training/gathering activities
and a `buildings` content plugin (combat stages and gathering lifeskills, as
developer-supplied Producers). Both offline (on login) and online (a periodic
tick) accrual run through the same `Producer` + `Window` primitives.

## Request & event flow (RPG example)

```
Client ──HTTP──▶ middleware chain (recover · request-id · log · secure-headers ·
                 CORS · rate-limit · body-cap) ──▶ plugin route ──▶ service ──▶ store
                                                                      │
Client ──WS───▶ /ws (authenticator) ──▶ MessageRouter ──▶ owning plugin's handler
                                                                      │
                              domain event ──▶ outbox (same tx) ──▶ relay ──▶ bus ──▶ subscribers
```
