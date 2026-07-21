// Package economy is a gamekit wallet toolkit: an entity holds balances of
// named currencies (gold, wood, mana, influence, …) with affordability-checked
// earn/spend. It is deliberately genre-neutral — an RPG spends gold to craft, an
// RTS spends minerals to build, a city-builder spends influence to develop — all
// on the same seam. Both operations route through hooks so plugins can tax,
// discount, cap, or react without the callers knowing about each other.
package economy

import (
	"context"
	"errors"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

const (
	// HookEarn is a Filter over a Change before crediting a wallet (a plugin
	// that doubles income, or a resource cap that clamps the amount).
	HookEarn = "economy.earn"
	// HookSpend is a Filter over a Change before debiting a wallet (a discount
	// or surcharge). The affordability check runs on the filtered amount.
	HookSpend = "economy.spend"
)

// ErrInsufficient is returned by Spend when the wallet cannot afford the cost.
var ErrInsufficient = errors.New("economy: insufficient funds")

// Wallet is the component: balances per currency.
type Wallet struct {
	Balances map[string]int64 `json:"balances"`
}

// Change is the hook payload: which entity, which currency, and how much is
// about to move. Filters may adjust Amount (never below zero).
type Change struct {
	EntityID int64
	Currency string
	Amount   int64
}

// Service manages the wallet component.
type Service struct {
	store component.Store[Wallet]
	hooks *hooks.Hooks
}

// NewService builds an economy service. hooks may be nil.
func NewService(store component.Store[Wallet], h *hooks.Hooks) *Service {
	return &Service{store: store, hooks: h}
}

// Get returns an entity's wallet (empty when it has none).
func (s *Service) Get(ctx context.Context, entityID int64) (Wallet, error) {
	w, _, err := s.store.Get(ctx, entityID)
	return w, err
}

// Balance returns the entity's balance of a single currency.
func (s *Service) Balance(ctx context.Context, entityID int64, currency string) (int64, error) {
	w, err := s.Get(ctx, entityID)
	if err != nil {
		return 0, err
	}
	return w.Balances[currency], nil
}

// CanAfford reports whether the entity holds at least amount of currency. It
// does not apply the spend filter — use it for pre-checks; Spend is the source
// of truth.
func (s *Service) CanAfford(ctx context.Context, entityID int64, currency string, amount int64) (bool, error) {
	bal, err := s.Balance(ctx, entityID, currency)
	if err != nil {
		return false, err
	}
	return bal >= amount, nil
}

// Earn credits the wallet (after the HookEarn filter). A non-positive resulting
// amount is a no-op. Returns the updated wallet.
func (s *Service) Earn(ctx context.Context, entityID int64, currency string, amount int64) (Wallet, error) {
	c := s.filter(ctx, HookEarn, Change{EntityID: entityID, Currency: currency, Amount: amount})
	w, err := s.Get(ctx, entityID)
	if err != nil {
		return Wallet{}, err
	}
	if c.Amount <= 0 {
		return w, nil
	}
	if w.Balances == nil {
		w.Balances = map[string]int64{}
	}
	w.Balances[c.Currency] += c.Amount
	return w, s.store.Set(ctx, entityID, w)
}

// Spend debits the wallet (after the HookSpend filter), failing with
// ErrInsufficient when the filtered cost exceeds the balance. A non-positive
// resulting amount is a no-op. Returns the updated wallet.
func (s *Service) Spend(ctx context.Context, entityID int64, currency string, amount int64) (Wallet, error) {
	c := s.filter(ctx, HookSpend, Change{EntityID: entityID, Currency: currency, Amount: amount})
	w, err := s.Get(ctx, entityID)
	if err != nil {
		return Wallet{}, err
	}
	if c.Amount <= 0 {
		return w, nil
	}
	if w.Balances[c.Currency] < c.Amount {
		return w, ErrInsufficient
	}
	w.Balances[c.Currency] -= c.Amount
	return w, s.store.Set(ctx, entityID, w)
}

func (s *Service) filter(ctx context.Context, hook string, c Change) Change {
	if s.hooks != nil {
		c = hooks.ApplyFilters(s.hooks, ctx, hook, c)
	}
	if c.Amount < 0 {
		c.Amount = 0
	}
	return c
}
