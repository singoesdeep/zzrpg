# API Design: zzrpg REST, WebSockets & FFI Interfaces (EN)

This document defines the interface specifications between the Frontend Client, Go Backend, and the embedded Rust `zzstat` core engine.

---

## 1. REST API Endpoints

All REST API requests return JSON. Standard formats follow:
- Success: `{ "success": true, "data": { ... } }`
- Error: `{ "success": false, "error": { "code": "ERROR_CODE", "message": "Human readable message" } }`

### 1.1 Authentication & Registration
- **POST `/api/v1/auth/register`**
  - Request: `{"username": "player1", "email": "player1@rpg.com", "password": "secure_password"}`
  - Response: `{"success": true, "data": {"user_id": 101}}`
- **POST `/api/v1/auth/login`**
  - Request: `{"username": "player1", "password": "secure_password"}`
  - Response: `{"success": true, "data": {"token": "jwt_token_here", "expires_in": 3600}}`

### 1.2 Character Management
- **GET `/api/v1/characters`**
  - Header: `Authorization: Bearer <token>`
  - Response: `{"success": true, "data": [{"id": 1, "name": "SuraKing", "class_name": "SURA", "level": 15, "gold": 12000}]}`
- **POST `/api/v1/characters`**
  - Request: `{"name": "SuraKing", "class_name": "SURA"}`
  - Response: `{"success": true, "data": {"id": 1, "name": "SuraKing", "level": 1, "class_name": "SURA"}}`
- **GET `/api/v1/characters/:id/stats`**
  - Response:
    ```json
    {
      "success": true,
      "data": {
        "character_id": 1,
        "base_stats": {"STR": 15, "INT": 22, "DEX": 10, "CON": 12},
        "final_stats": {"HP": 1250, "MP": 820, "ATTACK": 145, "DEFENSE": 68, "CRIT_RATE": 12}
      }
    }
    ```

### 1.3 Inventory & Equipment
- **GET `/api/v1/characters/:id/inventory`**
  - Response:
    ```json
    {
      "success": true,
      "data": {
        "items": [
          {
            "id": 120402,
            "slot_index": 12,
            "item_definition_id": "sword_01",
            "quantity": 1,
            "durability": 95,
            "custom_modifiers": [{"stat": "ATTACK", "operation": "ADD", "value": 5}]
          }
        ]
      }
    }
    ```
- **POST `/api/v1/inventory/move`**
  - Request: `{"character_id": 1, "from_slot": 12, "to_slot": 1000}` (e.g. equipping to weapon slot)
  - Response: `{"success": true, "data": {"refresh_stats": true}}`

### 1.4 Economy & Trade
- **POST `/api/v1/economy/npc/buy`**
  - Request: `{"character_id": 1, "npc_id": "merchant_1", "item_definition_id": "red_potion_1", "quantity": 10}`
- **POST `/api/v1/economy/npc/sell`**
  - Request: `{"character_id": 1, "npc_id": "merchant_1", "inventory_slot": 4}`

---

## 2. WebSocket Protocol (Real-time updates)

Websocket connections are established at `wss://<host>/ws?token=<jwt_token>&character_id=<character_id>`. All payloads are sent as JSON frame messages.

### Message Envelopes
```json
{
  "type": "MESSAGE_TYPE",
  "payload": { ... }
}
```

### 2.1 Incoming Events (Client -> Server)
- **ATTACK**: Initiate base attack on target.
  `{"type": "ATTACK", "payload": {"target_id": 4022, "target_type": "MOB"}}`
- **CAST_SKILL**: Trigger skill cast.
  `{"type": "CAST_SKILL", "payload": {"skill_id": "aura_of_sword", "target_id": 4022, "target_type": "MOB"}}`
- **CHAT**: Global, guild, or whisper.
  `{"type": "CHAT", "payload": {"channel": "GUILD", "message": "Gather for Metin stone!"}}`

### 2.2 Outgoing Events (Server -> Client)
- **COMBAT_DAMAGE**: Emits damage numbers, crits, dodges, and status effects.
  ```json
  {
    "type": "COMBAT_DAMAGE",
    "payload": {
      "attacker_id": 1,
      "defender_id": 4022,
      "damage": 342,
      "is_critical": true,
      "is_miss": false,
      "added_effects": ["burn"]
    }
  }
  ```
- **GOLD_UPDATE** / **EXP_UPDATE**: Idle progress ticks or loot notification.
- **STAT_UPDATE**: Pushed when equipment changes or buffs wear off.

---

## 3. Admin / Game Designer API

Protected by `Role: ADMIN` JWT verification. This is used by the designer console to update game parameters in real-time.

- **POST `/api/v1/admin/items`**: Insert or update item definitions.
  ```json
  {
    "id": "heaven_tear_shield_0",
    "name": "Heaven Tear Shield",
    "slot_type": "SHIELD",
    "min_level": 60,
    "stats_modifiers": [
      {"stat": "DEFENSE", "operation": "ADD", "value": 120},
      {"stat": "RESIST_MAGIC", "operation": "ADD", "value": 10}
    ]
  }
  ```
- **POST `/api/v1/admin/skills`**: Define or adjust skill parameters.
- **POST `/api/v1/admin/loot`**: Configure loot probabilities.

---

## 4. Go Backend -> Rust zzstat FFI Binding Interface

Instead of a network-based gRPC protocol, stats calculations are done in-process. The Go monolith loads the Rust core shared library (`libzzstat_ffi.so`) at startup using `purego` and communicates directly via memory-safe C FFI bindings.

### Core FFI Functions Exposed by Rust Core
The Go binding loads and exposes the following symbols from the Rust FFI library:

```go
// Resolver creation & deletion
zzstat_resolver_create() uintptr
zzstat_resolver_free(resolver uintptr)

// Context creation & deletion
zzstat_context_create() uintptr
zzstat_context_free(ctx uintptr)

// Registering base values and modifiers
zzstat_resolver_register_constant_source(resolver uintptr, statID *byte, value float64) int32
zzstat_resolver_register_scaling_transform(resolver uintptr, statID *byte, phase byte, rule byte, dependency *byte, scaleFactor float64) int32
zzstat_resolver_register_multiplicative_transform(resolver uintptr, statID *byte, phase byte, rule byte, value float64) int32

// Resolving calculations
zzstat_resolver_resolve(resolver uintptr, statID *byte, ctx uintptr, outValue *float64) int32
```

Using in-process FFI calls completely eliminates network latency and serialization/deserialization overhead (gRPC/JSON), making battle-stat recalculations and damage computations execution-speed bound.
