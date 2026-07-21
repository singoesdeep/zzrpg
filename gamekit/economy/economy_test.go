package economy

import (
	"context"
	"errors"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

func TestEarnSpendAndAffordability(t *testing.T) {
	ctx := context.Background()
	s := NewService(component.NewMemStore[Wallet]("wallet"), nil)

	if _, err := s.Earn(ctx, 1, "gold", 100); err != nil {
		t.Fatalf("earn: %v", err)
	}
	if bal, _ := s.Balance(ctx, 1, "gold"); bal != 100 {
		t.Fatalf("balance = %d, want 100", bal)
	}

	if ok, _ := s.CanAfford(ctx, 1, "gold", 150); ok {
		t.Fatal("should not afford 150")
	}
	if _, err := s.Spend(ctx, 1, "gold", 150); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("spend overdraft err = %v, want ErrInsufficient", err)
	}

	if _, err := s.Spend(ctx, 1, "gold", 40); err != nil {
		t.Fatalf("spend: %v", err)
	}
	if bal, _ := s.Balance(ctx, 1, "gold"); bal != 60 {
		t.Fatalf("balance after spend = %d, want 60", bal)
	}
}

func TestHooksTaxAndDiscount(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	// A 50% income tax and a flat 10-off discount — plugins the service never
	// knows about.
	hooks.AddFilter(h, HookEarn, 10, func(_ context.Context, c Change) Change { c.Amount /= 2; return c })
	hooks.AddFilter(h, HookSpend, 10, func(_ context.Context, c Change) Change { c.Amount -= 10; return c })

	s := NewService(component.NewMemStore[Wallet]("wallet"), h)

	s.Earn(ctx, 1, "gold", 100) // taxed to 50
	if bal, _ := s.Balance(ctx, 1, "gold"); bal != 50 {
		t.Fatalf("taxed balance = %d, want 50", bal)
	}
	s.Spend(ctx, 1, "gold", 30) // discounted to 20
	if bal, _ := s.Balance(ctx, 1, "gold"); bal != 30 {
		t.Fatalf("discounted balance = %d, want 30", bal)
	}
}
