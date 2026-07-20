# zzrpg — Idle Online RPG Backend Engine

**zzrpg** is a **plugin-first, event-driven, data-driven backend engine** for idle
online RPGs. A small game-agnostic **kernel** wires domains together as plugins,
game rules and content live in tunable JSON packs (fed to an embedded Rust
`zzstat` formula core), and a typed event bus fans domain events across nodes for
horizontal scale. It runs on PostgreSQL and Redis, with a WebSocket + REST
gateway.

> Originally a single MMORPG monolith, zzrpg has been refactored (behavior-
> preserving, Strangler-Fig) into a reusable engine. The full architecture plan
> lives in [`docs/ENGINE_TRANSFORMATION_PLAN.md`](docs/ENGINE_TRANSFORMATION_PLAN.md).

---

## Why an engine?

- **Kernel + plugins** — the 8 game domains are plugins over a game-agnostic kernel
  (ordered `Init/Start/Stop`, topo-sorted by declared dependencies, DI registry).
  Adding a domain doesn't touch the core.
- **Data-driven content** — class stats, derived-stat coefficients, mob defs,
  combat formulas, idle economy and loot tables are embedded JSON packs
  (`backend/content/`). Designers tune numbers without editing Go.
- **Typed event bus + cross-node fan-out** — domains publish typed events; a
  Redis-Streams layer broadcasts them to every node so subscribers on any node
  react (analytics, achievements, aggro/AI, presence) with zero core edits.
- **Durable, atomic events** — a transactional **outbox** writes reward events in
  the same DB transaction as the state change; a relay dispatches them at-least-
  once. An append-only **event_log** enables replay (login catch-up).
- **Production-ready** — request-id correlation, per-IP rate limiting, security
  headers, body-size limits, Prometheus `/metrics`, a `/readyz` probe, login
  brute-force protection, and rotating refresh tokens.

---

## Technical Stack

- **Go backend** — kernel, plugins, DI registry, event bus, HTTP + WebSocket gateway.
- **Rust `zzstat`** — embedded stat/combat formula core, loaded in-process via
  `purego` FFI. Derived stats and combat (hit/crit/variance) are evaluated from
  JSON formulas, not hardcoded Go.
- **PostgreSQL 16+** — persistence via a `Store`/`UnitOfWork` seam over `pgx`;
  JSONB for data-driven fields; migrations run automatically at startup.
- **Redis 7+** — read-through cache and the cross-node event stream (graceful
  degradation: the app runs single-node without Redis).

> The Rust `zzstat` core lives in a sibling repo (`github.com/singoesdeep/zzstat`)
> and is consumed via its Go bindings — see the `replace` directive in
> `backend/go.mod`. Rebuild it with `cargo build --release -p zzstat-ffi` after
> Rust changes.

---

## Component Layout

```
backend/
├── cmd/server/        # main.go (kernel wiring) + plugins.go (the 9 standalone domain plugins)
├── engine/            # game-agnostic engine core (zero RPG concepts)
│   ├── kernel/        # lifecycle: topo-sorted Init/Start/Stop, HTTP server, shutdown
│   ├── plugin/        # Plugin contract + Init/Run contexts
│   ├── registry/      # typed dependency-injection registry
│   ├── bus/           # typed event bus + Fanout (cross-node broadcast)
│   ├── hooks/         # synchronous hook pipeline (filters & veto actions)
│   ├── store/         # Store/UnitOfWork (Querier + WithinTx) persistence seam over pgx
│   ├── outbox/        # transactional outbox: Append (in-tx) + Relay (dispatch)
│   ├── eventstream/   # Redis-Streams cross-node event fan-out
│   └── eventlog/      # append-only per-stream history + replay
├── internal/          # game domain implementations
│   ├── auth/          # register/login, JWT, refresh-token rotation, brute-force guard
│   ├── character/     # stats, leveling, progression events
│   ├── idle/          # offline progression domain service (consumed by idlePlugin)
│   ├── combat/        # data-driven damage (via zzstat), exactly-once kills, events
│   ├── inventory/     # slots, equip→recalc, per-character locks
│   ├── items/ loot/   # item definitions, probability loot tables
│   ├── quests/        # multi-step quests, rewards, lifecycle events
│   ├── killreward/    # kill side effects (loot/quest/reward) behind a consumer interface
│   ├── session/       # in-memory combat/health session registry (domain state)
│   ├── socket/        # WebSocket hub, client, message router (transport)
│   ├── statclient/    # in-process FFI client to the Rust zzstat core
│   └── database/      # pgx pool + embedded SQL migrations
├── content/           # embedded JSON content packs (classes, formulas, mobs, idle, loot)
└── pkg/               # config, cache (Redis/Noop), httpx (middleware), metrics (Prometheus)
```

---

## Highlights

1. **Domain-Agnostic Kernel + Plug-and-Play Plugins** — `backend/engine` has zero RPG concepts. Domains (`character`, `inventory`, `idle`, `combat`, `quests`) register as independent plugins with declared `Requires` dependencies. Omit or drop in a plugin (e.g. `idlePlugin`) without touching core or other plugins.
2. **Data-driven everything** — class base stats, derived-stat coefficients, mob definitions, the combat damage formula, idle/offline economy, and loot fallback tables are all JSON.
3. **Embedded Rust stat core** — HP/MP/ATTACK/DEFENSE/CRIT_RATE and combat rolls (accuracy, crit, ±variance) are computed in-process from JSON formulas via `zzstat`.
4. **Event catalog + multi-node fan-out** — combat/character/quest/inventory/loot events on a typed bus, broadcast across nodes over Redis Streams.
5. **Transactional outbox + event_log replay** — reward events are atomic with their write and replayable for reconnect catch-up.
6. **Real-time WebSockets** — session registry, chat, combat broadcasts.
7. **Standalone Idle progression** — `idlePlugin` reacts asynchronously to `CharacterLoggedIn` events to compute STR/INT-scaled offline gold/exp and loot rolls (tunable in `content/idle/offline.json`) with zero coupling in `characterPlugin`.
8. **Production hardening** — rate limiting, security headers, request-id, Prometheus metrics, readiness probe, brute-force protection, refresh tokens.

---

## API (REST)

### Auth (public)
- `POST /api/v1/auth/register` — register a user.
- `POST /api/v1/auth/login` — returns an access token + rotating refresh token.
- `POST /api/v1/auth/refresh` — exchange a refresh token for a new pair (single-use).
- `POST /api/v1/auth/logout` — revoke a refresh token.

### Characters / Inventory / Quests / Loot / Items (protected; admin where noted)
- `POST|GET /api/v1/characters`, `GET /api/v1/characters/{id}`, `.../{id}/stats`
- `GET /api/v1/characters/{id}/inventory`, `POST /api/v1/inventory/move`,
  `POST /api/v1/admin/inventory/add`
- `GET /api/v1/quests`, `POST /api/v1/characters/{id}/quests/accept`,
  `GET /api/v1/characters/{id}/quests`, `POST /api/v1/admin/quests[ /progress ]`
- `POST|GET /api/v1/admin/loot`
- `POST|PUT|GET|DELETE /api/v1/admin/items[ /{id} ]`

### Operational
- `GET /health` — liveness (DB ping).
- `GET /readyz` — readiness (DB hard dependency; Redis reported, soft).
- `GET /metrics` — Prometheus metrics (HTTP rate/latency, Go runtime, outbox backlog).

### WebSocket (`ws://localhost:8080/ws?token=<JWT>`)
Client → server: `SELECT_CHARACTER`, `COMBAT_ATTACK`, `CHAT`.
Server → client: `SELECT_CHARACTER_ACK`, `OFFLINE_GAINS`, `AWAY_EVENTS`,
`COMBAT_DAMAGE`, `CHAT`, `COMBAT_ERROR`.

---

## Configuration (env)

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DATABASE_URL` | local pg | PostgreSQL DSN |
| `REDIS_URL` | local redis | Redis (cache + event stream); optional |
| `JWT_SECRET` | dev default | required (≥32 chars) when `ENV=production` |
| `ENV` | `development` | `production` enforces secure config |
| `ACCESS_TOKEN_TTL` | `15m` | access-token lifetime |
| `REFRESH_TOKEN_TTL` | `720h` | refresh-token lifetime |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `20` / `40` | per-IP HTTP rate limit (`0` disables) |
| `MAX_BODY_BYTES` | `1048576` | max request body |
| `OUTBOX_RETENTION` | `24h` | how long dispatched outbox rows are kept |
| `ZZRPG_CONTENT_DIR` | (embedded) | directory whose JSON packs override the embedded defaults (per file) |

---

## Getting Started

```bash
# 1. Infrastructure (PostgreSQL + Redis via Podman/Docker)
./scripts/start-infra.sh

# 2. Build the Rust zzstat shared library (sibling repo)
cd ../zzstat && cargo build --release -p zzstat-ffi   # -> target/release/libzzstat_ffi.so

# 3. Run the backend (migrations run automatically at startup)
cd backend
ZZSTAT_LIB_PATH=../../zzstat/target/release/libzzstat_ffi.so go run ./cmd/server
# API docs: http://localhost:8080/docs   |   metrics: http://localhost:8080/metrics

# 4. Tests (unit + race + live-PG/Redis integration when infra is up)
cd backend && go test -race ./...
```

---

## Documentation

| Doc | Topic |
|---|---|
| [`docs/PLUGIN_GUIDE.md`](docs/PLUGIN_GUIDE.md) | **Writing plugins** — extension points, hooks, events, example |
| [`docs/ENGINE_TRANSFORMATION_PLAN.md`](docs/ENGINE_TRANSFORMATION_PLAN.md) | Full engine architecture & roadmap |
| [`docs/ARCHITECTURE_EN.md`](docs/ARCHITECTURE_EN.md) / [`_TR`](docs/ARCHITECTURE_TR.md) | System architecture |
| [`docs/DATABASE_DESIGN_EN.md`](docs/DATABASE_DESIGN_EN.md) / [`API_DESIGN_EN.md`](docs/API_DESIGN_EN.md) | Schema & API |
| [`docs/COMBAT_DESIGN_EN.md`](docs/COMBAT_DESIGN_EN.md) / [`STAT_SYSTEM_EN.md`](docs/STAT_SYSTEM_EN.md) | Combat & stats |
