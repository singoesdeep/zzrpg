package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestID assigns each request a correlation id (honouring an inbound
// X-Request-ID, otherwise generating one), echoes it on the response, and stores
// it in the request context so downstream handlers and the logger can include it.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the request id stored in ctx, or "" if none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// SecureHeaders sets conservative security response headers on every response.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-XSS-Protection", "0")
		next.ServeHTTP(w, r)
	})
}

// MaxBodyBytes caps request body size (0 disables). Reads past the limit fail,
// so oversized payloads can't exhaust memory.
func MaxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if n <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit throttles requests per client IP with a token bucket (rps sustained,
// burst headroom). rps <= 0 disables it. Over-limit requests get a 429.
func RateLimit(rps float64, burst int, log *slog.Logger) func(http.Handler) http.Handler {
	if rps <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	rl := newRateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientIP(r)) {
				w.Header().Set("Retry-After", "1")
				WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP resolves the caller's IP, preferring the first X-Forwarded-For entry
// (set by a trusted proxy) and falling back to the connection's remote address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// visitor is one IP's token bucket.
type visitor struct {
	tokens float64
	last   time.Time
}

// rateLimiter is a per-IP token bucket with idle-eviction so the map stays
// bounded regardless of how many distinct IPs are ever seen.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rps      float64
	burst    float64
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rps:      rps,
		burst:    float64(burst),
	}
	go rl.sweep()
	return rl
}

// allow refills the caller's bucket by elapsed time and consumes one token,
// returning false when the bucket is empty.
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[ip]
	if !ok {
		rl.visitors[ip] = &visitor{tokens: rl.burst - 1, last: now}
		return true
	}

	v.tokens += now.Sub(v.last).Seconds() * rl.rps
	if v.tokens > rl.burst {
		v.tokens = rl.burst
	}
	v.last = now
	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

// sweep evicts buckets whose IP has been idle long enough that its bucket is
// fully refilled anyway (so eviction is behaviourally transparent).
func (rl *rateLimiter) sweep() {
	for range time.Tick(3 * time.Minute) {
		cutoff := time.Now().Add(-3 * time.Minute)
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if v.last.Before(cutoff) {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}
