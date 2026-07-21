<!-- sha: de0c8047441434948fa45002bc4aaf20b005c6d1 -->
# 🏛️ Engine Core & Kernel

The engine is a game-agnostic Go module (`sdk/`) with zero RPG concepts. Games
are plugins registered with the kernel.

## Kernel lifecycle
The kernel (`sdk/engine/kernel/kernel.go`) topologically sorts plugins by
`Meta().Requires`, then runs **Init → Start**, serves HTTP until the context is
cancelled, and **Stops** in reverse. Before Init it collects each plugin's
`MigrationSource` and applies core + plugin schema.

## Primitives (all game-agnostic)
- **Registry** (`sdk/engine/registry`) — typed DI: `Provide[T]`/`Resolve[T]`, plus
  `Key[T]` for compile-time-safe keys and a generic content registry
  (`DefineContent[T]`/`LoadContent`).
- **Event bus** (`sdk/engine/bus`) — async, panic-isolated, `Fanout`-wrapped for
  cross-node delivery over Redis Streams.
- **Hooks** (`sdk/engine/hooks`) — synchronous Filters (mutate a value) and
  Actions (ordered gates).
- **Idle** (`sdk/engine/idle`) — offline/online accrual: `Producer` + `Window`.
- **Admin** (`sdk/engine/admin`) — presentation contract + runtime `StateManager`.

## Plugin-scoped context & the activation gate
Each plugin gets an `InitContext`/`RunContext` scoped to it: its `Mux()` returns
a gated `Router` (503 when the plugin is disabled) and its `Bus()` suppresses
subscriptions while disabled — so activation is enforced uniformly by the kernel.

## Module split
`sdk/` (engine + pkg) is a standalone module; `backend/` (game + platform +
plugins) imports it via `replace => ../sdk`.
