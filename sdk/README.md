# zzrpg engine SDK

`github.com/singoesdeep/zzrpg/sdk` — the reusable, **game-agnostic** engine
behind zzrpg, packaged as a standalone Go module. It contains zero game
concepts (no character, combat, gold, …); games are built entirely as plugins
on top of it.

## What's in here

- **`engine/`** — the framework core:
  - `kernel` — plugin lifecycle (topological Init → Start → Stop) + HTTP server
  - `plugin` — the plugin contract, `Router`, and the `Migrator` interface
  - `registry` — typed DI registry (`Key[T]`) + generic content registry
    (`DefineContent[T]`)
  - `bus` — typed, async, in-process event bus (with Redis-Streams fanout)
  - `hooks` — synchronous filter/action extension points
  - `admin` — plugin presentation + runtime activation (`StateManager`)
  - `idle` — game-agnostic offline/online accrual framework (`Producer`)
  - `outbox`, `store`, `eventlog`, `eventstream` — transactional messaging
- **`pkg/`** — supporting utilities: `config`, `httpx`, `logger`, `metrics`,
  `cache`.

## Using it

A game is its own Go module that imports this one and registers plugins:

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

In this monorepo the game module (`../backend`) references the SDK with a local
`replace github.com/singoesdeep/zzrpg/sdk => ../sdk`. Two games already build on
it: the RPG (`backend/cmd/server`) and a standalone city-builder
(`backend/cmd/citygame`) that registers no RPG plugins and needs no zzstat.

A plugin ships its own schema (`Migrator`), its own content types (content
registry), and its own routes/events — without touching this module.
