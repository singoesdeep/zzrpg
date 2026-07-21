// Package idlekit is the idle subsystem rebuilt on the gamekit idle framework
// (gamekit/idle). It replaces the legacy idle plugin: it owns the idle accrual
// for real characters end to end — offline catch-up on login and online ticks —
// by mirroring each character as a gamekit entity, driving the gamekit idle
// Engine over developer-supplied Activities, and reflecting the results onto the
// live game (gold/exp to the character, resources to a wallet crafting spends).
//
// It ships two example activities and exposes the activity registry, so richer
// content (more stages, lifeskills, buildings) is added by OTHER plugins
// registering their own engine/idle.Producer — not by touching this package.
package idlekit

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"sync"
	"time"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
	gidle "github.com/singoesdeep/zzrpg/gamekit/idle"
	"github.com/singoesdeep/zzrpg/gamekit/kit"
	"github.com/singoesdeep/zzrpg/gamekit/progression"

	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// EntityKind is the gamekit entity Kind that mirrors a character, OwnerID set to
// the character id. Other plugins that extend idle content (buildings,
// lifeskills, …) look a character's entity up the same way idlekit does —
// entity.Repo.ListByOwner(charID) filtered by this Kind — rather than idlekit
// exposing a bespoke lookup per extension.
const EntityKind = "idlekit"

type Plugin struct {
	plugin.Base
	chars  character.CharacterService
	kit    *kit.Kit
	engine *gidle.Engine
	sys    gidle.System
	assign component.Store[gidle.Assignment]
	wallet component.Store[economy.Wallet]
	entkey sync.Mutex
}

func (*Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "idlekit", Requires: []string{"core", "character"}}
}

func (*Plugin) Migrations() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "idlekit", FS: fs.FS(migrationsFS), Dir: "migrations"}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	p.chars = registry.MustResolve[character.CharacterService](reg, "character")
	db := registry.MustResolve[*database.DB](reg, "db")

	p.kit = kit.New(kit.Deps{
		Store: db.Store, Hooks: ic.Hooks(), Bus: ic.Bus(),
		Curve: progression.Curve{Base: 50, Exp: 2},
	})
	p.assign = component.NewJSONStore[gidle.Assignment](db.Store, gidle.AssignmentComponent, "idlekit_assignment")
	p.wallet = p.kit.WalletStore
	p.kit.World.Register(p.assign)

	// The shared activity registry: seeded with the example activities, exposed
	// so other plugins can register more (buildings, lifeskills, …).
	activities := eidle.NewRegistry()
	activities.Register("training", training{})
	activities.Register("gathering", gathering{})
	if err := registry.Provide(reg, "idleActivities", activities); err != nil {
		return err
	}

	p.engine = gidle.NewEngine(gidle.Deps{
		Registry: activities,
		Assign:   p.assign,
		StateFor: p.stateFor, // read-bridge: live character stats → activity inputs
		Apply:    p.apply,    // write-bridge: output → character + resource wallet
		Hooks:    ic.Hooks(),
	})

	interval := ic.Config().IdleTickInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	// minSeconds gates tiny windows; capSeconds bounds an offline session (24h).
	p.sys = gidle.NewSystem(p.engine, interval, 1, 24*3600)
	p.kit.Scheduler.AddTick(p.sys)

	// Resource wallet crafting spends from (satisfied structurally; keyed by
	// character id, debit-capable — unlike the clamped economy.Earn).
	if err := registry.Provide(reg, "resourceWallet", resourceWallet{p: p}); err != nil {
		return err
	}

	jwt := ic.Config().JWTSecret
	mux := ic.Mux()
	mux.Handle("GET /api/v1/characters/{id}/idle/state", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.state)))
	mux.Handle("GET /api/v1/characters/{id}/idle/activities", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.activities)))
	mux.Handle("POST /api/v1/characters/{id}/idle/assign", auth.AuthMiddleware(jwt)(http.HandlerFunc(p.assignHandler)))
	return nil
}

func (p *Plugin) Start(rc plugin.RunContext) error {
	p.kit.Scheduler.Run(rc.Context()) // online ticks across all assigned entities

	// On login: mirror the character and settle offline gains via catch-up.
	rc.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(character.CharacterLoggedIn)
		if !ok {
			return
		}
		if eid, err := p.mirror(ctx, e.CharacterID); err == nil {
			p.kit.Scheduler.Catchup(ctx, p.sys, eid)
		}
	})
	return nil
}

// stateFor is the read-bridge: an entity's activity inputs come from its live
// character (power = sum of derived stats, plus level).
func (p *Plugin) stateFor(ctx context.Context, entityID int64) (eidle.State, error) {
	charID, err := p.charOf(ctx, entityID)
	if err != nil {
		return eidle.State{}, err
	}
	char, err := p.chars.GetByID(ctx, charID)
	if err != nil {
		return eidle.State{}, err
	}
	return eidle.State{Vars: map[string]float64{"power": power(char.Stats.DerivedStats), "level": float64(char.Level)}}, nil
}

// apply is the write-bridge: gold/exp are credited to the real character; every
// other amount is a resource banked in the gamekit wallet (spent by crafting).
func (p *Plugin) apply(ctx context.Context, entityID int64, out eidle.Output) error {
	charID, err := p.charOf(ctx, entityID)
	if err != nil {
		return err
	}
	gold, exp := out.Amounts["gold"], out.Amounts["exp"]
	if gold > 0 || exp > 0 {
		if _, _, err := p.chars.AddRewards(ctx, charID, gold, exp); err != nil {
			return err
		}
	}
	for name, amt := range out.Amounts {
		if name == "gold" || name == "exp" || amt <= 0 {
			continue
		}
		if _, err := p.kit.Economy.Earn(ctx, entityID, name, amt); err != nil {
			return err
		}
	}
	return nil
}

// mirror maps a character to its gamekit entity (create-once).
func (p *Plugin) mirror(ctx context.Context, charID int64) (int64, error) {
	p.entkey.Lock()
	defer p.entkey.Unlock()
	owned, err := p.kit.Entities.ListByOwner(ctx, charID)
	if err != nil {
		return 0, err
	}
	for _, e := range owned {
		if e.Kind == EntityKind {
			return e.ID, nil
		}
	}
	e, err := p.kit.Entities.Create(ctx, EntityKind, charID)
	if err != nil {
		return 0, err
	}
	return e.ID, nil
}

// charOf reverse-maps a mirror entity to its character (stored as OwnerID).
func (p *Plugin) charOf(ctx context.Context, entityID int64) (int64, error) {
	e, err := p.kit.Entities.Get(ctx, entityID)
	if err != nil {
		return 0, err
	}
	return e.OwnerID, nil
}

// --- HTTP (legacy idle contract preserved) ---

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

func (p *Plugin) state(w http.ResponseWriter, r *http.Request) {
	charID, ok := p.owned(w, r)
	if !ok {
		return
	}
	eid, err := p.mirror(r.Context(), charID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "IDLE", err.Error())
		return
	}
	cur, _, _ := p.engine.Current(r.Context(), eid)
	wallet, _, _ := p.wallet.Get(r.Context(), eid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"character_id": charID, "activity": cur, "resources": wallet.Balances,
	})
}

func (p *Plugin) activities(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"activities": p.engine.Activities()})
}

func (p *Plugin) assignHandler(w http.ResponseWriter, r *http.Request) {
	charID, ok := p.owned(w, r)
	if !ok {
		return
	}
	var body struct {
		ActivityID string `json:"activity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "IDLE", "invalid body")
		return
	}
	eid, err := p.mirror(r.Context(), charID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "IDLE", err.Error())
		return
	}
	if err := p.engine.Assign(r.Context(), eid, body.ActivityID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "IDLE", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"character_id": charID, "activity": body.ActivityID})
}

// resourceWallet adapts the gamekit wallet to crafting's Wallet interface,
// keyed by character id and debit-capable (crafting Credits negative amounts to
// spend). It reads/writes the same wallet idle banks resources into.
type resourceWallet struct{ p *Plugin }

func (rw resourceWallet) Balances(ctx context.Context, charID int64) (map[string]int64, error) {
	eid, err := rw.p.mirror(ctx, charID)
	if err != nil {
		return nil, err
	}
	w, _, err := rw.p.wallet.Get(ctx, eid)
	if err != nil {
		return nil, err
	}
	return w.Balances, nil
}

func (rw resourceWallet) Credit(ctx context.Context, charID int64, resourceID string, amount int64) error {
	eid, err := rw.p.mirror(ctx, charID)
	if err != nil {
		return err
	}
	w, _, err := rw.p.wallet.Get(ctx, eid)
	if err != nil {
		return err
	}
	if w.Balances == nil {
		w.Balances = map[string]int64{}
	}
	w.Balances[resourceID] += amount // amount may be negative (a spend)
	if w.Balances[resourceID] < 0 {
		w.Balances[resourceID] = 0
	}
	return rw.p.wallet.Set(ctx, eid, w)
}

// power is the read-bridge's rate input: the sum of a character's derived stats.
func power(derived map[string]float64) float64 {
	var total float64
	for _, v := range derived {
		total += v
	}
	return total
}
