# Architecture Document: zzrpg Backend (EN)

This document outlines the high-level system architecture, modular monolith design, technology stack, and component communication patterns for `zzrpg`.

## 1. System Architecture Diagram

Below is the high-level architecture of `zzrpg`. The architecture is designed as a **Modular Monolith** for the Go backend, coupled with a specialized, highly optimized **Rust zzstat core engine** embedded directly via in-process FFI bindings for stat computation.

```mermaid
graph TD
    %% Clients
    Browser[Browser / Next.js Client]

    %% Gateway/API Layer
    subgraph Go Backend (Modular Monolith)
        API[Go API Server REST/WS]
        
        %% Internal Modules
        subgraph Internal Modules
            Auth[Auth Module]
            Char[Character Module]
            Inv[Inventory Module]
            Equip[Equipment Module]
            Combat[Combat Module]
            Skill[Skill Module]
            Quest[Quest Module]
            Guild[Guild Module]
            Econ[Economy Module]
            Loot[Loot Module]
        end
        
        %% Shared Core Packages
        Database[Database Pkg - pgx]
        RedisClient[Redis Client - Session/Cache/Locks]
        WS[WebSocket Manager]
        StatClient[Stat Client - FFI]
        EventBus[In-Memory Event Bus]
    end

    %% External Infrastructure
    DB[(PostgreSQL Database)]
    Redis[(Redis Cache & Broker)]
    RustStat[Rust zzstat Core Library]

    %% Connections
    Browser <-->|HTTPS / WSS| API
    
    %% Go Internal relations
    API --> Auth
    API --> Char
    API --> Inv
    API --> Equip
    API --> Combat
    API --> Skill
    API --> Quest
    API --> Guild
    API --> Econ
    API --> Loot
    
    %% Infrastructure access
    Internal Modules --> Database
    Internal Modules --> RedisClient
    Internal Modules --> WS
    Internal Modules --> StatClient
    Internal Modules --> EventBus
    
    Database <--> DB
    RedisClient <--> Redis
    StatClient <-->|FFI In-Process Calls| RustStat
```

---

## 2. Go Backend Module Structure

The Go backend code structure follows clean architecture, domain isolation, and modular monolith principles. Each domain is self-contained with its own repository, service, and transport handlers.

```
backend/
├── cmd/
│   └── server/
│       └── main.go           # Application entry point, dependency injection, and server startup
├── internal/
│   ├── auth/                 # User registration, authentication, JWT tokens
│   ├── character/            # Character creation, basic info, level/experience, offline progression
│   ├── inventory/            # Player inventories, item storage, moving items
│   ├── items/                # Item definitions (data-driven), stats modifiers
│   ├── equipment/            # Currently equipped items, slots validation
│   ├── combat/               # Dynamic combat loop, calculated via zzstat
│   ├── skills/               # Skill templates, skill levels, upgrades
│   ├── quests/               # Data-driven quest steps, progression, rewards
│   ├── guild/                # Guild creation, ranks, bank, guild stat bonuses
│   ├── economy/              # Gold/currencies, market transaction logging
│   ├── loot/                 # Probability-based loot tables, mob drop mechanics
│   ├── statclient/           # In-process FFI client loading and executing Rust zzstat core
│   ├── database/             # PostgreSQL connection pool configuration, migration runner
│   ├── events/               # Event publisher/subscriber for decoupling modules
│   └── websocket/            # Connection manager, hub, read/write pumps, game notifications
├── pkg/
│   ├── config/               # Configuration parsing via environment variables
│   ├── logger/               # Structured logging (slog/zap)
│   └── utils/                # General helpers (UUID, hashing, etc.)
├── go.mod
├── go.sum
```

### Module Boundaries and Dependency Injection
1. **Isolation**: Modules must not access another module's database tables directly. They should use public interfaces/services provided by other modules.
2. **Repository Pattern**: Database operations are abstracted behind interface repositories, making it testable and mockable.
3. **Domain Event Bus**: To prevent tightly coupled dependencies, modules communicate asynchronously where appropriate using `internal/events` (e.g., when a character levels up, the `Quest` module listens to reward quest progress without direct coupling inside the `Character` module).

---

## 3. Technology Stack

- **Primary Language**: Go (1.23+) for fast performance, low memory footprint, concurrency primitives, and simple syntax.
- **Database**: PostgreSQL (16+) using `pgx` as the driver. Heavy usage of JSONB columns to achieve a schema-less, data-driven game design without losing transactional integrity.
- **Cache & Message Broker**: Redis (7+) for tracking player online status, session storage, distributed locking (combat/trade locks), and WebSocket event pub/sub.
- **Stat Service**: Rust-based `zzstat` core engine compiled as a shared library (`libzzstat_ffi.so`) and embedded directly into Go using `purego` FFI bindings, eliminating network overhead.
- **WebSockets**: Standard or `gorilla/websocket` implementation to push real-time events (combat logs, chat, items gained) to the browser.
