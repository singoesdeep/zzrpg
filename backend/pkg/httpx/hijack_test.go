package httpx

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// hijackableRecorder is an httptest recorder that also satisfies http.Hijacker,
// standing in for the real server's connection writer.
type hijackableRecorder struct{ *httptest.ResponseRecorder }

func (hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

// TestRequestLogger_PreservesHijacker guards the WebSocket upgrade path: the
// logging wrapper must not hide http.Hijacker, or /ws breaks with 500.
func TestRequestLogger_PreservesHijacker(t *testing.T) {
	var ok bool
	h := RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok = w.(http.Hijacker)
		}))
	h.ServeHTTP(hijackableRecorder{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if !ok {
		t.Fatal("RequestLogger hid http.Hijacker; WebSocket upgrades would fail")
	}
}
