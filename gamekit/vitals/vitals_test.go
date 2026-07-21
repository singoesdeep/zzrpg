package vitals

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestStartGetEnd(t *testing.T) {
	r := NewRegistry()
	got := r.Start(1, 100, 50)
	if got != (Pool{EntityID: 1, CurrentHP: 100, MaxHP: 100, CurrentMP: 50, MaxMP: 50}) {
		t.Fatalf("Start = %+v", got)
	}
	if p, ok := r.Get(1); !ok || p.CurrentHP != 100 {
		t.Fatalf("Get after start = %+v, ok=%v", p, ok)
	}
	r.End(1)
	if _, ok := r.Get(1); ok {
		t.Fatal("pool should be gone after End")
	}
}

func TestDeductAndReserveKillCreditsExactlyOneKiller(t *testing.T) {
	r := NewRegistry()
	r.Start(7, 100, 50)

	const attackers = 50
	var wg sync.WaitGroup
	var kills int64
	for i := 0; i < attackers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, killedNow := r.DeductAndReserveKill(7, 10); killedNow {
				atomic.AddInt64(&kills, 1)
			}
		}()
	}
	wg.Wait()

	if kills != 1 {
		t.Fatalf("expected exactly one killing blow credited, got %d", kills)
	}
	if p, ok := r.Get(7); !ok || !p.Dead || p.CurrentHP != 0 {
		t.Fatalf("expected dead at 0 HP, got %+v (ok=%v)", p, ok)
	}
}

func TestDeductAndReserveKillOnMissingOrDeadPool(t *testing.T) {
	r := NewRegistry()
	if hp, dead, killed := r.DeductAndReserveKill(99, 10); hp != 0 || dead || killed {
		t.Fatalf("missing pool: hp=%v dead=%v killed=%v", hp, dead, killed)
	}
	r.Start(1, 10, 0)
	r.DeductAndReserveKill(1, 100) // kills it
	if hp, dead, killed := r.DeductAndReserveKill(1, 10); hp != 0 || !dead || killed {
		t.Fatalf("already dead: hp=%v dead=%v killed=%v, want killed=false (no double credit)", hp, dead, killed)
	}
}

func TestHealClampsToMaxAndRefusesDead(t *testing.T) {
	r := NewRegistry()
	r.Start(1, 100, 0)
	r.DeductHP(1, 40)
	if hp, ok := r.Heal(1, 1000); !ok || hp != 100 {
		t.Fatalf("heal should clamp to max: hp=%v ok=%v", hp, ok)
	}
	r.DeductHP(1, 200) // kill it
	if _, ok := r.Heal(1, 10); ok {
		t.Fatal("heal on a dead pool should refuse")
	}
}

func TestSpendMPRefusesInsufficientBalance(t *testing.T) {
	r := NewRegistry()
	r.Start(1, 100, 20)
	if !r.SpendMP(1, 15) {
		t.Fatal("should afford 15/20 MP")
	}
	if r.SpendMP(1, 10) { // only 5 left
		t.Fatal("should not afford 10/5 remaining MP")
	}
}

func TestRevive(t *testing.T) {
	r := NewRegistry()
	r.Start(1, 100, 0)
	r.DeductAndReserveKill(1, 200)
	if !r.Revive(1) {
		t.Fatal("revive should succeed on existing pool")
	}
	p, _ := r.Get(1)
	if p.Dead || p.CurrentHP != p.MaxHP {
		t.Fatalf("revived pool = %+v, want alive at max HP", p)
	}
	if r.Revive(404) {
		t.Fatal("revive on missing pool should fail")
	}
}
