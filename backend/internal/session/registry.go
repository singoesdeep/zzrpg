// Package session holds the in-memory combat/health aggregate: a per-character
// live session (HP/MP, alive/dead) and the registry that owns it. This is domain
// state, not transport, so it lives here rather than in the socket (transport)
// package — combat and the character plugin depend on session directly, keeping
// the transport layer free of gameplay state.
//
// The registry is created and owned by the kernel (the core plugin constructs it
// and provides it through the DI registry); consumers receive it by injection,
// not via a package global. This makes it possible to run isolated worlds/tests
// with independent session state.
package session

import (
	"sync"
)

type CharacterSession struct {
	CharacterID int64
	CurrentHP   float64
	MaxHP       float64
	CurrentMP   float64
	MaxMP       float64
	IsDead      bool
}

type Registry struct {
	mu       sync.RWMutex
	sessions map[int64]*CharacterSession
}

// NewRegistry creates an empty session registry. Each call yields an independent
// instance, so tests and isolated worlds don't share session state.
func NewRegistry() *Registry {
	return &Registry{
		sessions: make(map[int64]*CharacterSession),
	}
}

// StartSession creates (or replaces) a session and returns a value copy. The
// registry keeps the authoritative pointer internally; callers never receive it,
// so session fields can only be mutated through the registry's locked methods.
func (r *Registry) StartSession(charID int64, maxHP, maxMP float64) CharacterSession {
	r.mu.Lock()
	defer r.mu.Unlock()

	session := &CharacterSession{
		CharacterID: charID,
		MaxHP:       maxHP,
		CurrentHP:   maxHP,
		MaxMP:       maxMP,
		CurrentMP:   maxMP,
		IsDead:      false,
	}
	r.sessions[charID] = session
	return *session
}

// GetSession returns a consistent value snapshot of the session taken under the
// read lock. Returning a copy (not the internal pointer) prevents data races
// where a caller reads session fields while another goroutine mutates them.
func (r *Registry) GetSession(charID int64) (CharacterSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, exists := r.sessions[charID]
	if !exists {
		return CharacterSession{}, false
	}
	return *sess, true
}

// DeductHPAndReserveKill atomically applies damage and reports whether THIS call
// landed the killing blow (killedNow). Death-triggered side effects (loot, quest
// progress, rewards) must be gated on killedNow so that two concurrent attackers
// finishing the same target cannot both be credited with the kill.
func (r *Registry) DeductHPAndReserveKill(charID int64, amount float64) (hp float64, isDead bool, killedNow bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, exists := r.sessions[charID]
	if !exists {
		return 0, false, false
	}
	if sess.IsDead {
		// Already dead: this attack did not land the kill.
		return 0, true, false
	}

	sess.CurrentHP -= amount
	if sess.CurrentHP <= 0 {
		sess.CurrentHP = 0
		sess.IsDead = true
		killedNow = true
	}
	return sess.CurrentHP, sess.IsDead, killedNow
}

func (r *Registry) EndSession(charID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, charID)
}

func (r *Registry) DeductHP(charID int64, amount float64) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, exists := r.sessions[charID]
	if !exists {
		return 0, false
	}

	if sess.IsDead {
		return 0, true
	}

	sess.CurrentHP -= amount
	if sess.CurrentHP <= 0 {
		sess.CurrentHP = 0
		sess.IsDead = true
	}

	return sess.CurrentHP, sess.IsDead
}

func (r *Registry) Heal(charID int64, amount float64) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, exists := r.sessions[charID]
	if !exists {
		return 0, false
	}

	if sess.IsDead {
		return 0, false
	}

	sess.CurrentHP += amount
	if sess.CurrentHP > sess.MaxHP {
		sess.CurrentHP = sess.MaxHP
	}

	return sess.CurrentHP, true
}

func (r *Registry) Revive(charID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, exists := r.sessions[charID]
	if !exists {
		return false
	}

	sess.CurrentHP = sess.MaxHP
	sess.IsDead = false
	return true
}
