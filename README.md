# zzrpg

A **game-agnostic backend engine** for building idle / RPG / RTS games in Go,
plus example games built on it. The engine is a reusable SDK; a game is nothing
but a set of plugins registered with the kernel.

The repository is two Go modules:

| Module | Path | Contents |
|--------|------|----------|
| **SDK** | [`sdk/`](sdk/) → `github.com/singoesdeep/zzrpg/sdk` | The reusable engine (`engine/`) + utilities (`pkg/`). Zero game concepts. |
| **Game** | [`backend/`](backend/) → `github.com/singoesdeep/zzrpg/backend` | Game domains (`game/`), plugins (`plugins/`), infrastructure (`platform/`), content, and the runnable binaries. Depends on the SDK via a local `replace`. |

Two games already run on the same engine:

- **`backend/cmd/server`** — a full RPG: auth, characters, combat (via an
  embedded Rust stat engine), inventory, loot, quests, and a content-driven
  idle system with offline + real-time online progression.
- **`backend/cmd/citygame`** — a standalone idle city-builder assembled from
  just two plugins (`core` + `city`). It registers no RPG plugins and needs no
  Rust library — proof that unrelated games build on the engine plugin-only.

## Quickstart

Prerequisites: Go 1.26+, PostgreSQL and Redis (see [`docker-compose.yml`](docker-compose.yml)),
and — for the RPG only — the Rust [zzstat](https://github.com/singoesdeep/zzstat)
FFI library.

```bash
# infrastructure
docker compose up -d          # postgres + redis

# --- the RPG server (needs the zzstat .so) ---
cd backend
ZZSTAT_LIB_PATH=/path/to/libzzstat_ffi.so go run ./cmd/server
# → http://localhost:8080  (migrations run on boot)

# --- the city-builder (no zzstat, no RPG plugins) ---
PORT=8082 go run ./cmd/citygame
# → http://localhost:8082
```

Configuration is via environment variables — see [`backend/.env.example`](backend/.env.example).
In `ENV=production` the server requires a real `JWT_SECRET` (≥32 chars),
`DATABASE_URL`, and an `ALLOWED_ORIGINS` allowlist.

## What makes it an engine

The kernel drives a plugin lifecycle and owns only game-agnostic primitives —
a DI registry, a typed event bus, a synchronous hook pipeline, an HTTP router
with an activation gate, an outbox relay, and an offline-accrual framework. A
plugin ships its own **database schema**, **content types**, **routes**, and
**events** without touching the engine. See:

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — layers, lifecycle, and the
  extension points.
- [docs/PLUGIN_GUIDE.md](docs/PLUGIN_GUIDE.md) — build a game as plugins,
  worked through the city example.
- [sdk/README.md](sdk/README.md) — the SDK module.
- [docs/wiki/](docs/wiki/) — the code-grounded living wiki.

## Testing

```bash
cd sdk     && go test ./...                              # engine + utils (standalone)
cd backend && ZZSTAT_LIB_PATH=/path/to/.so go test ./... # game + plugins
```

## Layout

```
sdk/                         reusable engine SDK (own Go module)
  engine/  kernel · plugin · registry · bus · hooks · admin · idle · outbox · store · eventlog · eventstream
  pkg/     config · httpx · logger · metrics · cache
backend/                     the games (own Go module, requires ../sdk)
  game/       RPG domains (character, combat, loot, quests, inventory, idle, …)
  platform/   infrastructure: socket · session · statclient · database
  plugins/    composition adapters: core · stat · auth · character · combat · … · idle · city
  content/    data-driven content (JSON)
  cmd/        server (RPG) · citygame (city-builder)
```
