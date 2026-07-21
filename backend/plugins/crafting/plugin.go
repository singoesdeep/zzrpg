package crafting

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
)

//go:embed content/recipes.json
var recipesJSON []byte

type Plugin struct {
	plugin.Base
	svc   *Service
	chars character.CharacterService
}

func (*Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Crafting",
		Description: "Resource sink: turn idle-generated wood/stone/metal + gold into inventory items",
		Icon:        "fa-hammer",
		Category:    "Economy",
		Endpoints:   []string{"GET /api/v1/characters/{id}/crafting/recipes", "POST /api/v1/characters/{id}/crafting/craft"},
	}
}

func (*Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "crafting", Requires: []string{"core", "character", "inventory", "idle"}}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	db := registry.MustResolve[*database.DB](reg, "db")
	p.chars = registry.MustResolve[character.CharacterService](reg, "character")
	inv := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	wallet := registry.MustResolve[Wallet](reg, "resourceWallet") // provided by the idle plugin

	svc, err := NewService(ic.Context(), db.Store, reg, wallet, p.chars, inv, recipesJSON)
	if err != nil {
		return err
	}
	p.svc = svc
	if err := registry.ProvideKey(reg, ServiceKey, svc); err != nil {
		return err
	}

	jwt := ic.Config().JWTSecret
	mux := ic.Mux()
	mux.Handle("GET /api/v1/characters/{id}/crafting/recipes", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.recipesHandler)))
	mux.Handle("POST /api/v1/characters/{id}/crafting/craft", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.craftHandler)))
	return nil
}

// owned resolves the {id} character and checks it belongs to the caller.
func (p *Plugin) owned(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID := auth.UserIDFromContext(r.Context())
	charID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || charID <= 0 || userID == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_ID", "invalid character id")
		return 0, false
	}
	char, err := p.chars.GetByID(r.Context(), charID)
	if err != nil || char.UserID != userID {
		httpx.WriteError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", "character not found")
		return 0, false
	}
	return charID, true
}

func (p *Plugin) recipesHandler(w http.ResponseWriter, r *http.Request) {
	charID, ok := p.owned(w, r)
	if !ok {
		return
	}
	views, err := p.svc.Recipes(r.Context(), charID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to list recipes")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, views)
}

func (p *Plugin) craftHandler(w http.ResponseWriter, r *http.Request) {
	charID, ok := p.owned(w, r)
	if !ok {
		return
	}
	var body struct {
		RecipeID string `json:"recipe_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	res, err := p.svc.Craft(r.Context(), charID, body.RecipeID)
	switch {
	case errors.Is(err, ErrUnknownRecipe):
		httpx.WriteError(w, http.StatusNotFound, "UNKNOWN_RECIPE", "unknown recipe")
	case errors.Is(err, ErrInsufficientRes):
		httpx.WriteError(w, http.StatusPaymentRequired, "INSUFFICIENT_RESOURCES", "not enough resources")
	case errors.Is(err, ErrInsufficientGold):
		httpx.WriteError(w, http.StatusPaymentRequired, "INSUFFICIENT_GOLD", "not enough gold")
	case err != nil:
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "craft failed")
	default:
		httpx.WriteJSON(w, http.StatusOK, res)
	}
}
