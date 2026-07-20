---
name: auth
description: "Skill for the Auth area of zzrpg. 10 symbols across 4 files."
---

# Auth

10 symbols | 4 files | Cohesion: 82%

## When to Use

- Working with code in `backend/`
- Understanding how RegisterHandler, LoginHandler, AuthMiddleware work
- Modifying auth-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `backend/internal/auth/handler.go` | RegisterHandler, LoginHandler, writeError |
| `backend/internal/auth/service.go` | Register, Login, NewAuthService |
| `backend/internal/auth/auth_test.go` | newMockUserRepository, TestRegister, TestLogin |
| `backend/internal/auth/middleware.go` | AuthMiddleware |

## Entry Points

Start here when exploring this area:

- **`RegisterHandler`** (Function) — `backend/internal/auth/handler.go:30`
- **`LoginHandler`** (Function) — `backend/internal/auth/handler.go:72`
- **`AuthMiddleware`** (Function) — `backend/internal/auth/middleware.go:17`
- **`TestRegister`** (Function) — `backend/internal/auth/auth_test.go:50`
- **`TestLogin`** (Function) — `backend/internal/auth/auth_test.go:75`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `RegisterHandler` | Function | `backend/internal/auth/handler.go` | 30 |
| `LoginHandler` | Function | `backend/internal/auth/handler.go` | 72 |
| `AuthMiddleware` | Function | `backend/internal/auth/middleware.go` | 17 |
| `TestRegister` | Function | `backend/internal/auth/auth_test.go` | 50 |
| `TestLogin` | Function | `backend/internal/auth/auth_test.go` | 75 |
| `NewAuthService` | Function | `backend/internal/auth/service.go` | 26 |
| `Register` | Method | `backend/internal/auth/service.go` | 11 |
| `Login` | Method | `backend/internal/auth/service.go` | 12 |
| `writeError` | Function | `backend/internal/auth/handler.go` | 113 |
| `newMockUserRepository` | Function | `backend/internal/auth/auth_test.go` | 14 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `RegisterHandler → ApiResponse` | intra_community | 3 |
| `RegisterHandler → ApiError` | intra_community | 3 |
| `LoginHandler → ApiResponse` | intra_community | 3 |
| `LoginHandler → ApiError` | intra_community | 3 |
| `AuthMiddleware → ApiResponse` | intra_community | 3 |
| `AuthMiddleware → ApiError` | intra_community | 3 |

## How to Explore

1. `context({name: "RegisterHandler"})` — see callers and callees
2. `query({search_query: "auth"})` — find related execution flows
3. Read key files listed above for implementation details
4. `explain({target: "<file or symbol>"})` — persisted taint findings (source→sink data flows), when indexed with `--pdg`
