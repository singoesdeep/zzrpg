# zzrpg engine SDK

`github.com/singoesdeep/zzrpg/sdk` — the reusable, **game-agnostic** engine
behind zzrpg, packaged as a standalone Go module. Zero game concepts; games are
built entirely as plugins on top of it.

## Contents

**`engine/`** — the framework core:

| Package | Responsibility |
|---------|----------------|
| `kernel` | Plugin lifecycle (topological Init → Start → Stop) + HTTP server + middleware chain |
| `plugin` | The plugin contract, the `Router` seam, and the `Migrator` interface |
| `registry` | Typed DI registry (`Key[T]`) and the generic content registry (`DefineContent[T]`) |
| `bus` | Typed, async, panic-isolated in-process event bus with Redis-Streams fanout |
| `hooks` | Synchronous filter/action extension points |
| `admin` | Plugin presentation contract + runtime activation (`StateManager`) |
| `idle` | Game-agnostic offline/online accrual framework (`Producer`, `Window`) |
| `outbox`, `store`, `eventlog`, `eventstream` | Transactional messaging & persistence seams |

**`pkg/`** — supporting utilities: `config`, `httpx` (middleware, JSON helpers),
`logger`, `metrics` (Prometheus), `cache` (Redis/no-op).

## Using it

A game is its own module that imports this one and registers plugins:

```go
import (
    "github.com/singoesdeep/zzrpg/sdk/engine/kernel"
    "github.com/singoesdeep/zzrpg/sdk/pkg/config"
)

cfg, _ := config.LoadConfig()
k := kernel.New(cfg, logger)
k.Register(core.NewPlugin(), &yourgame.Plugin{})
k.Run(ctx)
```

A plugin ships its own schema (`Migrator`), content types (content registry),
services, and routes/events — without touching this module. See
[../docs/PLUGIN_GUIDE.md](../docs/PLUGIN_GUIDE.md).

## Monorepo note

In this repository the game module (`../backend`) references the SDK with a
local `replace github.com/singoesdeep/zzrpg/sdk => ../sdk`, so both build
together without publishing. Two games already run on it: the RPG
(`backend/cmd/server`) and a standalone city-builder (`backend/cmd/citygame`)
that registers no RPG plugins and needs no native library.

```bash
cd sdk && go test ./...   # the SDK builds and tests standalone
```
