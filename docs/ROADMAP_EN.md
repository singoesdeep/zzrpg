# Development Roadmap & Milestones (EN)

This document details the development milestones and roadmap for building the modular monolith backend of **zzrpg**.

---

## 1. Month 1: Foundation (Auth, Database & Basic Stats)

### Goal: Establish base architecture, database connection pool, authentication REST endpoints, basic character classes, and FFI integration client skeleton.

#### Week 1: Environment & Auth Base
- Setup the Go workspace and standard directory layout (`cmd/`, `internal/`, `pkg/`).
- Setup dynamic configuration parameters using environment variables.
- Setup PostgreSQL connection pool utilizing `pgx/v5`.
- Implement SQL database migrations structure.

#### Week 2: User Authentication API
- Design `users` schema.
- Implement REST API endpoints for user Registration and Login.
- Generate and validate JWT authorization headers.
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

## 3. Month 3: Real-Time combat, WebSockets & Launch

### Goal: Move calculation loops to real-time WebSockets, implement sessions registry, loot rolling, and launch.

#### Week 9: WebSockets Gateway
- Establish WS server upgrade connection handler.
- Validate JWT authentication tokens inside WebSocket query strings.
- Construct the `Hub` connection manager to broadcast events and manage player session states.
- Resolve duplicate logins by dropping older sessions.

#### Week 10: Sockets-driven Combat Loop
- Parse incoming client messages (`COMBAT_ATTACK`).
- Build in-memory thread-safe `SessionRegistry` to manage active HP and combat status of players/mobs.
- Implement target lookup (PvP target or dummy target).
- Expose WebSocket damage event notifications (`COMBAT_DAMAGE`).

#### Week 11: Loot Tables Engine
- Design `loot_tables` schema supporting JSONB drops rates.
- Link mob death event triggers to loot table generation.
- Dispatch items and gold rewards directly to the player's inventory, with fallback to ground drops.

#### Week 12: Offline Progression & Polish
- Track `last_active_at` timestamp.
- Compare offline duration upon login and award STR/INT scaled passive experience and gold gains.
- Complete system audit, write documentation, clean code formatting, and deploy!
