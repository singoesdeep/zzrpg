package metrics

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type hijackableRecorder struct{ *httptest.ResponseRecorder }

func (hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

// TestMiddleware_PreservesHijacker guards the WebSocket upgrade path through the
// metrics middleware wrapper.
func TestMiddleware_PreservesHijacker(t *testing.T) {
	var ok bool
	h := New().Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = w.(http.Hijacker)
	}))
	h.ServeHTTP(hijackableRecorder{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if !ok {
		t.Fatal("metrics.Middleware hid http.Hijacker; WebSocket upgrades would fail")
	}
}
