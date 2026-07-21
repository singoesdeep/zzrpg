package idlekit

import (
	"context"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
)

// TestIdleSystemAccruesOverElapsed proves the ported mechanic: the gamekit
// TickSystem credits the economy wallet by rate × elapsed minutes — the same
// offline/online accrual the old idle Service did, now on gamekit primitives.
func TestIdleSystemAccruesOverElapsed(t *testing.T) {
	ctx := context.Background()
	prod := component.NewMemStore[Producer]("idlekit_producer")
	econ := economy.NewService(component.NewMemStore[economy.Wallet]("wallet"), nil)
	sys := idleSystem{prod: prod, econ: econ, interval: time.Minute}

	const eid = 1
	_ = prod.Set(ctx, eid, Producer{RatePerMin: 12, Resource: "gold"})

	// A 5-minute offline window → 12 * 5 = 60 gold.
	if err := sys.Tick(ctx, eid, nil, 5*time.Minute); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if bal, _ := econ.Balance(ctx, eid, "gold"); bal != 60 {
		t.Fatalf("gold = %d, want 60", bal)
	}

	// A further 30-minute window compounds onto the same wallet → +360 = 420.
	_ = sys.Tick(ctx, eid, nil, 30*time.Minute)
	if bal, _ := econ.Balance(ctx, eid, "gold"); bal != 420 {
		t.Fatalf("gold = %d, want 420", bal)
	}

	// An entity with no producer is a no-op (not every entity idles).
	if err := sys.Tick(ctx, 99, nil, time.Hour); err != nil {
		t.Fatalf("tick on producerless entity: %v", err)
	}
	if bal, _ := econ.Balance(ctx, 99, "gold"); bal != 0 {
		t.Fatalf("producerless gold = %d, want 0", bal)
	}
}

// TestPowerSumsDerivedStats pins the read-bridge: a character's power is the
// sum of its derived stats, which (with level) sets the producer rate.
func TestPowerSumsDerivedStats(t *testing.T) {
	got := power(map[string]float64{"ATTACK": 30, "DEFENSE": 15, "HP": 200})
	if got != 245 {
		t.Fatalf("power = %v, want 245", got)
	}
	if power(nil) != 0 {
		t.Fatal("power(nil) should be 0")
	}
}
