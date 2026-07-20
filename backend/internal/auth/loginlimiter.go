package auth

import (
	"sync"
	"time"
)

// Default brute-force policy: this many failed logins for one username within
// the lockout window locks further attempts for that window.
const (
	defaultLoginMaxFailures = 5
	defaultLoginLockout     = 15 * time.Minute
)

// loginLimiter throttles password guessing per username. It is a per-node,
// in-memory guard (a first layer on top of the per-IP HTTP rate limit); the map
// is idle-evicted so it stays bounded.
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]*loginAttempt
	max     int
	lockout time.Duration
}

type loginAttempt struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

func newLoginLimiter(max int, lockout time.Duration) *loginLimiter {
	l := &loginLimiter{
		entries: make(map[string]*loginAttempt),
		max:     max,
		lockout: lockout,
	}
	go l.sweep()
	return l
}

// locked reports whether key is currently locked out.
func (l *loginLimiter) locked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	return ok && time.Now().Before(e.lockedUntil)
}

// fail records a failed attempt, locking the key once max failures accumulate
// within one lockout window. A window that has fully elapsed resets the count.
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowStart) > l.lockout {
		l.entries[key] = &loginAttempt{failures: 1, windowStart: now}
		return
	}
	e.failures++
	if e.failures >= l.max {
		e.lockedUntil = now.Add(l.lockout)
	}
}

// success clears any failure state for key (a correct login resets the counter).
func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// sweep periodically evicts entries that are neither locked nor within an active
// window, keeping the map bounded.
func (l *loginLimiter) sweep() {
	for range time.Tick(l.lockout) {
		l.mu.Lock()
		now := time.Now()
		for k, e := range l.entries {
			if now.After(e.lockedUntil) && now.Sub(e.windowStart) > l.lockout {
				delete(l.entries, k)
			}
		}
		l.mu.Unlock()
	}
}
