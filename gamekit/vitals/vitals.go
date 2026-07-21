// Package vitals is gamekit's live health/mana pool toolkit: a concurrency-safe,
// in-memory registry of an entity's HP/MP over a session — combat's transient
// state, not persisted. It is deliberately minimal and genre-neutral: an RPG
// character, an RTS unit, or a city-builder building all just have HP; the
// package has no concept of damage formulas, skills, or rewards, only the pool
// and the race-safe primitive multiplayer combat needs — reserving exactly one
// kill credit when several attackers finish the same target concurrently.
package vitals

import "sync"

// Pool is one entity's live HP/MP state.
type Pool struct {
	EntityID  int64
	CurrentHP float64
	MaxHP     float64
	CurrentMP float64
	MaxMP     float64
	Dead      bool
}

// Registry is a concurrency-safe collection of Pools, keyed by entity id. Each
// instance is independent, so tests and isolated worlds don't share state.
type Registry struct {
	mu    sync.RWMutex
	pools map[int64]*Pool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{pools: make(map[int64]*Pool)}
}

// Start creates (or replaces) a pool at full HP/MP and returns a value copy.
// The registry keeps the authoritative pointer internally; callers never
// receive it, so a pool's fields can only be mutated through the registry's
// locked methods.
func (r *Registry) Start(entityID int64, maxHP, maxMP float64) Pool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := &Pool{EntityID: entityID, MaxHP: maxHP, CurrentHP: maxHP, MaxMP: maxMP, CurrentMP: maxMP}
	r.pools[entityID] = p
	return *p
}

// Get returns a consistent value snapshot of the pool taken under the read
// lock — never the internal pointer, so a caller can't race a concurrent
// mutation.
func (r *Registry) Get(entityID int64) (Pool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pools[entityID]
	if !ok {
		return Pool{}, false
	}
	return *p, true
}

// End removes a pool (session teardown).
func (r *Registry) End(entityID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pools, entityID)
}

// DeductAndReserveKill atomically applies damage and reports whether THIS call
// landed the killing blow (killedNow). Death-triggered side effects (loot,
// quest progress, rewards) must be gated on killedNow so that two concurrent
// attackers finishing the same target cannot both be credited with the kill —
// the primitive this package exists for.
func (r *Registry) DeductAndReserveKill(entityID int64, amount float64) (hp float64, dead bool, killedNow bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pools[entityID]
	if !ok {
		return 0, false, false
	}
	if p.Dead {
		// Already dead: this attack did not land the kill.
		return 0, true, false
	}
	p.CurrentHP -= amount
	if p.CurrentHP <= 0 {
		p.CurrentHP = 0
		p.Dead = true
		killedNow = true
	}
	return p.CurrentHP, p.Dead, killedNow
}

// DeductHP applies damage without kill reservation, for callers that don't need
// (or already handle) kill-credit races.
func (r *Registry) DeductHP(entityID int64, amount float64) (hp float64, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pools[entityID]
	if !ok {
		return 0, false
	}
	if p.Dead {
		return 0, true
	}
	p.CurrentHP -= amount
	if p.CurrentHP <= 0 {
		p.CurrentHP = 0
		p.Dead = true
	}
	return p.CurrentHP, p.Dead
}

// Heal restores HP up to MaxHP. It is a no-op (ok=false) on a dead or missing
// pool — reviving is Revive's job.
func (r *Registry) Heal(entityID int64, amount float64) (hp float64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, exists := r.pools[entityID]
	if !exists || p.Dead {
		return 0, false
	}
	p.CurrentHP += amount
	if p.CurrentHP > p.MaxHP {
		p.CurrentHP = p.MaxHP
	}
	return p.CurrentHP, true
}

// SpendMP deducts amount from an entity's current MP if it has enough,
// reporting whether the spend succeeded.
func (r *Registry) SpendMP(entityID int64, amount float64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pools[entityID]
	if !ok || p.CurrentMP < amount {
		return false
	}
	p.CurrentMP -= amount
	return true
}

// Revive restores a pool to full HP and clears Dead.
func (r *Registry) Revive(entityID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pools[entityID]
	if !ok {
		return false
	}
	p.CurrentHP = p.MaxHP
	p.Dead = false
	return true
}
