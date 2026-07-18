# Roadmap: zzrpg 3-Month Development Plan

This document details the development milestones and roadmap for building the modern, scalable browser-based RPG backend.

---

## 1. Month 1: Core Foundation, DB and zzstat Integration

### Goal: Establish the module boundaries, database migrations, authentication, character CRUD, and the Rust stat-engine client.

#### Week 1: Setup & Monolith Structure
- Initialize Go modular structure (`cmd/server/main.go`, directory layout).
- Integrate configuration loader (env variables) and structured logger.
- Configure PostgreSQL connection pool via `pgx/v5` and set up the schema migrations tool.
- Setup basic health check endpoints.

#### Week 2: User Accounts & Authentication
- Write `auth` module (registration, login, bcrypt password hashing, JWT signing).
- Implement router middleware for JWT token verification.
- Write unit tests for user creation and authentication handlers.

#### Week 3: Characters & Database Stats
- Implement `character` module: create character, select character, list characters.
- Add initial `character_stats` base values setup during character creation.
- Add repository tests for database state persistence.

#### Week 4: Rust zzstat Core FFI Integration
- Expose Rust core shared library exports (`libzzstat_ffi.so`).
- Create Go FFI bindings client (`statclient`) loading the shared library via `purego`.
- Configure stat client with formula registration DSL mappings.
- Implement in-process derived stats and damage calculation functions.

---

## 2. Month 2: Data-Driven Mechanics (Items, Inventory & Quests)

### Goal: Build the admin content engine, inventory system, and link equipment/buff changes directly to zzstat recalculation.

#### Week 5: Item Definitions & Admin Console
- Implement `items` module supporting dynamic item definition schemas.
- Implement the REST Admin API: `/api/v1/admin/items` to dynamically create items and define modifiers.
- Add validation logic to ensure correct formatting of modifiers JSONB.

#### Week 6: Inventory & Equipment Slots
- Implement `inventory` and `equipment` modules.
- Design database triggers or services to restrict equipment slot assignments based on class rules and minimum level.
- Link inventory adjustments (move, equip, discard) to dispatch event messages.

#### Week 7: Dynamic Stat Computations
- Build event subscriber that listens to `inventory.ItemEquipped` or `inventory.ItemUnequipped` events.
- On event: gather character base stats, all active equipment modifiers, compute final stats in-process via the embedded Rust `zzstat` engine, and update `character_stats.derived_stats` cache.
- Expose `/api/v1/characters/:id/stats` to query current final stats.

#### Week 8: Quest Engine & Progression
- Design quest step models (`KILL_MOB`, `TALK_NPC`).
- Implement `quests` module for tracking progress.
- Implement reward dispatching (exp, gold, items) upon quest completion.

---

## 3. Month 3: Combat Loop, WebSockets & Idle RPG Mechanics

### Goal: Put everything together with real-time sockets, dynamic combat mechanics, loot drops, and offline progressions.

#### Week 9: WebSocket Server Architecture
- Create standard Go HTTP websocket upgrader.
- Implement client connection Hub, managing active sockets, thread-safe write pumps, and mapping users to character sessions.
- Support simple channels (global chat, combat logs).

#### Week 10: Combat Loop & zzstat Integration
- Implement `combat` module.
- Fetch attacker's cached final stats and defender's cached final stats.
- Roll hit/miss, apply damage mitigation formulas, roll critical hit checks, and apply damage.
- Emit real-time `COMBAT_DAMAGE` events via WebSocket.

#### Week 11: Loot Tables & Death Event Dispatching
- Implement dynamic `loot` calculations using rolling percentage lists.
- Listen to character/mob death events: generate gold/items, append to player inventory, and notify player client.
- Test combat loops with mock mobs.

#### Week 12: Idle Progression (Offline Gains)
- Save `last_active_at` on websocket disconnect or user logout.
- Upon login, compare timestamps, calculate elapsed seconds.
- Execute offline simulation calculations:
  - Base production (gold/exp/gather rates per minute) multiplied by character stats.
  - Process items looted during the offline duration.
- Dispatch login summaries describing offline gains.
- Complete system-level integration tests.
