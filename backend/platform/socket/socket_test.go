package socket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/singoesdeep/zzrpg/backend/game/auth"
)

func TestWebSocketHubAndClient(t *testing.T) {
	// 1. Setup Hub
	hub := NewHub()
	go hub.Run()

	jwtSecret := "my-test-secret-123"

	// Mock Message Handler
	msgHandler := func(client *Client, msg WSMessage) {
		switch msg.Type {
		case "CHAT":
			var payload ChatPayload
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
			var payload SelectCharPayload
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

	// 2. Start HTTP Test Server
	server := httptest.NewServer(ServeWS(context.Background(), hub, testAuth(jwtSecret), nil, msgHandler, nil))
	defer server.Close()

	// 3. Generate Valid JWT Token for singo
	claims := &auth.Claims{
		UserID:   42,
		Username: "singo",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString([]byte(jwtSecret))

	// 4. Dial WebSocket
	url := strings.Replace(server.URL, "http", "ws", 1) + "?token=" + tokenStr
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer ws.Close()

	// 5. Test Select Character
	selectPayload := WSMessage{
		Type:    "SELECT_CHARACTER",
		Payload: []byte(`{"character_id":123}`),
	}
	payloadBytes, _ := json.Marshal(selectPayload)
	err = ws.WriteMessage(websocket.TextMessage, payloadBytes)
	if err != nil {
		t.Fatalf("failed to write select character message: %v", err)
	}

	// Read SELECT_CHARACTER_ACK
	_, p, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var response map[string]interface{}
	_ = json.Unmarshal(p, &response)
	if response["type"] != "SELECT_CHARACTER_ACK" {
		t.Errorf("expected SELECT_CHARACTER_ACK, got %+v", response)
	}

	// Verify client character association in Hub
	client, exists := hub.GetClientByCharacterID(123)
	if !exists || client.UserID != 42 || client.Username != "singo" {
		t.Errorf("expected client session association with character 123, got: exists=%v, %+v", exists, client)
	}

	// 6. Test Chat Broadcast
	chatPayload := WSMessage{
		Type:    "CHAT",
		Payload: []byte(`{"message":"hello world"}`),
	}
	chatBytes, _ := json.Marshal(chatPayload)
	err = ws.WriteMessage(websocket.TextMessage, chatBytes)
	if err != nil {
		t.Fatalf("failed to write chat message: %v", err)
	}

	// Read Chat Broadcast
	_, p, err = ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read chat broadcast: %v", err)
	}

	var chatResponse map[string]interface{}
	_ = json.Unmarshal(p, &chatResponse)
	if chatResponse["type"] != "CHAT" {
		t.Errorf("expected type CHAT, got %+v", chatResponse)
	}

	payloadMap := chatResponse["payload"].(map[string]interface{})
	if payloadMap["username"] != "singo" || payloadMap["message"] != "hello world" {
		t.Errorf("unexpected chat broadcast payload: %+v", payloadMap)
	}
}

func testAuth(secret string) Authenticator {
	return func(token string) (int64, string, bool) {
		claims, err := auth.ParseAccessToken(secret, token)
		if err != nil {
			return 0, "", false
		}
		return claims.UserID, claims.Username, true
	}
}
