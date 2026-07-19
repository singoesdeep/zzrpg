package socket

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

type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[int64]*CharacterSession
}

var globalRegistry = &SessionRegistry{
	sessions: make(map[int64]*CharacterSession),
}

func GetRegistry() *SessionRegistry {
	return globalRegistry
}

// StartSession creates (or replaces) a session and returns a value copy. The
// registry keeps the authoritative pointer internally; callers never receive it,
// so session fields can only be mutated through the registry's locked methods.
func (r *SessionRegistry) StartSession(charID int64, maxHP, maxMP float64) CharacterSession {
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
func (r *SessionRegistry) GetSession(charID int64) (CharacterSession, bool) {
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
func (r *SessionRegistry) DeductHPAndReserveKill(charID int64, amount float64) (hp float64, isDead bool, killedNow bool) {
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

func (r *SessionRegistry) EndSession(charID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, charID)
}

func (r *SessionRegistry) DeductHP(charID int64, amount float64) (float64, bool) {
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

func (r *SessionRegistry) Heal(charID int64, amount float64) (float64, bool) {
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

func (r *SessionRegistry) Revive(charID int64) bool {
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
