---
name: pb
description: "Skill for the Pb area of zzrpg. 6 symbols across 2 files."
---

# Pb

6 symbols | 2 files | Cohesion: 100%

## When to Use

- Working with code in `backend/`
- Understanding how Descriptor, CalculateStats work
- Modifying pb-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `backend/internal/statclient/pb/zzstat.pb.go` | Descriptor, file_zzstat_proto_rawDescGZIP, init, file_zzstat_proto_init |
| `backend/internal/statclient/pb/zzstat_grpc.pb.go` | CalculateStats, _StatService_CalculateStats_Handler |

## Entry Points

Start here when exploring this area:

- **`Descriptor`** (Method) — `backend/internal/statclient/pb/zzstat.pb.go:61`
- **`CalculateStats`** (Method) — `backend/internal/statclient/pb/zzstat_grpc.pb.go:39`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `Descriptor` | Method | `backend/internal/statclient/pb/zzstat.pb.go` | 61 |
| `CalculateStats` | Method | `backend/internal/statclient/pb/zzstat_grpc.pb.go` | 39 |
| `file_zzstat_proto_rawDescGZIP` | Function | `backend/internal/statclient/pb/zzstat.pb.go` | 232 |
| `init` | Function | `backend/internal/statclient/pb/zzstat.pb.go` | 258 |
| `file_zzstat_proto_init` | Function | `backend/internal/statclient/pb/zzstat.pb.go` | 259 |
| `_StatService_CalculateStats_Handler` | Function | `backend/internal/statclient/pb/zzstat_grpc.pb.go` | 88 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `Init → X` | intra_community | 3 |

## How to Explore

1. `context({name: "Descriptor"})` — see callers and callees
2. `query({search_query: "pb"})` — find related execution flows
3. Read key files listed above for implementation details
4. `explain({target: "<file or symbol>"})` — persisted taint findings (source→sink data flows), when indexed with `--pdg`
