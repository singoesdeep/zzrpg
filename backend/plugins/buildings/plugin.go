package buildings

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strconv"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
	gidle "github.com/singoesdeep/zzrpg/gamekit/idle"

	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
	"github.com/singoesdeep/zzrpg/backend/plugins/idlekit"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Plugin struct {
	plugin.Base
	catalog  Catalog
	entities entity.Repo
	levels   component.Store[Levels]
	chars    character.CharacterService
}

func (*Plugin) Meta() plugin.Meta {
	// Requires idlekit so it inits after — idleActivities/hooks must exist
	// before this plugin registers producers and the state filter.
	return plugin.Meta{Name: "buildings", Requires: []string{"core", "character", "idlekit"}}
}

func (*Plugin) Migrations() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "buildings", FS: fs.FS(migrationsFS), Dir: "migrations"}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	db := registry.MustResolve[*database.DB](reg, "db")
	p.chars = registry.MustResolve[character.CharacterService](reg, "character")
	activities := registry.MustResolve[*eidle.Registry](reg, "idleActivities")

	p.catalog = defaultCatalog()
	p.entities = entity.NewPgRepo(db.Store)
	p.levels = component.NewJSONStore[Levels](db.Store, "buildings", "idlekit_building")

	// The only two touchpoints with idlekit: register this content's Producers
	// on the SHARED registry idlekit exposes, and inject building levels into
	// accrual State through idlekit's HookState filter. idlekit never imports or
	// knows about this package.
	p.catalog.register(activities)
	hooks.AddFilter(ic.Hooks(), gidle.HookState, 10, p.catalog.stateFilter(p.levels))

	jwt := ic.Config().JWTSecret
	mux := ic.Mux()
	mux.Handle("GET /api/v1/characters/{id}/buildings", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.list)))
	mux.Handle("POST /api/v1/characters/{id}/buildings/{building}/upgrade", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.upgrade)))
	return nil
}

// entityFor looks a character's idlekit-mirrored entity up — the same entity
// the idle Engine accrues onto, found via the shared entity foundation
// (idlekit.EntityKind), without idlekit exposing a bespoke lookup.
func (p *Plugin) entityFor(ctx context.Context, charID int64) (int64, bool, error) {
	owned, err := p.entities.ListByOwner(ctx, charID)
	if err != nil {
		return 0, false, err
	}
	for _, e := range owned {
		if e.Kind == idlekit.EntityKind {
			return e.ID, true, nil
		}
	}
	return 0, false, nil
}

// owned resolves the {id} path character and checks it belongs to the caller
// (the authenticated user from the JWT), returning its id on success.
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

func (p *Plugin) list(w http.ResponseWriter, r *http.Request) {
	charID, ok := p.owned(w, r)
	if !ok {
		return
	}
	eid, ok, err := p.entityFor(r.Context(), charID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "BUILDINGS", err.Error())
		return
	}
	if !ok {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"levels": map[string]int32{}})
		return
	}
	lv, _, _ := p.levels.Get(r.Context(), eid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"levels": lv.Levels})
}

// upgrade spends gold (the doubling curve, UpgradeCost) to raise one building by
// a level — proving a plugin can both READ character state (via idlekit's
// entity mirror) and WRITE to it (via the real character wallet) without idlekit
// changes.
func (p *Plugin) upgrade(w http.ResponseWriter, r *http.Request) {
	charID, ok := p.owned(w, r)
	if !ok {
		return
	}
	id := r.PathValue("building")
	b, ok := p.catalog.Buildings[id]
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "BUILDINGS", "unknown building")
		return
	}

	eid, ok, err := p.entityFor(r.Context(), charID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "BUILDINGS", err.Error())
		return
	}
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "BUILDINGS", "character has not logged in yet (no idlekit entity)")
		return
	}

	lv, _, err := p.levels.Get(r.Context(), eid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "BUILDINGS", err.Error())
		return
	}
	if lv.Levels == nil {
		lv.Levels = map[string]int32{}
	}
	cost := b.UpgradeCost(lv.Levels[id])

	spent, err := p.chars.SpendGold(r.Context(), charID, cost)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "BUILDINGS", err.Error())
		return
	}
	if !spent {
		httpx.WriteError(w, http.StatusPaymentRequired, "INSUFFICIENT_GOLD", "not enough gold")
		return
	}

	lv.Levels[id]++
	if err := p.levels.Set(r.Context(), eid, lv); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "BUILDINGS", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"building": id, "level": lv.Levels[id], "gold_spent": cost})
}
