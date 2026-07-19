# zzrpg - High-Performance MMORPG Backend Monolith

**zzrpg** is a data-driven, high-performance MMORPG backend architecture consisting of a Go monolith backend with an embedded Rust `zzstat` core engine for in-process calculations, utilizing PostgreSQL for persistent storage, Redis for caching, and gorilla/websocket for real-time game loops.

---

## Technical Stack
- **Go Backend**: Core monolith implementing authentication, database management, quests, inventory, loot tables, and the WebSocket gateway.
- **Rust zzstat**: High-performance core engine embedded in Go via FFI bindings, resolving derived-stat formulas (base stats, equipment, skills, and buffs) through a priority-ordered modifier pipeline.
- **PostgreSQL**: Relational database with dynamic constraints and JSONB fields for data-driven designs.
- **Redis**: Fast caching store for active character sessions.
- **Gorilla WebSocket**: WebSockets handler with thread-safe write loops, connection overrides, and live event hubs.

---

## Component Layout
- `backend/`: Go monolith repository.
  - `backend/cmd/server/`: HTTP server router, WS mount points, service injectors.
  - `backend/internal/auth/`: JWT authentication, login/register handlers.
  - `backend/internal/character/`: Base attributes, stat provisions, leveling transactions.
  - `backend/internal/combat/`: Real-time combat loop executor, session registry bindings.
  - `backend/internal/inventory/`: Swapping, inventory bag constraints, items providers.
  - `backend/internal/loot/`: Probability drops, drop configuration managers.
  - `backend/internal/quests/`: Quest log registers, killing and npc progress hooks.
  - `backend/internal/socket/`: WebSockets connection hubs, read/write loops, auth validations.
  - `backend/internal/statclient/`: Go client that dynamically loads the Rust shared library (`libzzstat_ffi.so`) via FFI bindings and runs stat resolution in-process.
  - `backend/tests/`: End-to-end integration test suites.
- `docs/`: Design documentation (architecture, combat, database, API — EN/TR).
- `scripts/`: Infrastructure orchestration utilities (`start-infra.sh`).
- `docker-compose.yml`: PostgreSQL and Redis service definitions.

> The Rust `zzstat` core lives in a sibling repository (`github.com/singoesdeep/zzstat`) and is consumed through its Go bindings (`bindings/go`). See the `replace` directive in `backend/go.mod`.

---

## Feature Catalog
1. **JWT Security**: JWT authentication protecting API endpoints and WebSockets upgrade handshakes.
2. **Data-Driven Items**: Items defined dynamically in the database via JSONB modifier fields (`item_definitions`).
3. **Grid Inventory Slots**: Custom constraints for bag storage (`0..99`) and active equipment (`1000..1005`).
4. **Quest Engine**: Dynamic progression triggers (`KILL_MOB`, `TALK_NPC`) with gold/exp rewards and Level-up triggers (+2 base stats).
5. **Embedded Stat Core**: Derived stats (HP, MP, ATTACK, DEFENSE, CRIT_RATE) are resolved in-process by the Rust `zzstat` engine via FFI; combat rolls — accuracy (DEX vs Dodge), critical strikes, and damage variance (±10%) — are computed in Go on top of those stats.
6. **Real-time WebSockets**: In-memory health session registries (`SessionRegistry`), global chats, and combat damage broadcasts.
7. **Loot Table Rollers**: Probability-weighted loot awarded from JSONB drop tables when mobs/dummies are killed.
8. **Idle Progression**: STR/INT scaled offline gold/exp accumulation and offline loot rolling.

---

## API Catalog (REST Endpoints)

### Authentication (Public)
- `POST /api/v1/auth/register` - Registers a new user.
- `POST /api/v1/auth/login` - Logins a user and returns a JWT token.

### Characters (Protected)
- `POST /api/v1/characters` - Creates a new character.
- `GET /api/v1/characters` - Lists characters belonging to the user.
- `GET /api/v1/characters/{id}` - Retrieves character details.
- `GET /api/v1/characters/{id}/stats` - Retrieves character base & derived stats.

### Inventory (Protected)
- `GET /api/v1/characters/{id}/inventory` - Lists items in character inventory and equipment.
- `POST /api/v1/inventory/move` - Moves or swaps items between slots (equips/unequips).
- `POST /api/v1/admin/inventory/add` - **[Admin]** Directly appends an item to character inventory.

### Quests (Protected)
- `POST /api/v1/admin/quests` - **[Admin]** Registers a new quest template definition.
- `GET /api/v1/quests` - Lists all quest definitions in the game.
- `POST /api/v1/characters/{id}/quests/accept` - Character accepts a quest.
- `GET /api/v1/characters/{id}/quests` - Retrieves active character quest logs.
- `POST /api/v1/admin/quests/progress` - **[Admin/Dev]** Manually increments quest progress.

### Loot (Protected)
- `POST /api/v1/admin/loot` - **[Admin]** Creates a loot drop table.
- `GET /api/v1/admin/loot` - Lists all loot tables.

### Item Definitions (Protected)
- `POST /api/v1/admin/items` - **[Admin]** Registers a new item model.
- `PUT /api/v1/admin/items/{id}` - **[Admin]** Updates item definition.
- `GET /api/v1/admin/items` - Lists all item definitions.
- `GET /api/v1/admin/items/{id}` - Retrieves item details.
- `DELETE /api/v1/admin/items/{id}` - **[Admin]** Deletes item definition.

---

## WebSocket Protocol (`ws://localhost:8080/ws?token=<JWT>`)

### Client Commands (Sent to Server)
1. **Character Selection**:
   ```json
   { "type": "SELECT_CHARACTER", "payload": { "character_id": 1 } }
   ```
2. **Combat Attack**:
   ```json
   { "type": "COMBAT_ATTACK", "payload": { "defender_id": 9999 } }
   ```
3. **Chat Message**:
   ```json
   { "type": "CHAT", "payload": { "message": "Hello World!" } }
   ```

### Server Events (Sent to Client)
1. **Select Character Acknowledgment**:
   ```json
   { "type": "SELECT_CHARACTER_ACK", "payload": { "character_id": 1, "status": "ACTIVE" } }
   ```
2. **Offline Gains Summary**:
   ```json
   {
     "type": "OFFLINE_GAINS",
     "payload": {
       "elapsed_seconds": 3600.0,
       "gained_gold": 1500,
       "gained_exp": 3000,
       "leveled_up": true,
       "new_level": 12,
       "loot": [{"item_definition_id": "dragon_sword_0", "quantity": 1}]
     }
   }
   ```
3. **Combat Damage Event**:
   ```json
   {
     "type": "COMBAT_DAMAGE",
     "payload": {
       "attacker_id": 1,
       "defender_id": 9999,
       "is_hit": true,
       "damage": 80,
       "is_crit": false,
       "defender_hp": 420,
       "defender_max_hp": 500,
       "defender_is_dead": false,
       "loot": null
     }
   }
   ```
4. **Chat Broadcast**:
   ```json
   { "type": "CHAT", "payload": { "username": "singo", "message": "Hello World!" } }
   ```
5. **Combat Error**:
   ```json
   { "type": "COMBAT_ERROR", "payload": { "message": "attacker is dead" } }
   ```

---

## Getting Started

### 1. Launch Infrastructure
Start databases (PostgreSQL & Redis) using Podman containers:
```bash
./scripts/start-infra.sh
```

### 2. Apply Migrations
```bash
migrate -path backend/internal/database/migrations -database "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable" up
```

### 3. Build the Rust `zzstat` Library
The stat core is loaded in-process via FFI, so build the shared library from the sibling `zzstat` repository:
```bash
cd ../zzstat
cargo build --release
# produces target/release/libzzstat_ffi.so
```
The backend searches standard paths for `libzzstat_ffi.so`; override the location with `ZZSTAT_LIB_PATH` if needed.

### 4. Run Go Backend
```bash
cd backend
ZZSTAT_LIB_PATH=../../zzstat/target/release/libzzstat_ffi.so go run cmd/server/main.go
```
The API documentation is served at [http://localhost:8080/docs](http://localhost:8080/docs).

### 5. Running Tests
Run unit and E2E integration tests:
```bash
cd backend
go test -v ./...
```
