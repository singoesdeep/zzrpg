package idle

import (
	"context"
	"errors"
	"testing"
	"time"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
	"github.com/singoesdeep/zzrpg/gamekit/inventory"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// mineActivity is a developer-supplied activity: 10 gold + 5 exp + 1 ore per
// minute — the kind of Producer a game plugin registers on the engine.
type mineActivity struct{}

func (mineActivity) Unlocked(eidle.State) bool { return true }
func (mineActivity) Produce(min float64, _ eidle.State, _ func() float64) eidle.Output {
	var o eidle.Output
	o.Add("gold", int64(10*min))
	o.Add("exp", int64(5*min))
	o.AddDrop("ore", int(min))
	return o
}

func newFixture(h *hooks.Hooks) (*Engine, *economy.Service, *progression.Service, *inventory.Service) {
	econ := economy.NewService(component.NewMemStore[economy.Wallet]("wallet"), nil)
	prog := progression.NewService(component.NewMemStore[progression.Progression]("progression"), progression.Curve{Base: 50, Exp: 2}, nil)
	inv := inventory.NewService(component.NewMemStore[inventory.Inventory]("inventory"), nil)

	reg := eidle.NewRegistry()
	reg.Register("mine", mineActivity{})

	eng := NewEngine(Deps{
		Registry: reg,
		Assign:   component.NewMemStore[Assignment](AssignmentComponent),
		Apply:    DefaultApplier(econ, prog, inv, "exp"),
		Hooks:    h,
	})
	return eng, econ, prog, inv
}

func TestSystemAccruesAndRoutesOutput(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	// A "gold rush" plugin doubles gold, without the activity knowing.
	hooks.AddFilter(h, HookOutput, 10, func(_ context.Context, o eidle.Output) eidle.Output {
		o.Amounts["gold"] *= 2
		return o
	})
	eng, econ, prog, inv := newFixture(h)

	if err := eng.Assign(ctx, 1, "mine"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// 5-minute offline window through the tick system (min gate 60s).
	sys := NewSystem(eng, time.Minute, 60, 0)
	if err := sys.Tick(ctx, 1, nil, 5*time.Minute); err != nil {
		t.Fatalf("tick: %v", err)
	}
	// gold 10*5=50, doubled → 100 into the wallet; exp 5*5=25 into progression;
	// ore 5 into inventory.
	if bal, _ := econ.Balance(ctx, 1, "gold"); bal != 100 {
		t.Fatalf("gold = %d, want 100", bal)
	}
	if pr, _ := prog.Get(ctx, 1); pr.XP != 25 {
		t.Fatalf("xp = %d, want 25", pr.XP)
	}
	if n, _ := inv.Count(ctx, 1, "ore"); n != 5 {
		t.Fatalf("ore = %d, want 5", n)
	}
}

func TestSystemBelowWindowIsNoOp(t *testing.T) {
	ctx := context.Background()
	eng, econ, _, _ := newFixture(nil)
	_ = eng.Assign(ctx, 1, "mine")

	sys := NewSystem(eng, time.Minute, 60, 0)
	if err := sys.Tick(ctx, 1, nil, 30*time.Second); err != nil { // below 60s gate
		t.Fatalf("tick: %v", err)
	}
	if bal, _ := econ.Balance(ctx, 1, "gold"); bal != 0 {
		t.Fatalf("gold = %d, want 0 (below window)", bal)
	}
}

func TestUnassignedAndUnknownActivity(t *testing.T) {
	ctx := context.Background()
	eng, _, _, _ := newFixture(nil)

	// Unassigned entity: accrual is a no-op.
	if _, ran, err := eng.Accrue(ctx, 7, 10); err != nil || ran {
		t.Fatalf("unassigned accrue ran=%v err=%v", ran, err)
	}
	// Unknown activity: Assign refuses it.
	var unk *UnknownActivityError
	if err := eng.Assign(ctx, 1, "nope"); !errors.As(err, &unk) {
		t.Fatalf("assign unknown err = %v, want UnknownActivityError", err)
	}
}
