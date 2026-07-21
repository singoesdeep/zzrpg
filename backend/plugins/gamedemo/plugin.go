// Package gamedemo wires the gamekit framework into a runnable game to prove it
// end to end: it composes entities from data-driven templates, runs a production
// TickSystem with offline catch-up, levels entities up with a hook that grants a
// trophy, and serves it over HTTP — all with NO combat and NO native stat
// library (gamekit's pure-Go formula resolver).
package gamedemo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
	"github.com/singoesdeep/zzrpg/gamekit/inventory"
	"github.com/singoesdeep/zzrpg/gamekit/kit"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/gamekit/stats"
	"github.com/singoesdeep/zzrpg/gamekit/system"
	"github.com/singoesdeep/zzrpg/gamekit/template"
	"github.com/singoesdeep/zzrpg/gamekit/world"

	"github.com/singoesdeep/zzrpg/backend/platform/database"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed content/templates.json
var templatesJSON []byte

//go:embed content/formulas.json
var formulasJSON []byte

// Resources is a demo wallet component; Production is a demo generator component.
type Resources struct {
	Amounts map[string]int64 `json:"amounts"`
}
type Production struct {
	RatePerMin float64 `json:"rate_per_min"`
	Resource   string  `json:"resource"`
}

// productionSystem accrues each producing entity's resource over elapsed time —
// the generalised idle producer, as a gamekit TickSystem.
type productionSystem struct {
	prod component.Store[Production]
	res  component.Store[Resources]
}

func (productionSystem) Name() string            { return "production" }
func (productionSystem) Interval() time.Duration { return time.Minute }
func (productionSystem) Query() []string         { return []string{"production"} }
func (s productionSystem) Tick(ctx context.Context, id int64, _ *world.World, elapsed time.Duration) error {
	p, ok, err := s.prod.Get(ctx, id)
	if err != nil || !ok {
		return err
	}
	r, _, _ := s.res.Get(ctx, id)
	if r.Amounts == nil {
		r.Amounts = map[string]int64{}
	}
	r.Amounts[p.Resource] += int64(p.RatePerMin * elapsed.Minutes())
	return s.res.Set(ctx, id, r)
}

type Plugin struct {
	plugin.Base
	composer *template.Composer
	stats    *stats.Service
	prog     *progression.Service
	inv      *inventory.Service
	econ     *economy.Service
	res      component.Store[Resources]
	health   component.Store[Health]
	combat   *Combat
	sch      *system.Scheduler
	prodSys  productionSystem
}

func (*Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "gamedemo", Requires: []string{"core"}} }

func (*Plugin) Migrations() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "gamedemo", FS: fs.FS(migrationsFS), Dir: "migrations"}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	db := registry.MustResolve[*database.DB](reg, "db")
	h := ic.Hooks()

	var byKind map[string]stats.Formulas
	if err := json.Unmarshal(formulasJSON, &byKind); err != nil {
		return err
	}
	// One call assembles the framework core: entity repo, stats/progression/
	// inventory components + services, world, composer (with the built-in
	// initializers), and the scheduler.
	k := kit.New(kit.Deps{Store: db.Store, Hooks: h, Bus: ic.Bus(), Formulas: byKind, Curve: progression.Curve{Base: 50, Exp: 2}})
	p.stats, p.prog, p.inv, p.econ = k.Stats, k.Progression, k.Inventory, k.Economy
	p.composer, p.sch = k.Composer, k.Scheduler

	// This game's OWN components on top of the built-ins.
	resStore := component.NewJSONStore[Resources](db.Store, "resources", "entity_resources")
	prodStore := component.NewJSONStore[Production](db.Store, "production", "entity_production")
	healthStore := component.NewJSONStore[Health](db.Store, "health", "entity_health")
	p.res, p.health = resStore, healthStore
	p.combat = NewCombat(k.Stats, healthStore, h)
	for _, idx := range []component.ComponentIndex{resStore, prodStore, healthStore} {
		k.World.Register(idx)
	}
	k.Composer.RegisterComponent("resources", template.Init(resStore))
	k.Composer.RegisterComponent("production", template.Init(prodStore))
	k.Composer.RegisterComponent("health", template.Init(healthStore))

	var tpls map[string]map[string]json.RawMessage
	if err := json.Unmarshal(templatesJSON, &tpls); err != nil {
		return err
	}
	k.Composer.LoadTemplates(tpls)

	// This game's OWN systems: a production TickSystem (combat is the Combat
	// resolver above, driven by the attack endpoint).
	p.prodSys = productionSystem{prod: prodStore, res: resStore}
	p.sch.AddTick(p.prodSys)

	// Extension via hooks — plugins reaching across toolkits/systems that don't
	// know about each other, the core of the framework's extensibility:
	//   level up  → grant a trophy item  (progression → inventory)
	//   kill      → grant xp to the killer (combat → progression → …trophy)
	//   damage    → +5 weapon bonus       (a combat filter)
	hooks.AddAction(h, progression.HookLevelUp, 10, func(ctx context.Context, lu progression.LevelUp) error {
		return p.inv.AddItem(ctx, lu.EntityID, inventory.Item{ItemID: fmt.Sprintf("trophy_lvl_%d", lu.NewLevel), Quantity: 1})
	})
	hooks.AddAction(h, HookKill, 10, func(ctx context.Context, k Kill) error {
		_, _, err := p.prog.GrantXP(ctx, k.AttackerID, 100) // xp per kill (may cascade a level-up + trophy)
		return err
	})
	hooks.AddFilter(h, HookDamage, 10, func(_ context.Context, d Damage) Damage {
		d.Amount += 5 // a "sharp weapon" plugin
		return d
	})

	mux := ic.Mux()
	mux.HandleFunc("POST /api/v1/demo/spawn/{kind}", p.spawn)
	mux.HandleFunc("GET /api/v1/demo/entity/{id}", p.getEntity)
	mux.HandleFunc("POST /api/v1/demo/grant-xp/{id}", p.grantXP)
	mux.HandleFunc("POST /api/v1/demo/collect/{id}", p.collect)
	mux.HandleFunc("POST /api/v1/demo/attack/{attacker}/{defender}", p.attack)
	mux.HandleFunc("POST /api/v1/demo/spend/{id}", p.spend)
	return nil
}

func (p *Plugin) Start(rc plugin.RunContext) error {
	p.sch.Run(rc.Context()) // starts tick + event dispatch (non-blocking)
	return nil
}

func (p *Plugin) spawn(w http.ResponseWriter, r *http.Request) {
	e, err := p.composer.Spawn(r.Context(), r.PathValue("kind"), 0)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "SPAWN", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, e)
}

func (p *Plugin) getEntity(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	st, _, _ := p.stats.Get(r.Context(), id)
	pr, _ := p.prog.Get(r.Context(), id)
	inv, _ := p.inv.Get(r.Context(), id)
	res, _, _ := p.res.Get(r.Context(), id)
	hp, _, _ := p.health.Get(r.Context(), id)
	wallet, _ := p.econ.Get(r.Context(), id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "stats": st, "progression": pr, "inventory": inv, "resources": res, "health": hp, "wallet": wallet,
	})
}

func (p *Plugin) attack(w http.ResponseWriter, r *http.Request) {
	atk, _ := strconv.ParseInt(r.PathValue("attacker"), 10, 64)
	def, _ := strconv.ParseInt(r.PathValue("defender"), 10, 64)
	res, err := p.combat.Attack(r.Context(), atk, def)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ATTACK", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

// spend debits the entity's wallet through the built-in economy toolkit,
// returning 402 when it can't afford the cost.
func (p *Plugin) spend(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	currency := r.URL.Query().Get("currency")
	amount, _ := strconv.ParseInt(r.URL.Query().Get("amount"), 10, 64)
	wallet, err := p.econ.Spend(r.Context(), id, currency, amount)
	if errors.Is(err, economy.ErrInsufficient) {
		httpx.WriteError(w, http.StatusPaymentRequired, "INSUFFICIENT", err.Error())
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SPEND", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"wallet": wallet})
}

func (p *Plugin) grantXP(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	amount, _ := strconv.ParseInt(r.URL.Query().Get("amount"), 10, 64)
	pr, gained, err := p.prog.GrantXP(r.Context(), id, amount)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "XP", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"progression": pr, "levels_gained": gained})
}

func (p *Plugin) collect(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := p.sch.Catchup(r.Context(), p.prodSys, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "COLLECT", err.Error())
		return
	}
	res, _, _ := p.res.Get(r.Context(), id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"resources": res})
}
