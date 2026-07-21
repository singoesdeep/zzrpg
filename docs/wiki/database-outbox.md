<!-- sha: 1ec913a58ecaea4aad8d0f10d528442c4922f119 -->
# 💾 Persistence, Outbox & Migrations

## Store seam
Repositories depend on `store.Store`/`Querier` (`sdk/engine/store/store.go`),
satisfied by both the pgx pool and a transaction, so a method runs standalone or
inside `WithinTx` and is fakeable in tests.

## Transactional outbox
Domain events are written to an `outbox` table in the **same transaction** as the
state change (`sdk/engine/outbox/relay.go`), then a relay publishes them to the
bus — no dual-write inconsistency. An `event_log` supports away/replay
(`sdk/engine/eventlog`), and a Redis-Streams consumer fans events across nodes
(`sdk/engine/eventstream`).

## Migrations (per-module)
Plain numbered SQL applied on boot (`backend/platform/database/migrations.go`),
tracked per module in `schema_migrations(module, version)`. Core migrations live
in `backend/platform/database/migrations`; plugins ship their own via
`plugin.Migrator` (e.g. `backend/plugins/{idle,city}/migrations`), namespaced so
versions never collide.
