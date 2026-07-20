---
name: items
description: "Skill for the Items area of zzrpg. 22 symbols across 3 files."
---

# Items

22 symbols | 3 files | Cohesion: 77%

## When to Use

- Working with code in `backend/`
- Understanding how CreateHandler, UpdateHandler, GetHandler work
- Modifying items-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `backend/internal/items/service.go` | Create, Update, GetByID, List, Delete (+4) |
| `backend/internal/items/items_test.go` | newMockItemRepository, Create, TestCreateItem, Update, GetByID (+2) |
| `backend/internal/items/handler.go` | CreateHandler, UpdateHandler, GetHandler, ListHandler, DeleteHandler (+1) |

## Entry Points

Start here when exploring this area:

- **`CreateHandler`** (Function) — `backend/internal/items/handler.go:19`
- **`UpdateHandler`** (Function) — `backend/internal/items/handler.go:55`
- **`GetHandler`** (Function) — `backend/internal/items/handler.go:98`
- **`ListHandler`** (Function) — `backend/internal/items/handler.go:131`
- **`DeleteHandler`** (Function) — `backend/internal/items/handler.go:154`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `CreateHandler` | Function | `backend/internal/items/handler.go` | 19 |
| `UpdateHandler` | Function | `backend/internal/items/handler.go` | 55 |
| `GetHandler` | Function | `backend/internal/items/handler.go` | 98 |
| `ListHandler` | Function | `backend/internal/items/handler.go` | 131 |
| `DeleteHandler` | Function | `backend/internal/items/handler.go` | 154 |
| `TestCreateItem` | Function | `backend/internal/items/items_test.go` | 55 |
| `NewItemService` | Function | `backend/internal/items/service.go` | 19 |
| `TestUpdateAndDelete` | Function | `backend/internal/items/items_test.go` | 123 |
| `Create` | Method | `backend/internal/items/service.go` | 8 |
| `Update` | Method | `backend/internal/items/service.go` | 9 |
| `GetByID` | Method | `backend/internal/items/service.go` | 10 |
| `List` | Method | `backend/internal/items/service.go` | 11 |
| `Delete` | Method | `backend/internal/items/service.go` | 12 |
| `Create` | Method | `backend/internal/items/items_test.go` | 15 |
| `Update` | Method | `backend/internal/items/items_test.go` | 23 |
| `GetByID` | Method | `backend/internal/items/items_test.go` | 31 |
| `Delete` | Method | `backend/internal/items/items_test.go` | 47 |
| `Create` | Method | `backend/internal/items/service.go` | 23 |
| `Update` | Method | `backend/internal/items/service.go` | 30 |
| `writeError` | Function | `backend/internal/items/handler.go` | 185 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `CreateHandler → ApiResponse` | intra_community | 3 |
| `CreateHandler → ApiError` | intra_community | 3 |
| `UpdateHandler → ApiResponse` | intra_community | 3 |
| `UpdateHandler → ApiError` | intra_community | 3 |
| `GetHandler → ApiResponse` | intra_community | 3 |
| `GetHandler → ApiError` | intra_community | 3 |
| `ListHandler → ApiResponse` | intra_community | 3 |
| `ListHandler → ApiError` | intra_community | 3 |
| `DeleteHandler → ApiResponse` | intra_community | 3 |
| `DeleteHandler → ApiError` | intra_community | 3 |

## How to Explore

1. `context({name: "CreateHandler"})` — see callers and callees
2. `query({search_query: "items"})` — find related execution flows
3. Read key files listed above for implementation details
4. `explain({target: "<file or symbol>"})` — persisted taint findings (source→sink data flows), when indexed with `--pdg`
