package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDGeneratesAndPropagates(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seen == "" {
		t.Fatal("no request id in context")
	}
	if got := rec.Header().Get("X-Request-ID"); got != seen {
		t.Errorf("response header %q != context id %q", got, seen)
	}
}

func TestRequestIDHonoursInbound(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "caller-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "caller-123" {
		t.Errorf("expected inbound id preserved, got %q", seen)
	}
}

func TestSecureHeadersSet(t *testing.T) {
	h := SecureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options: DENY")
	}
}

func TestMaxBodyBytesRejectsOversized(t *testing.T) {
	var readErr error
	h := MaxBodyBytes(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		for {
			_, err := r.Body.Read(buf)
			if err != nil {
				readErr = err
				return
			}
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("way more than eight bytes"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if readErr == nil {
		t.Fatal("expected reading past the limit to fail")
	}
	if !strings.Contains(readErr.Error(), "too large") {
		t.Errorf("expected a body-too-large error, got %v", readErr)
	}
}

func TestRateLimitAllowsBurstThenBlocks(t *testing.T) {
	// rps low, burst 3: the first 3 requests from one IP pass, the 4th is 429ed.
	h := RateLimit(0.0001, 3, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 3; i++ {
		if code := call("1.2.3.4"); code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, code)
		}
	}
	if code := call("1.2.3.4"); code != http.StatusTooManyRequests {
		t.Errorf("4th request: expected 429, got %d", code)
	}
	// A different IP has its own bucket and is unaffected.
	if code := call("5.6.7.8"); code != http.StatusOK {
		t.Errorf("distinct IP: expected 200, got %d", code)
	}
}

func TestRateLimitDisabledWhenRPSZero(t *testing.T) {
	h := RateLimit(0, 0, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("rate limiting should be disabled, got %d", rec.Code)
		}
	}
}
