<!-- sha: cd8d48cc325f6b112923be8a742343189c59af5e -->
# 💾 Database, Outbox & Event Sourcing

The persistence layer uses PostgreSQL 16 with `pgxpool` and enforces strict transactional integrity via the `Store` and `UnitOfWork` seams.

## 1. Store & UnitOfWork Seam

- **`database.DB`:** Wraps connection pool and exposes `Store` interface ([backend/platform/database/database.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/platform/database/database.go)).
- **Unit of Work:** All multi-entity modifications (e.g. updating character gold while granting loot item to inventory) run inside single PostgreSQL transactions (`BeginTx` -> `Commit` / `Rollback`).

## 2. Transactional Outbox Pattern (`outbox`)

To prevent dual-write inconsistencies between PostgreSQL and Redis Streams or WebSocket consumers, domain events are written to the `outbox` database table within the same ACID transaction:

- **Relay Worker:** `outbox.Relay` ([backend/engine/outbox/relay.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/engine/outbox/relay.go#L1-L100)) continuously polls undispatched rows and publishes them to the kernel `bus`.
- **Event Log Replay:** Historical events are stored in `event_log` table for auditability and event replay debugging ([backend/engine/eventlog/](file:///home/singo/github.com/singoesdeep/zzrpg/backend/engine/eventlog/)).

## 3. Grounding & Code References

- Store Interface & DB Wrapper: [platform/database/database.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/platform/database/database.go)
- Outbox Relay: [engine/outbox/relay.go:L1-L100](file:///home/singo/github.com/singoesdeep/zzrpg/backend/engine/outbox/relay.go#L1-L100)
- Event Log Store: [engine/eventlog/store.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/engine/eventlog/store.go)
