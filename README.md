# zzrpg

A **batteries-included game framework** for building idle / RPG / RTS /
city-builder games in Go — plus a complete RPG built on it. Three Go modules,
each a stable layer the one above depends on:

| Module | Path | What it is |
|--------|------|------------|
| **sdk** | [`sdk/`](sdk/) → `.../zzrpg/sdk` | The game-**agnostic** engine substrate: plugin lifecycle, DI registry, event bus, hooks, HTTP router, outbox. Zero game concepts. |
| **gamekit** | [`gamekit/`](gamekit/) → `.../zzrpg/gamekit` | The batteries-included game **framework**: entity, component, world, stats, progression, inventory, economy, relation, system, template, idle, loot, quest, vitals — genre-neutral toolkits with hook seams, assembled by `kit.New`. **This is what you build a new game on.** |
| **backend** | [`backend/`](backend/) → `.../zzrpg/backend` | This repo's own game: a full RPG (`cmd/server`) and a reference example (`cmd/gamedemo`) exercising every gamekit toolkit, both built on gamekit. |

## Building your own game

Start here, in order:

1. [`gamekit/README.md`](gamekit/README.md) — the toolkit reference.
2. [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md) — a from-scratch
   walkthrough (spawn from a template, an idle system, extending via hooks).
3. `backend/plugins/gamedemo` — the fuller worked example (stats, combat,
   economy, a relation graph, offline+online accrual), runnable via
   `cmd/gamedemo`.
4. `backend/plugins/idlekit` + `backend/plugins/buildings` — the clearest
   example of the framework/content split: idlekit owns the accrual mechanism,
   buildings is an independent plugin that extends it (new activities, its own
   inputs) with **zero changes to idlekit**.

Porting an *existing* game onto gamekit? See
[`docs/MIGRATION_TEMPLATE.md`](docs/MIGRATION_TEMPLATE.md) — this repo's own
RPG was migrated subsystem by subsystem, and that document records the
patterns (and when *not* to migrate something).

## This repo's RPG

`backend/cmd/server` — auth, characters, combat (via an embedded Rust stat
engine, `zzstat`), inventory, crafting, and idle progression (activities +
buildings), all on gamekit.

## Quickstart

Prerequisites: Go 1.26+, PostgreSQL and Redis (see [`docker-compose.yml`](docker-compose.yml)),
and — for the RPG's combat only — the Rust [zzstat](https://github.com/singoesdeep/zzstat)
FFI library.

```bash
# infrastructure
docker compose up -d          # postgres + redis

# --- this repo's RPG (needs the zzstat .so) ---
cd backend
ZZSTAT_LIB_PATH=/path/to/libzzstat_ffi.so go run ./cmd/server
# → http://localhost:8080  (migrations run on boot)

# --- the gamekit reference example (no zzstat, no RPG-specific code) ---
PORT=8082 go run ./cmd/gamedemo
# → http://localhost:8082
```

Configuration is via environment variables — see [`backend/.env.example`](backend/.env.example).
In `ENV=production` the server requires a real `JWT_SECRET` (≥32 chars),
`DATABASE_URL`, and an `ALLOWED_ORIGINS` allowlist.

## Further reading

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — the three layers, the sdk
  plugin lifecycle, and the extension points.
- [docs/PLUGIN_GUIDE.md](docs/PLUGIN_GUIDE.md) — the low-level sdk plugin
  mechanics gamekit itself is built on (schema, content types, routes, events).
- [docs/FRAMEWORK_DESIGN.md](docs/FRAMEWORK_DESIGN.md) — the design rationale
  behind gamekit.
- [sdk/README.md](sdk/README.md) — the sdk module on its own.
- [docs/wiki/](docs/wiki/) — the code-grounded living wiki.

## Testing

```bash
cd sdk     && go test ./...                              # engine + utils (standalone)
cd gamekit && go test ./...                              # framework (standalone)
cd backend && ZZSTAT_LIB_PATH=/path/to/.so go test ./... # this RPG + plugins
```

## Layout

```
sdk/                         engine substrate (own Go module)
  engine/  kernel · plugin · registry · bus · hooks · admin · idle · outbox · store · eventlog · eventstream
  pkg/     config · httpx · logger · metrics · cache
gamekit/                     game framework (own Go module, requires ../sdk)
  entity · component · world · stats · progression · inventory · economy ·
  relation · system · template · idle · loot · quest · vitals · kit
backend/                     this repo's RPG (own Go module, requires ../sdk and ../gamekit)
  game/       RPG-specific domains (character, combat, creature, session, …)
  platform/   infrastructure: socket · session · statclient · database
  plugins/    composition adapters: core · stat · auth · character · combat ·
              idlekit · buildings · crafting · gamedemo · …
  content/    data-driven content (JSON)
  examples/   standalone, unregistered example plugins (sdk-level hooks/events on this RPG — see their README)
  cmd/        server (this RPG) · gamedemo (the gamekit reference example)
```
