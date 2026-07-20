package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"errors"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/eventlog"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/internal/creature"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
	"github.com/singoesdeep/zzrpg/backend/internal/killreward"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
	"github.com/singoesdeep/zzrpg/backend/internal/session"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

// mockStatClient is a stub for the Rust gRPC server during integration test
type mockStatClient struct{}

func (m *mockStatClient) Calculate(ctx context.Context, state statclient.CharacterState) (map[string]float64, error) {
	return map[string]float64{
		"HP":        500.0,
		"MP":        100.0,
		"ATTACK":    120.0,
		"DEFENSE":   30.0,
		"CRIT_RATE": 5.0,
	}, nil
}

func (m *mockStatClient) CalculateDamage(ctx context.Context, req statclient.CalculateDamageReq) (statclient.DamageResult, error) {
	return statclient.DamageResult{
		IsHit:  true,
		Damage: 80,
		IsCrit: false,
	}, nil
}

func (m *mockStatClient) Close() error {
	return nil
}

func TestEndToEndGameLoop(t *testing.T) {
	// 1. Establish database connection or skip
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible on localhost:5432, skipping integration test. Start infra via scripts/start-infra.sh to run.")
	}
	defer pool.Close()

	// Ping database to make sure it is actually responding
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping integration test.")
	}
	migrateTestDB(t, pool)

	// 2. Initialize all modules in-memory
	db := &database.DB{Pool: pool, Store: store.New(pool)}
	jwtSecret := "integration-test-secret-321-secure"

	authRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}

	charRepo := character.NewCharacterRepository(db.Store)
	charService := character.NewCharacterService(charRepo, statClient, nil, nil, nil)

	itemRepo := items.NewItemRepository(db.Store)
	itemService := items.NewItemService(itemRepo)
	_ = itemService

	invRepo := inventory.NewInventoryRepository(db.Store)
	invService := inventory.NewInventoryService(invRepo, charService, bus.NewInProc(nil))

	charService.SetEquipmentProvider(invService)

	questRepo := quests.NewQuestRepository(db.Store)
	questService := quests.NewQuestService(questRepo, charService, invService, nil, nil)

	lootRepo := loot.NewLootRepository(db.Store)
	lootService := loot.NewLootService(lootRepo)

	hub := socket.NewHub()
	go hub.Run()

	sessionReg := session.NewRegistry()
	combatService := combat.NewCombatService(makeCreatures(charService), statClient, sessionReg, killreward.New(charService, questService, lootService, invService, nil), nil, nil, nil)

	// WebSocket handler routing callback
	wsMsgHandler := func(client *socket.Client, msg socket.WSMessage) {
		switch msg.Type {
		case "CHAT":
			var payload socket.ChatPayload
			_ = json.Unmarshal(msg.Payload, &payload)
			broadMsg, _ := json.Marshal(map[string]interface{}{
				"type": "CHAT",
				"payload": map[string]interface{}{
					"username": client.Username,
					"message":  payload.Message,
				},
			})
			hub.Broadcast <- broadMsg

		case "SELECT_CHARACTER":
			var payload socket.SelectCharPayload
			_ = json.Unmarshal(msg.Payload, &payload)
			hub.AssociateCharacter(client, payload.CharacterID)

			char, err := charService.GetByID(context.Background(), payload.CharacterID)
			if err == nil {
				sessionReg.StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])
				elapsedSeconds := time.Now().Sub(char.LastActiveAt).Seconds()
				if elapsedSeconds >= 10 {
					gainedGold := int64(100)
					gainedExp := int64(200)
					leveledUp, newLevel, err := charService.AddRewards(context.Background(), payload.CharacterID, gainedGold, gainedExp)
					if err == nil {
						gainsSummary, _ := json.Marshal(map[string]interface{}{
							"type": "OFFLINE_GAINS",
							"payload": map[string]interface{}{
								"elapsed_seconds": elapsedSeconds,
								"gained_gold":     gainedGold,
								"gained_exp":      gainedExp,
								"leveled_up":      leveledUp,
								"new_level":       newLevel,
							},
						})
						client.Send <- gainsSummary
					}
				}
				_ = charService.UpdateLastActive(context.Background(), payload.CharacterID)
			}

			ack, _ := json.Marshal(map[string]interface{}{
				"type": "SELECT_CHARACTER_ACK",
				"payload": map[string]interface{}{
					"character_id": payload.CharacterID,
					"status":       "ACTIVE",
				},
			})
			client.Send <- ack

		case "COMBAT_ATTACK":
			var payload combat.AttackRequest
			_ = json.Unmarshal(msg.Payload, &payload)
			payload.AttackerID = client.CharacterID

			res, err := combatService.ExecuteAttack(context.Background(), payload)
			if err != nil {
				return
			}

			broadMsg, _ := json.Marshal(map[string]interface{}{
				"type":    "COMBAT_DAMAGE",
				"payload": res,
			})
			hub.Broadcast <- broadMsg
		}
	}

	wsDisconnectHandler := func(client *socket.Client) {
		if client.CharacterID > 0 {
			_ = charService.UpdateLastActive(context.Background(), client.CharacterID)
			sessionReg.EndSession(client.CharacterID)
		}
	}

	// 3. Setup router
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", auth.RegisterHandler(authService))
	mux.HandleFunc("POST /api/v1/auth/login", auth.LoginHandler(authService))
	mux.Handle("POST /api/v1/characters", auth.AuthMiddleware(jwtSecret)(character.CreateHandler(charService)))
	mux.HandleFunc("/ws", socket.ServeWS(hub, jwtSecret, wsMsgHandler, wsDisconnectHandler))

	server := httptest.NewServer(mux)
	defer server.Close()

	// 4. Register E2E User
	uniqueUser := "user_" + time.Now().Format("02150405")
	regBody, _ := json.Marshal(map[string]string{
		"username": uniqueUser,
		"email":    uniqueUser + "@test.com",
		"password": "securepassword123",
	})
	resp, err := http.Post(server.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(regBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to register user: %v, status: %d", err, resp.StatusCode)
	}

	// 5. Login User to get JWT
	loginBody, _ := json.Marshal(map[string]string{
		"username": uniqueUser,
		"password": "securepassword123",
	})
	resp, err = http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(loginBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to login: %v", err)
	}
	var loginRes struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&loginRes)
	token := loginRes.Data.Token
	if token == "" {
		t.Fatalf("empty JWT token returned")
	}

	// 6. Create Character
	charBody, _ := json.Marshal(map[string]string{
		"name":       "char_" + uniqueUser[5:],
		"class_name": "WARRIOR",
	})
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/characters", bytes.NewBuffer(charBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err = httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create character: %v, status: %d", err, resp.StatusCode)
	}

	var charRes struct {
		Success bool `json:"success"`
		Data    struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&charRes)
	charID := charRes.Data.ID

	// 7. Dial WebSocket Connection
	wsURL, _ := url.Parse(server.URL)
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws"
	q := wsURL.Query()
	q.Set("token", token)
	wsURL.RawQuery = q.Encode()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	if err != nil {
		t.Fatalf("websocket connection failed: %v", err)
	}
	defer ws.Close()

	// 8. Select Character over WebSocket
	selectPayload := map[string]interface{}{
		"type": "SELECT_CHARACTER",
		"payload": map[string]interface{}{
			"character_id": charID,
		},
	}
	selectMsgBytes, _ := json.Marshal(selectPayload)
	err = ws.WriteMessage(websocket.TextMessage, selectMsgBytes)
	if err != nil {
		t.Fatalf("failed to send SELECT_CHARACTER command: %v", err)
	}

	// Read SELECT_CHARACTER_ACK response
	_, p, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read WS response: %v", err)
	}

	var wsResponse map[string]interface{}
	_ = json.Unmarshal(p, &wsResponse)
	if wsResponse["type"] != "SELECT_CHARACTER_ACK" {
		t.Errorf("expected SELECT_CHARACTER_ACK, got: %+v", wsResponse)
	}

	// 9. Execute Attack against Training Dummy
	attackPayload := map[string]interface{}{
		"type": "COMBAT_ATTACK",
		"payload": map[string]interface{}{
			"defender_id": 9999, // Training Dummy ID
		},
	}
	attackMsgBytes, _ := json.Marshal(attackPayload)
	err = ws.WriteMessage(websocket.TextMessage, attackMsgBytes)
	if err != nil {
		t.Fatalf("failed to send COMBAT_ATTACK command: %v", err)
	}

	// Read COMBAT_DAMAGE event broadcast
	_, p, err = ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read combat broadcast: %v", err)
	}

	var combatResponse map[string]interface{}
	_ = json.Unmarshal(p, &combatResponse)
	if combatResponse["type"] != "COMBAT_DAMAGE" {
		t.Errorf("expected type COMBAT_DAMAGE, got: %+v", combatResponse)
	}

	combatPayload := combatResponse["payload"].(map[string]interface{})
	if combatPayload["damage"].(float64) != 80.0 || combatPayload["is_hit"].(bool) != true {
		t.Errorf("unexpected combat damage details: %+v", combatPayload)
	}

	// 10. Close WebSocket connection cleanly to trigger wsDisconnectHandler
	err = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		fmt.Printf("connection closed message send failed (expected if already closing): %v\n", err)
	}
}

func TestDoubleSessionOverride(t *testing.T) {
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping double session test.")
	}
	defer pool.Close()

	// Ping database to make sure it is actually responding
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping double session test.")
	}
	migrateTestDB(t, pool)

	db := &database.DB{Pool: pool, Store: store.New(pool)}
	jwtSecret := "double-session-secret"

	authRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}
	charRepo := character.NewCharacterRepository(db.Store)
	charService := character.NewCharacterService(charRepo, statClient, nil, nil, nil)

	invRepo := inventory.NewInventoryRepository(db.Store)
	invService := inventory.NewInventoryService(invRepo, charService, bus.NewInProc(nil))
	charService.SetEquipmentProvider(invService)

	hub := socket.NewHub()
	go hub.Run()

	wsMsgHandler := func(client *socket.Client, msg socket.WSMessage) {
		if msg.Type == "SELECT_CHARACTER" {
			var payload socket.SelectCharPayload
			_ = json.Unmarshal(msg.Payload, &payload)
			hub.AssociateCharacter(client, payload.CharacterID)

			ack, _ := json.Marshal(map[string]interface{}{
				"type": "SELECT_CHARACTER_ACK",
				"payload": map[string]interface{}{
					"character_id": payload.CharacterID,
					"status":       "ACTIVE",
				},
			})
			client.Send <- ack
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", auth.RegisterHandler(authService))
	mux.HandleFunc("POST /api/v1/auth/login", auth.LoginHandler(authService))
	mux.Handle("POST /api/v1/characters", auth.AuthMiddleware(jwtSecret)(character.CreateHandler(charService)))
	mux.HandleFunc("/ws", socket.ServeWS(hub, jwtSecret, wsMsgHandler, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Register & Login User
	uniqueUser := "user_ds_" + time.Now().Format("02150405")
	regBody, _ := json.Marshal(map[string]string{
		"username": uniqueUser,
		"email":    uniqueUser + "@test.com",
		"password": "securepassword123",
	})
	_, _ = http.Post(server.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(regBody))

	loginBody, _ := json.Marshal(map[string]string{
		"username": uniqueUser,
		"password": "securepassword123",
	})
	resp, _ := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(loginBody))
	var loginRes struct {
		Data struct{ Token string } `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&loginRes)
	token := loginRes.Data.Token

	// Create Character
	charBody, _ := json.Marshal(map[string]string{
		"name":       "ds_" + uniqueUser[8:],
		"class_name": "WARRIOR",
	})
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/characters", bytes.NewBuffer(charBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{}
	resp, err = httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create character in double session test: status: %d", resp.StatusCode)
	}
	var charRes struct {
		Success bool `json:"success"`
		Data    struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&charRes)
	charID := charRes.Data.ID

	// WS 1 Connect
	wsURL, _ := url.Parse(server.URL)
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws"
	q := wsURL.Query()
	q.Set("token", token)
	wsURL.RawQuery = q.Encode()

	ws1, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	if err != nil {
		t.Fatalf("ws1 failed to connect: %v", err)
	}
	defer ws1.Close()

	// Select character on WS 1
	selectPayload := map[string]interface{}{
		"type": "SELECT_CHARACTER",
		"payload": map[string]interface{}{
			"character_id": charID,
		},
	}
	selectMsgBytes, _ := json.Marshal(selectPayload)
	_ = ws1.WriteMessage(websocket.TextMessage, selectMsgBytes)

	// Read WS 1 response
	_, _, err = ws1.ReadMessage()
	if err != nil {
		t.Fatalf("ws1 failed to read response: %v", err)
	}

	// WS 2 Connect for same user/character
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	if err != nil {
		t.Fatalf("ws2 failed to connect: %v", err)
	}
	defer ws2.Close()

	// Select same character on WS 2
	_ = ws2.WriteMessage(websocket.TextMessage, selectMsgBytes)

	// WS 1 should get disconnected by session override
	time.Sleep(100 * time.Millisecond)
	_, _, err = ws1.ReadMessage()
	if err == nil {
		t.Errorf("expected WS 1 to be disconnected by session override, but read succeeded")
	}
}

func TestDeadAttackerAndDefender(t *testing.T) {
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping dead status test.")
	}
	defer pool.Close()

	// Ping database to make sure it is actually responding
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping dead status test.")
	}
	migrateTestDB(t, pool)

	db := &database.DB{Pool: pool, Store: store.New(pool)}
	jwtSecret := "dead-status-secret"

	authRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}
	charRepo := character.NewCharacterRepository(db.Store)
	charService := character.NewCharacterService(charRepo, statClient, nil, nil, nil)

	invRepo := inventory.NewInventoryRepository(db.Store)
	invService := inventory.NewInventoryService(invRepo, charService, bus.NewInProc(nil))
	charService.SetEquipmentProvider(invService)

	questRepo := quests.NewQuestRepository(db.Store)
	questService := quests.NewQuestService(questRepo, charService, invService, nil, nil)

	lootRepo := loot.NewLootRepository(db.Store)
	lootService := loot.NewLootService(lootRepo)

	hub := socket.NewHub()
	go hub.Run()

	sessionReg := session.NewRegistry()
	combatService := combat.NewCombatService(makeCreatures(charService), statClient, sessionReg, killreward.New(charService, questService, lootService, invService, nil), nil, nil, nil)

	wsMsgHandler := func(client *socket.Client, msg socket.WSMessage) {
		switch msg.Type {
		case "SELECT_CHARACTER":
			var payload socket.SelectCharPayload
			errUn := json.Unmarshal(msg.Payload, &payload)
			fmt.Printf("DEBUG: msg.Payload = %s, payload.CharacterID = %d, err = %v\n", string(msg.Payload), payload.CharacterID, errUn)
			hub.AssociateCharacter(client, payload.CharacterID)

			char, err := charService.GetByID(context.Background(), payload.CharacterID)
			if err == nil {
				sessionReg.StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])
			}

			ack, _ := json.Marshal(map[string]interface{}{
				"type": "SELECT_CHARACTER_ACK",
				"payload": map[string]interface{}{
					"character_id": payload.CharacterID,
					"status":       "ACTIVE",
				},
			})
			client.Send <- ack

		case "COMBAT_ATTACK":
			var payload combat.AttackRequest
			_ = json.Unmarshal(msg.Payload, &payload)
			payload.AttackerID = client.CharacterID

			res, err := combatService.ExecuteAttack(context.Background(), payload)
			if err != nil {
				errAck, _ := json.Marshal(map[string]interface{}{
					"type": "COMBAT_ERROR",
					"payload": map[string]interface{}{
						"message": err.Error(),
					},
				})
				client.Send <- errAck
				return
			}

			broadMsg, _ := json.Marshal(map[string]interface{}{
				"type":    "COMBAT_DAMAGE",
				"payload": res,
			})
			hub.Broadcast <- broadMsg
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", auth.RegisterHandler(authService))
	mux.HandleFunc("POST /api/v1/auth/login", auth.LoginHandler(authService))
	mux.Handle("POST /api/v1/characters", auth.AuthMiddleware(jwtSecret)(character.CreateHandler(charService)))
	mux.HandleFunc("/ws", socket.ServeWS(hub, jwtSecret, wsMsgHandler, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Register & Login User
	uniqueUser := "user_dead_" + time.Now().Format("02150405")
	regBody, _ := json.Marshal(map[string]string{
		"username": uniqueUser,
		"email":    uniqueUser + "@test.com",
		"password": "securepassword123",
	})
	_, _ = http.Post(server.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(regBody))

	loginBody, _ := json.Marshal(map[string]string{
		"username": uniqueUser,
		"password": "securepassword123",
	})
	resp, _ := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(loginBody))
	var loginRes struct {
		Data struct{ Token string } `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&loginRes)
	token := loginRes.Data.Token

	// Create Character 1 (Attacker)
	charBody, _ := json.Marshal(map[string]string{
		"name":       "d1_" + uniqueUser[10:],
		"class_name": "WARRIOR",
	})
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/characters", bytes.NewBuffer(charBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{}
	resp, err = httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create character 1 in dead status test: status: %d", resp.StatusCode)
	}
	var charRes struct {
		Success bool `json:"success"`
		Data    struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&charRes)
	attackerID := charRes.Data.ID

	// Create Character 2 (Defender)
	charBody2, _ := json.Marshal(map[string]string{
		"name":       "d2_" + uniqueUser[10:],
		"class_name": "WARRIOR",
	})
	req2, _ := http.NewRequest("POST", server.URL+"/api/v1/characters", bytes.NewBuffer(charBody2))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := httpClient.Do(req2)
	if err != nil || resp2.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create character 2 in dead status test: status: %d", resp2.StatusCode)
	}
	var charRes2 struct {
		Success bool `json:"success"`
		Data    struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&charRes2)
	defenderID := charRes2.Data.ID

	// WS Connect Attacker
	wsURL, _ := url.Parse(server.URL)
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws"
	q := wsURL.Query()
	q.Set("token", token)
	wsURL.RawQuery = q.Encode()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	if err != nil {
		t.Fatalf("ws connect failed: %v", err)
	}
	defer ws.Close()

	// Select Character Attacker
	selectPayload := map[string]interface{}{
		"type": "SELECT_CHARACTER",
		"payload": map[string]interface{}{
			"character_id": attackerID,
		},
	}
	selectMsgBytes, _ := json.Marshal(selectPayload)
	_ = ws.WriteMessage(websocket.TextMessage, selectMsgBytes)
	_, pAck, errAck := ws.ReadMessage()
	t.Logf("SELECT ACK RESPONSE: %s, err: %v", string(pAck), errAck)

	sess, exists := sessionReg.GetSession(attackerID)
	t.Logf("ATTACKER SESSION BEFORE ATTACK: exists=%v, %+v", exists, sess)

	// Start Defender Session manually so it exists in registry
	_ = sessionReg.StartSession(defenderID, 500.0, 100.0)

	// 1. Attacker is Dead: Deduct all HP from attacker
	_, _ = sessionReg.DeductHP(attackerID, 1000.0) // kills attacker

	// Try attacking defender
	attackPayload := map[string]interface{}{
		"type": "COMBAT_ATTACK",
		"payload": map[string]interface{}{
			"defender_id": defenderID,
		},
	}
	attackMsgBytes, _ := json.Marshal(attackPayload)
	_ = ws.WriteMessage(websocket.TextMessage, attackMsgBytes)

	_, p, _ := ws.ReadMessage()
	var errResponse map[string]interface{}
	_ = json.Unmarshal(p, &errResponse)
	if errResponse["type"] != "COMBAT_ERROR" {
		t.Errorf("expected COMBAT_ERROR, got: %+v", errResponse)
	}
	errMsg := errResponse["payload"].(map[string]interface{})["message"].(string)
	if errMsg != combat.ErrAttackerDead.Error() {
		t.Errorf("expected error message '%s', got '%s'", combat.ErrAttackerDead.Error(), errMsg)
	}

	// 2. Attacker is revived, but Defender is Dead: Revive attacker, kill defender
	_ = sessionReg.Revive(attackerID)
	_, _ = sessionReg.DeductHP(defenderID, 1000.0) // kills defender

	_ = ws.WriteMessage(websocket.TextMessage, attackMsgBytes)

	_, p, _ = ws.ReadMessage()
	t.Logf("SECOND ATTACK RESPONSE: %s", string(p))
	_ = json.Unmarshal(p, &errResponse)
	if errResponse["type"] != "COMBAT_ERROR" {
		t.Errorf("expected COMBAT_ERROR, got: %+v", errResponse)
	}
	errMsg = errResponse["payload"].(map[string]interface{})["message"].(string)
	if errMsg != combat.ErrDefenderDead.Error() {
		t.Errorf("expected error message '%s', got '%s'", combat.ErrDefenderDead.Error(), errMsg)
	}

	// Cleanup
	sessionReg.EndSession(attackerID)
	sessionReg.EndSession(defenderID)
}

func TestInvalidJWTToken(t *testing.T) {
	mux := http.NewServeMux()
	hub := socket.NewHub()
	go hub.Run()
	mux.HandleFunc("/ws", socket.ServeWS(hub, "my-secret", nil, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL, _ := url.Parse(server.URL)
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws"
	q := wsURL.Query()
	q.Set("token", "invalid-jwt-payload-string")
	wsURL.RawQuery = q.Encode()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	if err == nil {
		t.Errorf("expected connection to fail with invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status code 401 Unauthorized, got: %d", resp.StatusCode)
	}
}

// TestOutboxDispatchesRewardEvents proves the transactional-outbox path end to
// end against live Postgres: character.AddRewards writes RewardsGranted to the
// outbox in the SAME transaction as the reward, and the relay then decodes and
// republishes it on the bus, marking the row published. Requires PostgreSQL.
func TestOutboxDispatchesRewardEvents(t *testing.T) {
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping outbox integration test.")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping outbox integration test.")
	}
	migrateTestDB(t, pool)

	st := store.New(pool)

	// A user (FK target) + a character to reward.
	authRepo := auth.NewUserRepository(st)
	authService := auth.NewAuthService(authRepo, "outbox-test-secret-000000000000")
	uname := "outbox_" + time.Now().Format("150405.000000")
	user, err := authService.Register(ctx, uname, uname+"@test.com", "securepassword123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	charRepo := character.NewCharacterRepository(st)
	charService := character.NewCharacterService(charRepo, &mockStatClient{}, nil, nil, nil)
	charName := fmt.Sprintf("Ob%d", time.Now().UnixNano()%100000000000)
	char, err := charService.Create(ctx, user.ID, charName, "WARRIOR")
	if err != nil {
		t.Fatalf("create character: %v", err)
	}

	// Reward the character: RewardsGranted must land in the outbox, unpublished.
	if _, _, err := charService.AddRewards(ctx, char.ID, 50, 100); err != nil {
		t.Fatalf("add rewards: %v", err)
	}

	var unpublished int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE event_type = 'rewards_granted' AND published_at IS NULL`,
	).Scan(&unpublished)
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if unpublished == 0 {
		t.Fatal("expected an unpublished rewards_granted outbox row after AddRewards")
	}

	// The relay decodes and republishes it on the bus.
	eventBus := bus.NewInProc(nil)
	got := make(chan character.RewardsGranted, 16)
	eventBus.Subscribe(character.EventRewardsGranted, func(_ context.Context, ev bus.Event) {
		got <- ev.(character.RewardsGranted)
	})
	relay := outbox.NewRelay(st, eventBus, nil)
	character.RegisterEventDecoders(relay.Registry())
	if _, err := relay.Dispatch(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	// Our reward event must arrive (there may be leftover rows for other chars).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-got:
			if ev.CharacterID == char.ID {
				if ev.Gold != 50 || ev.Exp != 100 {
					t.Errorf("unexpected RewardsGranted: %+v", ev)
				}
				// And the row is now marked published.
				var stillUnpublished int
				_ = pool.QueryRow(ctx,
					`SELECT count(*) FROM outbox WHERE event_type='rewards_granted' AND published_at IS NULL AND (payload->>'CharacterID')::bigint = $1`,
					char.ID,
				).Scan(&stillUnpublished)
				if stillUnpublished != 0 {
					t.Errorf("expected the dispatched row to be marked published, %d still unpublished", stillUnpublished)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for our RewardsGranted from the relay")
		}
	}
}

// TestEventLogReplay proves the append-only event_log + replay against live
// Postgres: AddRewards records RewardsGranted in the character's stream (in the
// same tx), and Replay returns it for a `since` before the write but nothing for
// a `since` after it. Requires PostgreSQL.
func TestEventLogReplay(t *testing.T) {
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping event_log replay test.")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping event_log replay test.")
	}
	migrateTestDB(t, pool)

	st := store.New(pool)
	authRepo := auth.NewUserRepository(st)
	authService := auth.NewAuthService(authRepo, "elog-test-secret-0000000000000")
	uname := "elog_" + time.Now().Format("150405.000000")
	user, err := authService.Register(ctx, uname, uname+"@test.com", "securepassword123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	charRepo := character.NewCharacterRepository(st)
	charService := character.NewCharacterService(charRepo, &mockStatClient{}, nil, nil, nil)
	charName := fmt.Sprintf("El%d", time.Now().UnixNano()%100000000000)
	char, err := charService.Create(ctx, user.ID, charName, "WARRIOR")
	if err != nil {
		t.Fatalf("create character: %v", err)
	}

	before := time.Now().Add(-time.Second)
	if _, _, err := charService.AddRewards(ctx, char.ID, 30, 60); err != nil {
		t.Fatalf("add rewards: %v", err)
	}
	after := time.Now().Add(time.Second)

	reg := outbox.NewRegistry()
	character.RegisterEventDecoders(reg)
	stream := eventlog.CharacterStream(char.ID)

	// Replay since `before` must include the RewardsGranted just written.
	got, err := eventlog.Replay(ctx, st, reg, stream, before)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	var found bool
	for _, r := range got {
		if rg, ok := r.Event.(character.RewardsGranted); ok && rg.CharacterID == char.ID && rg.Gold == 30 && rg.Exp == 60 {
			found = true
		}
	}
	if !found {
		t.Errorf("replay since `before` did not return the RewardsGranted event; got %d events", len(got))
	}

	// Replay since `after` must return nothing for this character.
	later, err := eventlog.Replay(ctx, st, reg, stream, after)
	if err != nil {
		t.Fatalf("replay (after): %v", err)
	}
	if len(later) != 0 {
		t.Errorf("replay since `after` should be empty, got %d events", len(later))
	}
}

// migrateTestDB brings the connected database up to the latest schema so
// integration tests are self-sufficient on a fresh database (no manual table
// creation). RunMigrations is idempotent, so it is a no-op on an already-migrated
// database.
func migrateTestDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if err := (&database.DB{Pool: pool}).RunMigrations(context.Background()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
}

// TestOutboxPruneRemovesOldPublished proves the relay prunes only dispatched
// (published) rows older than the retention, keeping recent-published and
// undispatched rows. Requires PostgreSQL.
func TestOutboxPruneRemovesOldPublished(t *testing.T) {
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping outbox prune test.")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping outbox prune test.")
	}
	migrateTestDB(t, pool)

	et := fmt.Sprintf("prune_test_%d", time.Now().UnixNano())
	// old published (2 days ago), recent published (now), and undispatched.
	_, err = pool.Exec(ctx,
		`INSERT INTO outbox (event_type, payload, occurred_at, published_at) VALUES
			($1, '{}', now() - interval '2 days', now() - interval '2 days'),
			($1, '{}', now(), now()),
			($1, '{}', now(), NULL)`, et)
	if err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	relay := outbox.NewRelay(store.New(pool), bus.NewInProc(nil), nil)
	if _, err := relay.Prune(ctx, 24*time.Hour); err != nil {
		t.Fatalf("prune: %v", err)
	}

	var total, oldPublished int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE event_type = $1`, et).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE event_type = $1 AND published_at IS NOT NULL AND published_at < now() - interval '1 day'`, et,
	).Scan(&oldPublished); err != nil {
		t.Fatalf("count old: %v", err)
	}

	if oldPublished != 0 {
		t.Errorf("old published rows should be pruned, %d remain", oldPublished)
	}
	if total != 2 {
		t.Errorf("expected the recent-published and undispatched rows to survive (2), got %d", total)
	}

	_, _ = pool.Exec(context.Background(), `DELETE FROM outbox WHERE event_type = $1`, et)
}

// TestRefreshTokenRotationPg proves the Postgres-backed refresh store against
// live Postgres: login stores a refresh token, refresh rotates it (old rejected),
// and logout revokes it. Also exercises migration 000010. Requires PostgreSQL.
func TestRefreshTokenRotationPg(t *testing.T) {
	dbURL := "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skip("PostgreSQL not accessible, skipping refresh-token test.")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL running but ping failed, skipping refresh-token test.")
	}
	migrateTestDB(t, pool)

	st := store.New(pool)
	userRepo := auth.NewUserRepository(st)
	authService := auth.NewAuthService(userRepo, "refresh-test-secret-00000000000",
		auth.WithRefreshStore(auth.NewPgRefreshStore(st)))

	uname := "refresh_" + time.Now().Format("150405.000000")
	if _, err := authService.Register(ctx, uname, uname+"@test.com", "securepassword123"); err != nil {
		t.Fatalf("register: %v", err)
	}

	pair, err := authService.Login(ctx, uname, "securepassword123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	next, err := authService.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if next.RefreshToken == pair.RefreshToken {
		t.Error("refresh token was not rotated")
	}
	// The rotated-away token is now invalid in the DB.
	if _, err := authService.Refresh(ctx, pair.RefreshToken); err != auth.ErrInvalidRefreshToken {
		t.Errorf("expected ErrInvalidRefreshToken for reused token, got %v", err)
	}
	// Logout revokes the current token.
	if err := authService.Logout(ctx, next.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := authService.Refresh(ctx, next.RefreshToken); err != auth.ErrInvalidRefreshToken {
		t.Errorf("expected ErrInvalidRefreshToken after logout, got %v", err)
	}
}

// makeCreatures builds a creature.Resolver mirroring the production composite
// (mobs from the pack, then characters) for combat in integration tests.
func makeCreatures(charSvc character.CharacterService) creature.Resolver {
	mobs := content.MustLoadMobs()
	return creature.ResolverFunc(func(ctx context.Context, id int64) (creature.Creature, bool, error) {
		if def, ok := mobs.Mobs[strconv.FormatInt(id, 10)]; ok {
			return creature.Creature{
				ID: id, Kind: creature.KindMob, Level: def.Level, Defense: def.Defense,
				Dex: def.Dex, MaxHP: def.MaxHP, MaxMP: def.MaxMP,
				LootTableID: def.LootTableID, QuestTag: def.QuestTag,
			}, true, nil
		}
		c, err := charSvc.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, character.ErrCharacterNotFound) {
				return creature.Creature{}, false, nil
			}
			return creature.Creature{}, false, err
		}
		return creature.Creature{
			ID: id, Kind: creature.KindCharacter, Class: c.ClassName, Level: c.Level,
			Attack:      c.Stats.DerivedStats["ATTACK"],
			Defense:     c.Stats.DerivedStats["DEFENSE"],
			Dex:         c.Stats.BaseStats["DEX"],
			CritRate:    c.Stats.DerivedStats["CRIT_RATE"],
			MaxHP:       c.Stats.DerivedStats["HP"],
			MaxMP:       c.Stats.DerivedStats["MP"],
			LootTableID: mobs.PvP.LootTableID, QuestTag: mobs.PvP.QuestTag,
		}, true, nil
	})
}
