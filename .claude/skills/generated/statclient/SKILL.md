---
name: statclient
description: "Skill for the Statclient area of zzrpg. 4 symbols across 3 files."
---

# Statclient

4 symbols | 3 files | Cohesion: 86%

## When to Use

- Working with code in `backend/`
- Understanding how NewClient, TestStatClient, NewStatServiceClient work
- Modifying statclient-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `backend/internal/statclient/pb/zzstat_grpc.pb.go` | NewStatServiceClient, RegisterStatServiceServer |
| `backend/internal/statclient/client.go` | NewClient |
| `backend/internal/statclient/client_test.go` | TestStatClient |

## Entry Points

Start here when exploring this area:

- **`NewClient`** (Function) — `backend/internal/statclient/client.go:37`
- **`TestStatClient`** (Function) — `backend/internal/statclient/client_test.go:26`
- **`NewStatServiceClient`** (Function) — `backend/internal/statclient/pb/zzstat_grpc.pb.go:35`
- **`RegisterStatServiceServer`** (Function) — `backend/internal/statclient/pb/zzstat_grpc.pb.go:77`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `NewClient` | Function | `backend/internal/statclient/client.go` | 37 |
| `TestStatClient` | Function | `backend/internal/statclient/client_test.go` | 26 |
| `NewStatServiceClient` | Function | `backend/internal/statclient/pb/zzstat_grpc.pb.go` | 35 |
| `RegisterStatServiceServer` | Function | `backend/internal/statclient/pb/zzstat_grpc.pb.go` | 77 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `NewClient → StatServiceClient` | intra_community | 3 |

## How to Explore

1. `context({name: "NewClient"})` — see callers and callees
2. `query({search_query: "statclient"})` — find related execution flows
3. Read key files listed above for implementation details
4. `explain({target: "<file or symbol>"})` — persisted taint findings (source→sink data flows), when indexed with `--pdg`
