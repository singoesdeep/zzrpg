package system

import (
	"context"
	"time"

	"github.com/singoesdeep/zzrpg/gamekit/world"
	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
)

// Scheduler owns the registered Systems and drives them: it ticks each
// TickSystem on its interval and subscribes each EventSystem to its event.
type Scheduler struct {
	world   *world.World
	bus     bus.EventBus
	lastRun LastRunStore

	ticks  []TickSystem
	events []EventSystem
	now    func() time.Time // injectable clock for tests
}

// NewScheduler builds a scheduler over a world, event bus, and last-run store.
func NewScheduler(w *world.World, b bus.EventBus, lr LastRunStore) *Scheduler {
	return &Scheduler{world: w, bus: b, lastRun: lr, now: time.Now}
}

// AddTick / AddEvent register systems (before Run).
func (s *Scheduler) AddTick(sys TickSystem)   { s.ticks = append(s.ticks, sys) }
func (s *Scheduler) AddEvent(sys EventSystem) { s.events = append(s.events, sys) }

// SetClock overrides the time source (for deterministic ticks in tests).
func (s *Scheduler) SetClock(now func() time.Time) { s.now = now }

// Run subscribes event systems and starts a ticker per tick system, until ctx is
// cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	for _, es := range s.events {
		es := es
		s.bus.Subscribe(es.On(), func(c context.Context, ev bus.Event) {
			_ = es.Handle(c, ev, s.world)
		})
	}
	for _, ts := range s.ticks {
		go s.runTicker(ctx, ts)
	}
}

func (s *Scheduler) runTicker(ctx context.Context, ts TickSystem) {
	if ts.Interval() <= 0 {
		return
	}
	t := time.NewTicker(ts.Interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.TickAll(ctx, ts)
		}
	}
}

// TickAll runs one tick system across every matching entity. Exposed so a tick
// can be driven deterministically (tests) or on demand.
func (s *Scheduler) TickAll(ctx context.Context, ts TickSystem) error {
	ids, err := s.world.Query(ctx, ts.Query()...)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.tickEntity(ctx, ts, id); err != nil {
			return err
		}
	}
	return nil
}

// Catchup runs a single tick system for a single entity immediately — used to
// settle offline progress when an entity loads/logs in.
func (s *Scheduler) Catchup(ctx context.Context, ts TickSystem, entityID int64) error {
	return s.tickEntity(ctx, ts, entityID)
}

// tickEntity computes the elapsed time since this system last ran for the entity
// (offline catch-up), runs the system, then records the new last-run time.
func (s *Scheduler) tickEntity(ctx context.Context, ts TickSystem, entityID int64) error {
	now := s.now()
	elapsed := ts.Interval()
	if last, ok, err := s.lastRun.Get(ctx, ts.Name(), entityID); err != nil {
		return err
	} else if ok {
		elapsed = now.Sub(last)
	}
	if err := ts.Tick(ctx, entityID, s.world, elapsed); err != nil {
		return err
	}
	return s.lastRun.Set(ctx, ts.Name(), entityID, now)
}
