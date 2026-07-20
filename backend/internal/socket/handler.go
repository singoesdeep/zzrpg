package socket

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
)

// ServeWS returns the /ws HTTP handler. baseCtx is the parent for every
// connection's context (typically the server's run context) so in-flight
// message handling is cancelled on server shutdown as well as on disconnect.
func ServeWS(baseCtx context.Context, hub *Hub, jwtSecret string, msgHandler func(*Client, WSMessage), disconnectHandler func(*Client)) http.HandlerFunc {
	// Configure upgrader to allow all origins in development
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Validate JWT Token (pin HS256 to prevent algorithm-substitution attacks)
		claims := &auth.Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{"HS256"}))

		if err != nil || !token.Valid {
			http.Error(w, "UnauthorizedClaim", http.StatusUnauthorized)
			return
		}

		// Upgrade to websocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("failed to upgrade websocket connection", "error", err)
			return
		}

		connCtx, cancel := context.WithCancel(baseCtx)
		client := &Client{
			Hub:      hub,
			Conn:     conn,
			Send:     make(chan []byte, 256),
			UserID:   claims.UserID,
			Username: claims.Username,
			ctx:      connCtx,
			cancel:   cancel,
		}

		client.Hub.Register <- client

		// Start reader and writer loops in separate goroutines
		go client.WritePump()
		go client.ReadPump(msgHandler, disconnectHandler)
	}
}
