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

func (r *SessionRegistry) StartSession(charID int64, maxHP, maxMP float64) *CharacterSession {
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
	return session
}

func (r *SessionRegistry) GetSession(charID int64) (*CharacterSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, exists := r.sessions[charID]
	return sess, exists
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
