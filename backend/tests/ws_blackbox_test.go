package tests

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/singoesdeep/zzrpg/sdk/engine/kernel"
	"github.com/singoesdeep/zzrpg/sdk/pkg/config"

	"github.com/singoesdeep/zzrpg/backend/platform/socket"
)

// TestBlackBox_MiddlewareChain exercises the *assembled* kernel middleware chain
// end-to-end over a real HTTP/WebSocket server — the gap the per-middleware unit
// tests miss. It is the regression guard for the class of bug where a wrapping
// ResponseWriter breaks WebSocket upgrades (the http.Hijacker incident): plain
// requests must pass with the engine's hardening headers, and a real WS upgrade
// must succeed THROUGH the full chain, not just against ServeWS in isolation.
func TestBlackBox_MiddlewareChain(t *testing.T) {
	cfg := &config.Config{
		Env:            "development",
		RateLimitRPS:   1000,
		RateLimitBurst: 1000,
		MaxBodyBytes:   1 << 20,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	k := kernel.New(cfg, log)

	hub := socket.NewHub()
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
	authenticate := func(string) (int64, string, bool) { return 1, "tester", true }
	echo := func(c *socket.Client, m socket.WSMessage) {
		c.Send <- append([]byte("echo:"), m.Payload...)
	}
	mux.HandleFunc("/ws", socket.ServeWS(context.Background(), hub, authenticate, nil, echo, nil))

	srv := httptest.NewServer(k.Harden(mux))
	defer srv.Close()

	// 1) A normal request passes through the chain and picks up engine hardening.
	resp, err := http.Get(srv.URL + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "pong" {
		t.Fatalf("body = %q, want pong", body)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("middleware chain did not assign X-Request-ID")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("middleware chain did not apply security headers")
	}

	// 2) A real WebSocket upgrade must succeed through the entire chain.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?token=x"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket upgrade failed through the middleware chain: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"PING","payload":"hi"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.HasPrefix(msg, []byte("echo:")) {
		t.Fatalf("unexpected echo %q", msg)
	}
}
