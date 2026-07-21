package idle

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"sync"
	"time"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/idle"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
	"github.com/singoesdeep/zzrpg/backend/platform/socket"
	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations ships the idle domain's own schema, applied by the persistence
// plugin under the "idle" module — this plugin owns its tables without touching
// platform/database.
func (*Plugin) Migrations() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "idle", FS: fs.FS(migrationsFS), Dir: "migrations"}
}

type Plugin struct {
	plugin.Base
	svc          *idle.Service
	chars        character.CharacterService
	hub          *socket.Hub
	tickInterval time.Duration

	// online is the set of connected characters accruing real-time progress.
	mu     sync.Mutex
	online map[int64]struct{}
}

func (*Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Idle Progression",
		Description: "Content-driven idle activities: combat stages, gathering lifeskills, and RTS resource generators, with offline + real-time online accrual",
		Icon:        "fa-moon",
		Category:    "Economy",
		Endpoints: []string{
			"GET /api/v1/characters/{id}/idle/state",
			"POST /api/v1/characters/{id}/idle/assign",
			"EVENT: CharacterLoggedIn -> OFFLINE_GAINS",
			"TICK: IDLE_TICK",
		},
	}
}

func (*Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "idle", Requires: []string{"core", "character", "inventory", "loot"}}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	p.chars = registry.MustResolve[character.CharacterService](reg, "character")
	lootSvc := registry.MustResolve[loot.LootService](reg, "loot")
	invSvc := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	p.hub = registry.MustResolve[*socket.Hub](reg, "hub")
	db := registry.MustResolve[*database.DB](reg, "db")
	p.tickInterval = ic.Config().IdleTickInterval
	p.online = make(map[int64]struct{})

	wallet := idle.NewWalletRepo(db.Store)
	p.svc = idle.NewService(idle.Deps{
		Chars:       p.chars,
		Loot:        lootSvc,
		Inv:         invSvc,
		Assignments: idle.NewAssignmentRepo(db.Store),
		Lifeskills:  idle.NewLifeskillRepo(db.Store),
		Buildings:   idle.NewBuildingRepo(db.Store),
		Wallet:      wallet,
	})
	if err := registry.Provide(reg, "idle", p.svc); err != nil {
		return err
	}
	// Expose the resource wallet so other plugins (e.g. crafting) can spend
	// idle-generated resources.
	if err := registry.Provide(reg, "resourceWallet", wallet); err != nil {
		return err
	}

	jwt := ic.Config().JWTSecret
	mux := ic.Mux()
	mux.Handle("GET /api/v1/characters/{id}/idle/state", auth.AuthMiddleware(jwt)(idle.StateHandler(p.svc, p.chars)))
	mux.Handle("GET /api/v1/characters/{id}/idle/activities", auth.AuthMiddleware(jwt)(idle.ActivitiesHandler(p.svc, p.chars)))
	mux.Handle("POST /api/v1/characters/{id}/idle/assign", auth.AuthMiddleware(jwt)(idle.AssignHandler(p.svc, p.chars)))
	mux.Handle("POST /api/v1/characters/{id}/idle/buildings/{gen}/upgrade", auth.AuthMiddleware(jwt)(idle.UpgradeBuildingHandler(p.svc, p.chars)))
	return nil
}

func (p *Plugin) Start(rc plugin.RunContext) error {
	// On login: grant the offline window, then start accruing in real time.
	// Activation gating is handled by the plugin-scoped bus, so these handlers
	// stop firing while the idle plugin is deactivated.
	rc.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(character.CharacterLoggedIn)
		if !ok {
			return
		}
		char, err := p.chars.GetByID(ctx, e.CharacterID)
		if err != nil {
			return
		}
		power := p.svc.Power(char.Stats.DerivedStats)
		grant, granted, err := p.svc.Accrue(ctx, idle.AccrualRequest{
			CharacterID: e.CharacterID,
			Since:       e.LastActiveAt,
			Power:       power,
			Level:       char.Level,
		})
		if err == nil && granted {
			p.pushGrant(e.CharacterID, "OFFLINE_GAINS", grant)
			_ = rc.Bus().Publish(ctx, character.OfflineGainsGranted{
				CharacterID: e.CharacterID, ElapsedSeconds: grant.ElapsedSeconds,
				Gold: grant.Gold, Exp: grant.Exp, LeveledUp: grant.LeveledUp,
				NewLevel: grant.NewLevel, Loot: grant.Loot,
			})
		}
		p.setOnline(e.CharacterID, true)
	})

	rc.Bus().Subscribe(character.EventCharacterLoggedOut, func(_ context.Context, ev bus.Event) {
		if e, ok := ev.(character.CharacterLoggedOut); ok {
			p.setOnline(e.CharacterID, false)
		}
	})

	go p.runTicker(rc.Context())
	return nil
}

// runTicker accrues real-time progress for every online character on each tick.
func (p *Plugin) runTicker(ctx context.Context) {
	if p.tickInterval <= 0 {
		return
	}
	ticker := time.NewTicker(p.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, charID := range p.onlineSnapshot() {
				p.tickCharacter(ctx, charID)
			}
		}
	}
}

// tickCharacter accrues one character's progress since its last accrual point
// (its persisted last_active), pushes the delta, and advances last_active so the
// next tick — and any crash-recovery offline grant — starts from here.
func (p *Plugin) tickCharacter(ctx context.Context, charID int64) {
	char, err := p.chars.GetByID(ctx, charID)
	if err != nil {
		return
	}
	power := p.svc.Power(char.Stats.DerivedStats)
	grant, granted, err := p.svc.Accrue(ctx, idle.AccrualRequest{
		CharacterID: charID,
		Since:       char.LastActiveAt,
		Power:       power,
		Level:       char.Level,
	})
	if err != nil || !granted {
		return // below the min-elapsed gate: accumulate into the next tick
	}
	_ = p.chars.UpdateLastActive(ctx, charID)
	p.pushGrant(charID, "IDLE_TICK", grant)
}

func (p *Plugin) setOnline(charID int64, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if on {
		p.online[charID] = struct{}{}
	} else {
		delete(p.online, charID)
	}
}

func (p *Plugin) onlineSnapshot() []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]int64, 0, len(p.online))
	for id := range p.online {
		ids = append(ids, id)
	}
	return ids
}

// pushGrant sends a grant summary to the character's connected client, if any.
func (p *Plugin) pushGrant(charID int64, msgType string, grant idle.Grant) {
	msg, _ := json.Marshal(map[string]interface{}{
		"type": msgType,
		"payload": map[string]interface{}{
			"elapsed_seconds":    grant.ElapsedSeconds,
			"gained_gold":        grant.Gold,
			"gained_exp":         grant.Exp,
			"leveled_up":         grant.LeveledUp,
			"new_level":          grant.NewLevel,
			"loot":               grant.Loot,
			"resources":          grant.Resources,
			"lifeskill_levelups": grant.LifeskillLevelUps,
		},
	})
	if client, exists := p.hub.GetClientByCharacterID(charID); exists {
		client.Send <- msg
	}
}
