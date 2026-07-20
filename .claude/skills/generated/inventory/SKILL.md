---
name: inventory
description: "Skill for the Inventory area of zzrpg. 23 symbols across 4 files."
---

# Inventory

23 symbols | 4 files | Cohesion: 91%

## When to Use

- Working with code in `backend/`
- Understanding how Global, TestMoveAndEquipItem, NewInventoryService work
- Modifying inventory-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `backend/internal/inventory/service.go` | NewInventoryService, MoveItem, GetInventory, AddItem, MoveItem (+4) |
| `backend/internal/inventory/inventory_test.go` | newMockInventoryRepository, GetBySlot, Move, Swap, AddItem (+2) |
| `backend/internal/inventory/handler.go` | GetInventoryHandler, MoveItemHandler, AddAdminItemHandler, ctx, writeError |
| `backend/internal/events/events.go` | Global, Subscribe |

## Entry Points

Start here when exploring this area:

- **`Global`** (Function) — `backend/internal/events/events.go:30`
- **`TestMoveAndEquipItem`** (Function) — `backend/internal/inventory/inventory_test.go:98`
- **`NewInventoryService`** (Function) — `backend/internal/inventory/service.go:23`
- **`GetInventoryHandler`** (Function) — `backend/internal/inventory/handler.go:29`
- **`MoveItemHandler`** (Function) — `backend/internal/inventory/handler.go:63`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `Global` | Function | `backend/internal/events/events.go` | 30 |
| `TestMoveAndEquipItem` | Function | `backend/internal/inventory/inventory_test.go` | 98 |
| `NewInventoryService` | Function | `backend/internal/inventory/service.go` | 23 |
| `GetInventoryHandler` | Function | `backend/internal/inventory/handler.go` | 29 |
| `MoveItemHandler` | Function | `backend/internal/inventory/handler.go` | 63 |
| `AddAdminItemHandler` | Function | `backend/internal/inventory/handler.go` | 104 |
| `Subscribe` | Method | `backend/internal/events/events.go` | 34 |
| `GetBySlot` | Method | `backend/internal/inventory/inventory_test.go` | 21 |
| `Move` | Method | `backend/internal/inventory/inventory_test.go` | 40 |
| `Swap` | Method | `backend/internal/inventory/inventory_test.go` | 49 |
| `AddItem` | Method | `backend/internal/inventory/inventory_test.go` | 63 |
| `RemoveItem` | Method | `backend/internal/inventory/inventory_test.go` | 69 |
| `MoveItem` | Method | `backend/internal/inventory/service.go` | 11 |
| `GetInventory` | Method | `backend/internal/inventory/service.go` | 12 |
| `AddItem` | Method | `backend/internal/inventory/service.go` | 13 |
| `MoveItem` | Method | `backend/internal/inventory/service.go` | 63 |
| `GetEquippedModifiers` | Method | `backend/internal/inventory/service.go` | 219 |
| `newMockInventoryRepository` | Function | `backend/internal/inventory/inventory_test.go` | 17 |
| `ctx` | Function | `backend/internal/inventory/handler.go` | 144 |
| `writeError` | Function | `backend/internal/inventory/handler.go` | 150 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `AddAdminItemHandler → ApiResponse` | intra_community | 3 |
| `AddAdminItemHandler → ApiError` | intra_community | 3 |
| `GetInventoryHandler → ApiResponse` | intra_community | 3 |
| `GetInventoryHandler → ApiError` | intra_community | 3 |
| `MoveItemHandler → ApiResponse` | intra_community | 3 |
| `MoveItemHandler → ApiError` | intra_community | 3 |
| `MoveItem → IsEquipmentSlot` | intra_community | 3 |

## How to Explore

1. `context({name: "Global"})` — see callers and callees
2. `query({search_query: "inventory"})` — find related execution flows
3. Read key files listed above for implementation details
4. `explain({target: "<file or symbol>"})` — persisted taint findings (source→sink data flows), when indexed with `--pdg`
