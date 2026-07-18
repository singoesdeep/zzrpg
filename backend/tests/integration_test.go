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
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/events"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
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
	db := &database.DB{Pool: pool}
	jwtSecret := "integration-test-secret-321-secure"

	authRepo := auth.NewUserRepository(db.Pool)
	authService := auth.NewAuthService(authRepo, jwtSecret)

	statClient := &mockStatClient{}

	charRepo := character.NewCharacterRepository(db.Pool)
	charService := character.NewCharacterService(charRepo, statClient, nil)

	itemRepo := items.NewItemRepository(db.Pool)
	itemService := items.NewItemService(itemRepo)
	_ = itemService

	invRepo := inventory.NewInventoryRepository(db.Pool)
	invService := inventory.NewInventoryService(invRepo, charService, events.Global())

	charService.SetEquipmentProvider(invService)

	questRepo := quests.NewQuestRepository(db.Pool)
	questService := quests.NewQuestService(questRepo, charService, invService)

	lootRepo := loot.NewLootRepository(db.Pool)
	lootService := loot.NewLootService(lootRepo)

	hub := socket.NewHub()
	go hub.Run()

	combatService := combat.NewCombatService(charService, statClient, socket.GetRegistry(), questService, lootService, invService)

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
