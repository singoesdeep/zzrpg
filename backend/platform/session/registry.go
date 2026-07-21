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
//
// The pool mechanism itself — concurrency-safe HP/MP with race-safe kill
// reservation — is gamekit/vitals; this package is the adapter to this RPG's
// CharacterSession naming.
package session

import (
	gvitals "github.com/singoesdeep/zzrpg/gamekit/vitals"
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
	vitals *gvitals.Registry
}

// NewRegistry creates an empty session registry. Each call yields an independent
// instance, so tests and isolated worlds don't share session state.
func NewRegistry() *Registry {
	return &Registry{vitals: gvitals.NewRegistry()}
}

// StartSession creates (or replaces) a session and returns a value copy.
func (r *Registry) StartSession(charID int64, maxHP, maxMP float64) CharacterSession {
	return toSession(r.vitals.Start(charID, maxHP, maxMP))
}

// GetSession returns a consistent value snapshot of the session.
func (r *Registry) GetSession(charID int64) (CharacterSession, bool) {
	p, ok := r.vitals.Get(charID)
	return toSession(p), ok
}

// DeductHPAndReserveKill atomically applies damage and reports whether THIS
// call landed the killing blow (killedNow). Death-triggered side effects (loot,
// quest progress, rewards) must be gated on killedNow so that two concurrent
// attackers finishing the same target cannot both be credited with the kill.
func (r *Registry) DeductHPAndReserveKill(charID int64, amount float64) (hp float64, isDead bool, killedNow bool) {
	return r.vitals.DeductAndReserveKill(charID, amount)
}

func (r *Registry) EndSession(charID int64) {
	r.vitals.End(charID)
}

func (r *Registry) DeductHP(charID int64, amount float64) (float64, bool) {
	return r.vitals.DeductHP(charID, amount)
}

func (r *Registry) Heal(charID int64, amount float64) (float64, bool) {
	return r.vitals.Heal(charID, amount)
}

// SpendMP deducts amount from a character's current MP if it has enough,
// returning whether the spend succeeded. Used for skill mana costs.
func (r *Registry) SpendMP(charID int64, amount float64) bool {
	return r.vitals.SpendMP(charID, amount)
}

func (r *Registry) Revive(charID int64) bool {
	return r.vitals.Revive(charID)
}

func toSession(p gvitals.Pool) CharacterSession {
	return CharacterSession{
		CharacterID: p.EntityID,
		CurrentHP:   p.CurrentHP,
		MaxHP:       p.MaxHP,
		CurrentMP:   p.CurrentMP,
		MaxMP:       p.MaxMP,
		IsDead:      p.Dead,
	}
}
