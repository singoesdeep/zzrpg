# Testing Guide: zzrpg Month 1 Setup (EN)

Follow these steps to run the infrastructure, start the services, and test the APIs using `curl`.

---

## Step 1: Start the Podman Infrastructure

Run the helper script from the project root to start PostgreSQL and Redis:
```bash
./scripts/start-infra.sh
```

---

## Step 2: Build the Rust zzstat shared library

The Rust `zzstat` core lives in a sibling repository (`github.com/singoesdeep/zzstat`). Open a terminal, switch to it, and compile the FFI shared library:
```bash
cd ../zzstat
cargo build --release
```
This produces `../zzstat/target/release/libzzstat_ffi.so`. The Go backend client loads it dynamically at runtime; set `ZZSTAT_LIB_PATH` to point at it if it is not on a standard search path.

---

## Step 3: Start the Go Backend Server

Open a second terminal, navigate to the `backend/` directory, and run:
```bash
cd backend
ZZSTAT_LIB_PATH=../../zzstat/target/release/libzzstat_ffi.so go run ./cmd/server
```
You should see:
```
Starting zzrpg backend...
Connecting to PostgreSQL...
Successfully connected to PostgreSQL
Running database migrations...
All database migrations completed successfully
HTTP server listening on :8080
```
Keep this terminal open.

---

## Step 4: Run test requests via curl

Open a third terminal to run test commands:

### 1. Check API & DB Health
```bash
curl -i http://localhost:8080/health
```
**Expected Response (HTTP 200):**
```json
{"status":"UP", "database":"OK"}
```

### 2. Register a New User
```bash
curl -i -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"singo","email":"singo@test.com","password":"password123"}' \
  http://localhost:8080/api/v1/auth/register
```
**Expected Response (HTTP 201):**
```json
{"success":true,"data":{"user_id":1,"username":"singo","email":"singo@test.com"}}
```

### 3. Log In & Retrieve JWT Token
```bash
curl -i -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"singo","password":"password123"}' \
  http://localhost:8080/api/v1/auth/login
```
**Expected Response (HTTP 200):**
```json
{
  "success": true,
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "expires_in": 86400
  }
}
```
*Copy the `token` value from the response to use in the next steps.*

---

## Step 5: Test Authenticated Endpoints

Set a temporary environment variable in your terminal for the JWT token:
```bash
export TOKEN="your_jwt_token_here"
```

### 1. Verify User Profile Info (Me endpoint)
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/auth/me
```
**Expected Response (HTTP 200):**
```json
{"success":true,"data":{"user_id":1,"username":"singo"}}
```

### 2. Create a Character (WARRIOR class)
```bash
curl -i -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"WarriorGod","class_name":"WARRIOR"}' \
  http://localhost:8080/api/v1/characters
```
**Expected Response (HTTP 201):**
```json
{
  "success": true,
  "data": {
    "id": 1,
    "user_id": 1,
    "name": "WarriorGod",
    "class_name": "WARRIOR",
    "level": 1,
    "experience": 0,
    "gold": 0,
    "last_active_at": "2026-07-18T...",
    "created_at": "2026-07-18T...",
    "updated_at": "2026-07-18T...",
    "stats": {
      "character_id": 1,
      "base_stats": {"CON": 15, "DEX": 10, "INT": 5, "STR": 15},
      "derived_stats": {"HP": 225, "MP": 50, "ATTACK": 30, "DEFENSE": 15, "CRIT_RATE": 5},
      "updated_at": "2026-07-18T..."
    }
  }
}
```

### 3. List All Your Characters
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/characters
```
**Expected Response (HTTP 200):**
```json
{"success":true,"data":[{"id":1,"user_id":1,"name":"WarriorGod","class_name":"WARRIOR","level":1,"experience":0,"gold":0,"last_active_at":"...","created_at":"...","updated_at":"..."}]}
```

### 4. Fetch Details & Stats of Character #1
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/characters/1
```
**Expected Response (HTTP 200):**
Same detailed JSON response showing the character attributes and computed initial stats cache.
