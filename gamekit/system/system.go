// Package system is the gamekit engine abstraction — the generalisation of the
// idle Producer. A game's behaviour is a set of Systems the scheduler drives:
// TickSystems run periodically over entities (with offline catch-up), and
// EventSystems react to events. Combat is an EventSystem; city production is a
// TickSystem — a game swaps engines by choosing which Systems to register.
package system

import (
	"context"
	"sync"
	"time"

	"github.com/singoesdeep/zzrpg/gamekit/world"
	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
)

// TickSystem runs on an interval over every entity that has its required
// components. elapsed is the real time since this system last ran for that
// entity, so an interval tick and an offline catch-up use the same code path.
type TickSystem interface {
	Name() string
	Interval() time.Duration
	Query() []string // component names an entity must have to be processed
	Tick(ctx context.Context, entityID int64, w *world.World, elapsed time.Duration) error
}

// EventSystem reacts to a named bus event.
type EventSystem interface {
	Name() string
	On() string
	Handle(ctx context.Context, ev bus.Event, w *world.World) error
}

// LastRunStore persists when a TickSystem last ran for an entity, which is what
// makes offline catch-up work across restarts. An in-memory implementation
// gives online catch-up; a persisted one (a table) gives offline catch-up.
type LastRunStore interface {
	Get(ctx context.Context, system string, entityID int64) (time.Time, bool, error)
	Set(ctx context.Context, system string, entityID int64, at time.Time) error
}

type lrKey struct {
	system   string
	entityID int64
}

type memLastRun struct {
	mu sync.Mutex
	m  map[lrKey]time.Time
}

// NewMemLastRun returns an in-memory LastRunStore (online catch-up only).
func NewMemLastRun() LastRunStore { return &memLastRun{m: map[lrKey]time.Time{}} }

func (s *memLastRun) Get(_ context.Context, system string, entityID int64) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[lrKey{system, entityID}]
	return t, ok, nil
}

func (s *memLastRun) Set(_ context.Context, system string, entityID int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[lrKey{system, entityID}] = at
	return nil
}
