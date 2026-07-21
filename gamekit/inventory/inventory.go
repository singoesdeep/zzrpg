// Package inventory is a lean gamekit inventory component: a stack list of items
// on an entity, with add/remove/has for economy loops (loot, crafting sinks). A
// game needing slots/equipment writes a richer inventory component on top of the
// same Store[T] seam.
package inventory

import (
	"context"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// HookAddItem is a Filter over an item before it enters an inventory (e.g. a
// plugin that doubles drops, or rejects an item by zeroing its quantity).
const HookAddItem = "inventory.additem"

// Item is one stack.
type Item struct {
	ItemID   string `json:"item_id"`
	Quantity int32  `json:"quantity"`
}

// Inventory is the component: an entity's item stacks.
type Inventory struct {
	Items []Item `json:"items"`
}

// Service manages the inventory component.
type Service struct {
	store component.Store[Inventory]
	hooks *hooks.Hooks
}

// NewService builds an inventory service. hooks may be nil.
func NewService(store component.Store[Inventory], h *hooks.Hooks) *Service {
	return &Service{store: store, hooks: h}
}

// Get returns an entity's inventory (empty when it has none).
func (s *Service) Get(ctx context.Context, entityID int64) (Inventory, error) {
	inv, _, err := s.store.Get(ctx, entityID)
	return inv, err
}

// AddItem stacks an item into the entity's inventory (after the HookAddItem
// filter). A non-positive resulting quantity is ignored.
func (s *Service) AddItem(ctx context.Context, entityID int64, item Item) error {
	if s.hooks != nil {
		item = hooks.ApplyFilters(s.hooks, ctx, HookAddItem, item)
	}
	if item.Quantity <= 0 {
		return nil
	}
	inv, err := s.Get(ctx, entityID)
	if err != nil {
		return err
	}
	for i := range inv.Items {
		if inv.Items[i].ItemID == item.ItemID {
			inv.Items[i].Quantity += item.Quantity
			return s.store.Set(ctx, entityID, inv)
		}
	}
	inv.Items = append(inv.Items, item)
	return s.store.Set(ctx, entityID, inv)
}

// Count returns how many of an item the entity holds.
func (s *Service) Count(ctx context.Context, entityID int64, itemID string) (int32, error) {
	inv, err := s.Get(ctx, entityID)
	if err != nil {
		return 0, err
	}
	for _, it := range inv.Items {
		if it.ItemID == itemID {
			return it.Quantity, nil
		}
	}
	return 0, nil
}

// RemoveItem removes qty of an item, returning false (no change) when the entity
// holds fewer than qty — the primitive economy sinks build on.
func (s *Service) RemoveItem(ctx context.Context, entityID int64, itemID string, qty int32) (bool, error) {
	if qty <= 0 {
		return true, nil
	}
	inv, err := s.Get(ctx, entityID)
	if err != nil {
		return false, err
	}
	for i := range inv.Items {
		if inv.Items[i].ItemID == itemID {
			if inv.Items[i].Quantity < qty {
				return false, nil
			}
			inv.Items[i].Quantity -= qty
			if inv.Items[i].Quantity == 0 {
				inv.Items = append(inv.Items[:i], inv.Items[i+1:]...)
			}
			return true, s.store.Set(ctx, entityID, inv)
		}
	}
	return false, nil
}
