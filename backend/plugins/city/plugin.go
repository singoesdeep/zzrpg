package city

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/platform/database"
	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"
)

//go:embed content/buildings.json
var buildingsJSON []byte

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Plugin struct {
	plugin.Base
	svc *Service
}

func (*Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "City Builder",
		Description: "Standalone example game: idle resource city — no characters, combat, or zzstat",
		Icon:        "fa-city",
		Category:    "Gameplay",
		Endpoints:   []string{"POST /api/v1/city/{owner}/found", "GET /api/v1/city/{owner}", "POST /api/v1/city/{owner}/build/{building}", "POST /api/v1/city/{owner}/collect"},
	}
}

func (*Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "city", Requires: []string{"core"}} }

// Migrations ships this game's own schema under the "city" module.
func (*Plugin) Migrations() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "city", FS: fs.FS(migrationsFS), Dir: "migrations"}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	db := registry.MustResolve[*database.DB](reg, "db")

	svc, err := NewService(db.Store, reg, buildingsJSON)
	if err != nil {
		return err
	}
	p.svc = svc
	if err := registry.ProvideKey(reg, ServiceKey, svc); err != nil {
		return err
	}

	mux := ic.Mux()
	mux.HandleFunc("POST /api/v1/city/{owner}/found", p.foundHandler)
	mux.HandleFunc("GET /api/v1/city/{owner}", p.stateHandler)
	mux.HandleFunc("POST /api/v1/city/{owner}/build/{building}", p.buildHandler)
	mux.HandleFunc("POST /api/v1/city/{owner}/collect", p.collectHandler)
	return nil
}

func (p *Plugin) foundHandler(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	if err := p.svc.Found(r.Context(), owner); err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"owner": owner, "starter_gold": StarterGold})
}

func (p *Plugin) stateHandler(w http.ResponseWriter, r *http.Request) {
	view, err := p.svc.State(r.Context(), r.PathValue("owner"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (p *Plugin) buildHandler(w http.ResponseWriter, r *http.Request) {
	level, err := p.svc.Build(r.Context(), r.PathValue("owner"), r.PathValue("building"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"building": r.PathValue("building"), "level": level})
}

func (p *Plugin) collectHandler(w http.ResponseWriter, r *http.Request) {
	gained, err := p.svc.Collect(r.Context(), r.PathValue("owner"))
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"collected": gained})
}

// writeErr maps domain errors to HTTP statuses.
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoCity):
		httpx.WriteError(w, http.StatusNotFound, "NO_CITY", "city not founded")
	case errors.Is(err, ErrCityExists):
		httpx.WriteError(w, http.StatusConflict, "CITY_EXISTS", "city already founded")
	case errors.Is(err, ErrUnknownBuilding):
		httpx.WriteError(w, http.StatusNotFound, "UNKNOWN_BUILDING", "unknown building")
	case errors.Is(err, ErrCannotAfford):
		httpx.WriteError(w, http.StatusPaymentRequired, "CANNOT_AFFORD", "insufficient resources")
	case errors.Is(err, ErrNothingToCollect):
		httpx.WriteError(w, http.StatusOK, "NOTHING_YET", "nothing to collect yet")
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}
