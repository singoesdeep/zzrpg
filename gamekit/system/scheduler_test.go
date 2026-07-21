package system_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
	"github.com/singoesdeep/zzrpg/gamekit/system"
	"github.com/singoesdeep/zzrpg/gamekit/world"
	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
)

type Prod struct{ RatePerMin float64 }
type Wallet struct{ Amount float64 }

// producer is a TickSystem: every entity with a "prod" component accrues into
// its "wallet" component, scaled by elapsed time — the generalised idle producer.
type producer struct {
	prod   component.Store[Prod]
	wallet component.Store[Wallet]
}

func (producer) Name() string            { return "producer" }
func (producer) Interval() time.Duration { return time.Minute }
func (producer) Query() []string         { return []string{"prod"} }
func (p producer) Tick(ctx context.Context, id int64, _ *world.World, elapsed time.Duration) error {
	pr, ok, err := p.prod.Get(ctx, id)
	if err != nil || !ok {
		return err
	}
	w, _, _ := p.wallet.Get(ctx, id)
	w.Amount += pr.RatePerMin * elapsed.Minutes()
	return p.wallet.Set(ctx, id, w)
}

func TestScheduler_TickWithOfflineCatchup(t *testing.T) {
	ctx := context.Background()
	entities := entity.NewMemRepo()
	w := world.New(entities)
	prodStore := component.NewMemStore[Prod]("prod")
	walletStore := component.NewMemStore[Wallet]("wallet")
	w.Register(prodStore)
	w.Register(walletStore)

	city, _ := entities.Create(ctx, "city", 1)
	_ = prodStore.Set(ctx, city.ID, Prod{RatePerMin: 10})

	lr := system.NewMemLastRun()
	sch := system.NewScheduler(w, bus.NewInProc(nil), lr)
	sys := producer{prod: prodStore, wallet: walletStore}
	sch.AddTick(sys)

	// Freeze the clock and pretend the system last ran 5 minutes ago (offline).
	now := time.Now()
	sch.SetClock(func() time.Time { return now })
	_ = lr.Set(ctx, sys.Name(), city.ID, now.Add(-5*time.Minute))

	if err := sch.TickAll(ctx, sys); err != nil {
		t.Fatalf("TickAll: %v", err)
	}
	if wv, _, _ := walletStore.Get(ctx, city.ID); wv.Amount != 50 { // 10/min * 5 min
		t.Fatalf("offline catch-up wrong: got %v, want 50", wv.Amount)
	}
	// last-run advanced to now, so a repeat tick with the same clock adds nothing.
	_ = sch.TickAll(ctx, sys)
	if wv, _, _ := walletStore.Get(ctx, city.ID); wv.Amount != 50 {
		t.Fatalf("second tick at same instant should add nothing, got %v", wv.Amount)
	}
}

type Attack struct{ Target int64 }

func (Attack) Name() string { return "ATTACK" }

// killer is an EventSystem reacting to an ATTACK event.
type killer struct{ hits *atomic.Int64 }

func (killer) Name() string { return "killer" }
func (killer) On() string   { return "ATTACK" }
func (k killer) Handle(_ context.Context, _ bus.Event, _ *world.World) error {
	k.hits.Add(1)
	return nil
}

func TestScheduler_EventSystem(t *testing.T) {
	ctx := context.Background()
	w := world.New(entity.NewMemRepo())
	b := bus.NewInProc(nil)
	sch := system.NewScheduler(w, b, system.NewMemLastRun())
	var hits atomic.Int64
	sch.AddEvent(killer{hits: &hits})
	sch.Run(ctx)

	_ = b.Publish(ctx, Attack{Target: 9})
	time.Sleep(20 * time.Millisecond) // bus dispatch is async
	if hits.Load() != 1 {
		t.Fatalf("expected the event system to handle 1 attack, got %d", hits.Load())
	}
}
