---
name: character
description: "Skill for the Character area of zzrpg. 31 symbols across 14 files."
---

# Character

31 symbols | 14 files | Cohesion: 71%

## When to Use

- Working with code in `backend/`
- Understanding how UsernameFromContext, NewUserRepository, NewCharacterRepository work
- Modifying character-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `backend/internal/character/service.go` | RecalculateStats, SetEquipmentProvider, Create, GetByID, ListByUserID (+4) |
| `backend/internal/character/handler.go` | CreateHandler, ListHandler, GetHandler, GetStatsHandler, writeError |
| `backend/internal/character/character_test.go` | newMockCharacterRepository, TestCreateCharacter, TestCharacterLimit |
| `backend/internal/auth/middleware.go` | UsernameFromContext, UserIDFromContext |
| `backend/internal/database/database.go` | NewConnectionPool, Close |
| `backend/pkg/config/config.go` | LoadConfig, getEnv |
| `backend/cmd/server/main.go` | main |
| `backend/internal/auth/repository.go` | NewUserRepository |
| `backend/internal/character/repository.go` | NewCharacterRepository |
| `backend/internal/database/migrations.go` | RunMigrations |

## Entry Points

Start here when exploring this area:

- **`UsernameFromContext`** (Function) — `backend/internal/auth/middleware.go:64`
- **`NewUserRepository`** (Function) — `backend/internal/auth/repository.go:15`
- **`NewCharacterRepository`** (Function) — `backend/internal/character/repository.go:17`
- **`NewConnectionPool`** (Function) — `backend/internal/database/database.go:17`
- **`NewInventoryRepository`** (Function) — `backend/internal/inventory/repository.go:16`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `UsernameFromContext` | Function | `backend/internal/auth/middleware.go` | 64 |
| `NewUserRepository` | Function | `backend/internal/auth/repository.go` | 15 |
| `NewCharacterRepository` | Function | `backend/internal/character/repository.go` | 17 |
| `NewConnectionPool` | Function | `backend/internal/database/database.go` | 17 |
| `NewInventoryRepository` | Function | `backend/internal/inventory/repository.go` | 16 |
| `NewItemRepository` | Function | `backend/internal/items/repository.go` | 16 |
| `LoadConfig` | Function | `backend/pkg/config/config.go` | 17 |
| `NewLogger` | Function | `backend/pkg/logger/logger.go` | 7 |
| `UserIDFromContext` | Function | `backend/internal/auth/middleware.go` | 56 |
| `CreateHandler` | Function | `backend/internal/character/handler.go` | 27 |
| `ListHandler` | Function | `backend/internal/character/handler.go` | 74 |
| `GetHandler` | Function | `backend/internal/character/handler.go` | 103 |
| `GetStatsHandler` | Function | `backend/internal/character/handler.go` | 142 |
| `TestCreateCharacter` | Function | `backend/internal/character/character_test.go` | 93 |
| `TestCharacterLimit` | Function | `backend/internal/character/character_test.go` | 142 |
| `NewCharacterService` | Function | `backend/internal/character/service.go` | 23 |
| `RecalculateStats` | Method | `backend/internal/character/service.go` | 13 |
| `SetEquipmentProvider` | Method | `backend/internal/character/service.go` | 14 |
| `Close` | Method | `backend/internal/database/database.go` | 49 |
| `RunMigrations` | Method | `backend/internal/database/migrations.go` | 15 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `Main → Config` | intra_community | 3 |
| `Main → GetEnv` | intra_community | 3 |
| `Main → DB` | intra_community | 3 |
| `CreateHandler → ApiResponse` | intra_community | 3 |
| `CreateHandler → ApiError` | intra_community | 3 |
| `ListHandler → ApiResponse` | intra_community | 3 |
| `ListHandler → ApiError` | intra_community | 3 |
| `GetHandler → ApiResponse` | intra_community | 3 |
| `GetHandler → ApiError` | intra_community | 3 |
| `GetStatsHandler → ApiResponse` | intra_community | 3 |

## Connected Areas

| Area | Connections |
|------|-------------|
| Auth | 16 calls |
| Inventory | 7 calls |
| Items | 6 calls |
| Statclient | 1 calls |

## How to Explore

1. `context({name: "UsernameFromContext"})` — see callers and callees
2. `query({search_query: "character"})` — find related execution flows
3. Read key files listed above for implementation details
4. `explain({target: "<file or symbol>"})` — persisted taint findings (source→sink data flows), when indexed with `--pdg`
