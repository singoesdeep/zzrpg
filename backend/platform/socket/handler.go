package socket

import (
	"context"
	"log/slog"
	"net/http"
)

// Authenticator validates a connection token and returns the authenticated
// user's identity. Returning ok=false rejects the upgrade with 401. It keeps
// the transport layer free of any JWT/auth-domain dependency: the caller
// injects the concrete scheme.
type Authenticator func(token string) (userID int64, username string, ok bool)

// ServeWS returns the /ws HTTP handler. baseCtx is the parent for every
// connection's context (typically the server's run context) so in-flight
// message handling is cancelled on server shutdown as well as on disconnect.
// authenticate validates the ?token query parameter. allowOrigin guards the
// upgrade against cross-site WebSocket hijacking; when nil, all origins are
// allowed (development/tests only).
func ServeWS(baseCtx context.Context, hub *Hub, authenticate Authenticator, allowOrigin func(origin string) bool, msgHandler func(*Client, WSMessage), disconnectHandler func(*Client)) http.HandlerFunc {
	upgrader.CheckOrigin = func(r *http.Request) bool {
		if allowOrigin == nil {
			return true
		}
		return allowOrigin(r.Header.Get("Origin"))
	}

	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, username, ok := authenticate(tokenStr)
		if !ok {
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
			UserID:   userID,
			Username: username,
			ctx:      connCtx,
			cancel:   cancel,
		}

		client.Hub.Register <- client

		// Start reader and writer loops in separate goroutines
		go client.WritePump()
		go client.ReadPump(msgHandler, disconnectHandler)
	}
}
