// Package idlekit is the PILOT port of the idle subsystem onto the gamekit
// framework (see docs/MIGRATION_TEMPLATE.md). It proves the migration pattern on
// the real server without touching the live game: it listens to the real
// character login event, mirrors the character as a gamekit entity, and runs a
// genuine gamekit TickSystem + Scheduler + economy wallet to accrue idle gold —
// offline (catch-up on login) and online (ticks). It credits a SEPARATE gamekit
// wallet (never the live character), so it runs safely alongside the existing
// idle plugin for behaviour comparison.
package idlekit

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
	"github.com/singoesdeep/zzrpg/gamekit/kit"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/gamekit/world"

	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"

	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// entityKind marks the gamekit entity that mirrors a character for idlekit.
const entityKind = "idlekit"

// Producer is idlekit's component: how fast a mirrored character accrues,
// derived from the live character's power and level (the read-bridge).
type Producer struct {
	RatePerMin float64 `json:"rate_per_min"`
	Resource   string  `json:"resource"`
}

// idleSystem is the gamekit TickSystem: it accrues each producer's resource over
// elapsed time into the economy wallet — the same shape gamedemo proved, now
// driven by real character data.
type idleSystem struct {
	prod     component.Store[Producer]
	econ     *economy.Service
	interval time.Duration
}

func (idleSystem) Name() string              { return "idlekit" }
func (s idleSystem) Interval() time.Duration { return s.interval }
func (idleSystem) Query() []string           { return []string{"idlekit_producer"} }

func (s idleSystem) Tick(ctx context.Context, id int64, _ *world.World, elapsed time.Duration) error {
	p, ok, err := s.prod.Get(ctx, id)
	if err != nil || !ok {
		return err
	}
	gain := int64(p.RatePerMin * elapsed.Minutes())
	if gain <= 0 {
		return nil
	}
	_, err = s.econ.Earn(ctx, id, p.Resource, gain)
	return err
}

type Plugin struct {
	plugin.Base
	chars  character.CharacterService
	kit    *kit.Kit
	prod   component.Store[Producer]
	sys    idleSystem
	entkey sync.Mutex // guards mirror-entity create-or-get
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

	// Assemble the gamekit core (stats/progression are unused here; the pilot
	// exercises entity + economy + system).
	p.kit = kit.New(kit.Deps{
		Store: db.Store, Hooks: ic.Hooks(), Bus: ic.Bus(),
		Curve: progression.Curve{Base: 50, Exp: 2},
	})
	p.prod = component.NewJSONStore[Producer](db.Store, "idlekit_producer", "idlekit_producer")
	p.kit.World.Register(p.prod)

	interval := ic.Config().IdleTickInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	p.sys = idleSystem{prod: p.prod, econ: p.kit.Economy, interval: interval}
	p.kit.Scheduler.AddTick(p.sys)

	ic.Mux().HandleFunc("GET /api/v1/idlekit/state/{charID}", p.state)
	return nil
}

func (p *Plugin) Start(rc plugin.RunContext) error {
	p.kit.Scheduler.Run(rc.Context()) // online ticks across all mirrored entities

	// On login: mirror the character, refresh its producer rate from live stats,
	// then settle offline gains via the gamekit catch-up.
	rc.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(character.CharacterLoggedIn)
		if !ok {
			return
		}
		eid, err := p.mirror(ctx, e.CharacterID)
		if err != nil {
			return
		}
		p.kit.Scheduler.Catchup(ctx, p.sys, eid)
	})
	return nil
}

// mirror is the bridge: it maps a character to its gamekit entity (creating it
// once), then sets the producer rate from the character's live power and level.
func (p *Plugin) mirror(ctx context.Context, charID int64) (int64, error) {
	eid, err := p.ensureEntity(ctx, charID)
	if err != nil {
		return 0, err
	}
	char, err := p.chars.GetByID(ctx, charID)
	if err != nil {
		return 0, err
	}
	rate := power(char.Stats.DerivedStats)*0.1 + float64(char.Level)
	if err := p.prod.Set(ctx, eid, Producer{RatePerMin: rate, Resource: "gold"}); err != nil {
		return 0, err
	}
	return eid, nil
}

// ensureEntity returns the gamekit entity mirroring a character, creating it on
// first sight. Serialised so two concurrent logins don't double-create.
func (p *Plugin) ensureEntity(ctx context.Context, charID int64) (int64, error) {
	p.entkey.Lock()
	defer p.entkey.Unlock()
	owned, err := p.kit.Entities.ListByOwner(ctx, charID)
	if err != nil {
		return 0, err
	}
	for _, e := range owned {
		if e.Kind == entityKind {
			return e.ID, nil
		}
	}
	e, err := p.kit.Entities.Create(ctx, entityKind, charID)
	if err != nil {
		return 0, err
	}
	return e.ID, nil
}

// entityFor looks up a character's mirror entity without creating one.
func (p *Plugin) entityFor(ctx context.Context, charID int64) (int64, bool, error) {
	owned, err := p.kit.Entities.ListByOwner(ctx, charID)
	if err != nil {
		return 0, false, err
	}
	for _, e := range owned {
		if e.Kind == entityKind {
			return e.ID, true, nil
		}
	}
	return 0, false, nil
}

// state reports the gamekit-accrued wallet for a character's mirror entity.
func (p *Plugin) state(w http.ResponseWriter, r *http.Request) {
	charID, _ := strconv.ParseInt(r.PathValue("charID"), 10, 64)
	eid, ok, err := p.entityFor(r.Context(), charID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "IDLEKIT", err.Error())
		return
	}
	if !ok {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"character_id": charID, "mirrored": false})
		return
	}
	wallet, _ := p.kit.Economy.Get(r.Context(), eid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"character_id": charID, "mirrored": true, "entity_id": eid, "wallet": wallet,
	})
}

// power is the read-bridge's rate input: the sum of a character's derived stats
// (the old idle used a weighted sum; the pilot uses weight 1 for every stat).
func power(derived map[string]float64) float64 {
	var total float64
	for _, v := range derived {
		total += v
	}
	return total
}
