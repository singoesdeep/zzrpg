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

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
	"github.com/singoesdeep/zzrpg/backend/internal/killreward"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
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

	// 2. Initialize all modules in-memory
	db := &database.DB{Pool: pool, Store: store.New(pool)}
	jwtSecret := "integration-test-secret-321-secure"

	authRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}

	charRepo := character.NewCharacterRepository(db.Store)
	charService := character.NewCharacterService(charRepo, statClient, nil, nil)

	itemRepo := items.NewItemRepository(db.Store)
	itemService := items.NewItemService(itemRepo)
	_ = itemService

	invRepo := inventory.NewInventoryRepository(db.Store)
	invService := inventory.NewInventoryService(invRepo, charService, bus.NewInProc(nil))

	charService.SetEquipmentProvider(invService)

	questRepo := quests.NewQuestRepository(db.Store)
	questService := quests.NewQuestService(questRepo, charService, invService, nil)

	lootRepo := loot.NewLootRepository(db.Store)
	lootService := loot.NewLootService(lootRepo)

	hub := socket.NewHub()
	go hub.Run()

	combatService := combat.NewCombatService(charService, statClient, socket.GetRegistry(), killreward.New(charService, questService, lootService, invService, nil), nil)

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
				socket.GetRegistry().StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])
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
			socket.GetRegistry().EndSession(client.CharacterID)
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

	db := &database.DB{Pool: pool, Store: store.New(pool)}
	jwtSecret := "double-session-secret"

	authRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}
	charRepo := character.NewCharacterRepository(db.Store)
	charService := character.NewCharacterService(charRepo, statClient, nil, nil)

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

	db := &database.DB{Pool: pool, Store: store.New(pool)}
	jwtSecret := "dead-status-secret"

	authRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}
	charRepo := character.NewCharacterRepository(db.Store)
	charService := character.NewCharacterService(charRepo, statClient, nil, nil)

	invRepo := inventory.NewInventoryRepository(db.Store)
	invService := inventory.NewInventoryService(invRepo, charService, bus.NewInProc(nil))
	charService.SetEquipmentProvider(invService)

	questRepo := quests.NewQuestRepository(db.Store)
	questService := quests.NewQuestService(questRepo, charService, invService, nil)

	lootRepo := loot.NewLootRepository(db.Store)
	lootService := loot.NewLootService(lootRepo)

	hub := socket.NewHub()
	go hub.Run()

	combatService := combat.NewCombatService(charService, statClient, socket.GetRegistry(), killreward.New(charService, questService, lootService, invService, nil), nil)

	wsMsgHandler := func(client *socket.Client, msg socket.WSMessage) {
		switch msg.Type {
		case "SELECT_CHARACTER":
			var payload socket.SelectCharPayload
			errUn := json.Unmarshal(msg.Payload, &payload)
			fmt.Printf("DEBUG: msg.Payload = %s, payload.CharacterID = %d, err = %v\n", string(msg.Payload), payload.CharacterID, errUn)
			hub.AssociateCharacter(client, payload.CharacterID)

			char, err := charService.GetByID(context.Background(), payload.CharacterID)
			if err == nil {
				socket.GetRegistry().StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])
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

	sess, exists := socket.GetRegistry().GetSession(attackerID)
	t.Logf("ATTACKER SESSION BEFORE ATTACK: exists=%v, %+v", exists, sess)

	// Start Defender Session manually so it exists in registry
	_ = socket.GetRegistry().StartSession(defenderID, 500.0, 100.0)

	// 1. Attacker is Dead: Deduct all HP from attacker
	_, _ = socket.GetRegistry().DeductHP(attackerID, 1000.0) // kills attacker

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
	_ = socket.GetRegistry().Revive(attackerID)
	_, _ = socket.GetRegistry().DeductHP(defenderID, 1000.0) // kills defender

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
	socket.GetRegistry().EndSession(attackerID)
	socket.GetRegistry().EndSession(defenderID)
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
