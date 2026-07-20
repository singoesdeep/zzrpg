package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func allowOnly(allowed string) func(string) bool {
	return func(o string) bool { return o == allowed }
}

func TestCORS_AllowedOriginReflected(t *testing.T) {
	h := CORS(allowOnly("https://game.example"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Origin", "https://game.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://game.example" {
		t.Fatalf("allowed origin not reflected: %q", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected passthrough 200, got %d", rec.Code)
	}
}

func TestCORS_DisallowedOriginGetsNoHeader(t *testing.T) {
	h := CORS(allowOnly("https://game.example"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin must get no CORS header, got %q", got)
	}
}

func TestCORS_PreflightShortCircuits(t *testing.T) {
	called := false
	h := CORS(allowOnly("https://game.example"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/x", nil)
	req.Header.Set("Origin", "https://game.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight should return 204, got %d", rec.Code)
	}
	if called {
		t.Fatal("preflight must not reach the next handler")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("preflight must advertise allowed methods")
	}
}
