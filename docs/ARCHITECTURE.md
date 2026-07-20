# Architecture

zzrpg is a plugin-first engine. The kernel contains **zero game concepts**; every
feature — even "core infrastructure" — is a plugin. This document describes the
layers, the plugin lifecycle, and the extension points a plugin uses.

## Layers

Dependencies point downward only; nothing lower imports anything higher.

```
plugins/   composition adapters — wire domains + platform into the kernel     (backend module)
   │
game/      the specific game's domains (character, combat, idle, city, …)     (backend module)
   │
platform/  domain-free infrastructure — socket, session, statclient, database (backend module)
   │
engine/    the game-agnostic framework — kernel, plugin, registry, bus, …     (SDK module)
pkg/       utilities — config, httpx, logger, metrics, cache                  (SDK module)
```

`engine/` + `pkg/` are a separate Go module (`.../zzrpg/sdk`) so a game can
import the engine as a dependency. `platform/` stays in the game module because
`statclient` couples to game content; it depends on the SDK like any consumer.

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
game maps that onto its systems. The RPG uses producers for combat stages,
gathering lifeskills, and RTS resource generators; the city game uses one for
its buildings. Both offline (on login) and online (a periodic tick) accrual run
through the same `Producer` + `Window` primitives.

## Request & event flow (RPG example)

```
Client ──HTTP──▶ middleware chain (recover · request-id · log · secure-headers ·
                 CORS · rate-limit · body-cap) ──▶ plugin route ──▶ service ──▶ store
                                                                      │
Client ──WS───▶ /ws (authenticator) ──▶ MessageRouter ──▶ owning plugin's handler
                                                                      │
                              domain event ──▶ outbox (same tx) ──▶ relay ──▶ bus ──▶ subscribers
```
