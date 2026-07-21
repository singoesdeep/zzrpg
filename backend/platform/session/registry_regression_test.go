package session

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestDeductHPAndReserveKillCreditsExactlyOneKiller proves that when many
// attackers finish the same target concurrently, exactly one is credited with
// the kill. This guards against the double-reward bug where every attacker who
// saw IsDead==true triggered loot/quest progression. Run under -race to also
// confirm there is no data race on the session fields.
func TestDeductHPAndReserveKillCreditsExactlyOneKiller(t *testing.T) {
	r := NewRegistry()
	r.StartSession(7, 100, 50)

	const attackers = 50
	var wg sync.WaitGroup
	var kills int64
	for i := 0; i < attackers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Total damage (50*10=500) far exceeds 100 HP; only one call lands the kill.
			if _, _, killedNow := r.DeductHPAndReserveKill(7, 10); killedNow {
				atomic.AddInt64(&kills, 1)
			}
		}()
	}
	wg.Wait()

	if kills != 1 {
		t.Fatalf("expected exactly one killing blow to be credited, got %d", kills)
	}
	if sess, ok := r.GetSession(7); !ok || !sess.IsDead || sess.CurrentHP != 0 {
		t.Fatalf("expected target dead at 0 HP, got %+v (exists=%v)", sess, ok)
	}
}
