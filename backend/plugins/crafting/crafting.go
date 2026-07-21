// Package crafting is a resource sink: it consumes idle-generated wallet
// resources (wood, stone, metal, …) plus optional gold and produces inventory
// items, closing the economy loop generators → resources → crafting → gear.
// It is a self-contained plugin — its own content (recipes) and item-definition
// seeding — that depends only on the character, inventory, and idle-wallet
// services it resolves.
package crafting

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/engine/store"

	"github.com/singoesdeep/zzrpg/backend/game/inventory"
)

// ContentKind is crafting's own content type in the generic content registry.
const ContentKind = "crafting_recipe"

var (
	ErrUnknownRecipe    = errors.New("crafting: unknown recipe")
	ErrInsufficientRes  = errors.New("crafting: insufficient resources")
	ErrInsufficientGold = errors.New("crafting: insufficient gold")

	ServiceKey = registry.NewKey[*Service]("crafting")
)

// Output is the item a recipe produces.
type Output struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	SlotType string `json:"slot_type"`
	Quantity int32  `json:"quantity"`
}

// Recipe consumes wallet resources (Cost) and gold (GoldCost) to make Output.
type Recipe struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Cost     map[string]int64 `json:"cost"`
	GoldCost int64            `json:"gold_cost"`
	Output   Output           `json:"output"`
}

// Wallet is the resource wallet crafting spends from (satisfied structurally by
// the idle plugin's wallet, resolved without importing idle).
type Wallet interface {
	Balances(ctx context.Context, charID int64) (map[string]int64, error)
	Credit(ctx context.Context, charID int64, resourceID string, amount int64) error
}

// GoldSpender debits a character's gold (satisfied by the character service).
type GoldSpender interface {
	SpendGold(ctx context.Context, charID int64, amount int64) (bool, error)
}

// ItemGranter adds an item to a character's inventory (satisfied by the
// inventory service). Segregated to the one method crafting needs.
type ItemGranter interface {
	AddItem(ctx context.Context, item *inventory.InventoryItem) error
}

// Service holds crafting's dependencies and loaded recipes.
type Service struct {
	db     store.Store
	reg    *registry.Registry
	wallet Wallet
	gold   GoldSpender
	inv    ItemGranter
	order  []string
}

// NewService defines the recipe content type, loads the embedded recipe pack,
// and seeds the recipes' output item definitions (idempotently) so crafted
// items satisfy the inventory foreign key.
func NewService(ctx context.Context, db store.Store, reg *registry.Registry, wallet Wallet, gold GoldSpender, inv ItemGranter, recipesJSON []byte) (*Service, error) {
	if err := registry.DefineContent[Recipe](reg, ContentKind); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(recipesJSON, &raw); err != nil {
		return nil, err
	}
	s := &Service{db: db, reg: reg, wallet: wallet, gold: gold, inv: inv}
	for id, r := range raw {
		if err := reg.LoadContent(ContentKind, id, r); err != nil {
			return nil, err
		}
		s.order = append(s.order, id)
	}
	sort.Strings(s.order)
	return s, s.seedItemDefinitions(ctx)
}

func (s *Service) recipe(id string) (Recipe, bool) {
	return registry.Content[Recipe](s.reg, ContentKind, id)
}

// seedItemDefinitions inserts each recipe's output item definition if absent, so
// crafted items reference a valid item_definitions row.
func (s *Service) seedItemDefinitions(ctx context.Context) error {
	for _, id := range s.order {
		r, _ := s.recipe(id)
		slot := r.Output.SlotType
		if slot == "" {
			slot = "NONE"
		}
		if _, err := s.db.Exec(ctx, `
			INSERT INTO item_definitions (id, name, description, slot_type, min_level)
			VALUES ($1, $2, $3, $4, 1)
			ON CONFLICT (id) DO NOTHING`,
			r.Output.ItemID, r.Output.Name, "Crafted item", slot); err != nil {
			return err
		}
	}
	return nil
}

// RecipeView is a recipe plus whether the character can currently afford it.
type RecipeView struct {
	Recipe
	Affordable bool `json:"affordable"`
}

// Recipes lists every recipe with an affordability flag for the character.
func (s *Service) Recipes(ctx context.Context, charID int64) ([]RecipeView, error) {
	bal, err := s.wallet.Balances(ctx, charID)
	if err != nil {
		return nil, err
	}
	var out []RecipeView
	for _, id := range s.order {
		r, _ := s.recipe(id)
		affordable := true
		for res, c := range r.Cost {
			if bal[res] < c {
				affordable = false
			}
		}
		out = append(out, RecipeView{Recipe: r, Affordable: affordable})
	}
	return out, nil
}

// CraftResult is the outcome of a successful craft.
type CraftResult struct {
	ItemID   string `json:"item_id"`
	Quantity int32  `json:"quantity"`
}

// Craft consumes a recipe's resource + gold cost and grants its output item.
// Costs are fully checked before anything is debited; gold (atomic) is paid
// first so a shortfall aborts before any resource is spent.
func (s *Service) Craft(ctx context.Context, charID int64, recipeID string) (CraftResult, error) {
	r, ok := s.recipe(recipeID)
	if !ok {
		return CraftResult{}, ErrUnknownRecipe
	}

	bal, err := s.wallet.Balances(ctx, charID)
	if err != nil {
		return CraftResult{}, err
	}
	for res, c := range r.Cost {
		if bal[res] < c {
			return CraftResult{}, ErrInsufficientRes
		}
	}

	if r.GoldCost > 0 {
		okGold, err := s.gold.SpendGold(ctx, charID, r.GoldCost)
		if err != nil {
			return CraftResult{}, err
		}
		if !okGold {
			return CraftResult{}, ErrInsufficientGold
		}
	}
	for res, c := range r.Cost {
		if err := s.wallet.Credit(ctx, charID, res, -c); err != nil {
			return CraftResult{}, err
		}
	}

	qty := r.Output.Quantity
	if qty <= 0 {
		qty = 1
	}
	if err := s.inv.AddItem(ctx, &inventory.InventoryItem{
		CharacterID:      int32(charID),
		ItemDefinitionID: r.Output.ItemID,
		Quantity:         qty,
		Durability:       100,
	}); err != nil {
		return CraftResult{}, err
	}
	return CraftResult{ItemID: r.Output.ItemID, Quantity: qty}, nil
}
