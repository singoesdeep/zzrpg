// Package kit is the batteries-included assembly of the gamekit framework: one
// call wires the entity repo, the built-in component stores (stats, progression,
// inventory), their services, the world, a composer pre-registered with the
// built-in component initializers, and the system scheduler. A game gets the
// whole core in one line and then adds its own components, systems, and hooks.
package kit

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
	"github.com/singoesdeep/zzrpg/gamekit/inventory"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/gamekit/stats"
	"github.com/singoesdeep/zzrpg/gamekit/system"
	"github.com/singoesdeep/zzrpg/gamekit/template"
	"github.com/singoesdeep/zzrpg/gamekit/world"

	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// Deps configure the kit.
type Deps struct {
	Store    store.Store
	Hooks    *hooks.Hooks
	Bus      bus.EventBus
	Formulas map[string]stats.Formulas // stat derivation, per entity kind ("" = default)
	Curve    progression.Curve         // xp curve
}

// Kit is the assembled framework core. The services and the composer/world/
// scheduler are ready to use; the exported stores let a game read/write the
// built-in components directly.
type Kit struct {
	Entities    entity.Repo
	Stats       *stats.Service
	Progression *progression.Service
	Inventory   *inventory.Service
	Economy     *economy.Service
	World       *world.World
	Composer    *template.Composer
	Scheduler   *system.Scheduler

	StatsStore       component.Store[stats.Stats]
	ProgressionStore component.Store[progression.Progression]
	InventoryStore   component.Store[inventory.Inventory]
	WalletStore      component.Store[economy.Wallet]
}

type kinder struct{ repo entity.Repo }

func (k kinder) Kind(ctx context.Context, id int64) (string, error) {
	e, err := k.repo.Get(ctx, id)
	return e.Kind, err
}

// New assembles the framework core over a store. The built-in components use the
// standard tables (see MigrationSource); their initializers are pre-registered
// on the composer so a template can declare "stats"/"progression"/"inventory".
func New(d Deps) *Kit {
	entities := entity.NewPgRepo(d.Store)
	statsStore := component.NewJSONStore[stats.Stats](d.Store, "stats", "entity_stats")
	progStore := component.NewJSONStore[progression.Progression](d.Store, "progression", "entity_progression")
	invStore := component.NewJSONStore[inventory.Inventory](d.Store, "inventory", "entity_inventory")
	walletStore := component.NewJSONStore[economy.Wallet](d.Store, "wallet", "entity_wallet")

	statsSvc := stats.NewService(statsStore, stats.NewFormulaResolver(d.Formulas), kinder{entities}, d.Hooks)
	progSvc := progression.NewService(progStore, d.Curve, d.Hooks)
	invSvc := inventory.NewService(invStore, d.Hooks)
	econSvc := economy.NewService(walletStore, d.Hooks)

	w := world.New(entities)
	w.Register(statsStore)
	w.Register(progStore)
	w.Register(invStore)
	w.Register(walletStore)

	composer := template.NewComposer(entities)
	composer.RegisterComponent("stats", func(ctx context.Context, id int64, raw json.RawMessage) error {
		var base map[string]float64
		if err := json.Unmarshal(raw, &base); err != nil {
			return err
		}
		_, err := statsSvc.SetBase(ctx, id, base)
		return err
	})
	composer.RegisterComponent("progression", template.Init(progStore))
	composer.RegisterComponent("inventory", template.Init(invStore))
	composer.RegisterComponent("wallet", template.Init(walletStore))

	sch := system.NewScheduler(w, d.Bus, system.NewPgLastRun(d.Store, "entity_system_runs"))

	return &Kit{
		Entities: entities, Stats: statsSvc, Progression: progSvc, Inventory: invSvc, Economy: econSvc,
		World: w, Composer: composer, Scheduler: sch,
		StatsStore: statsStore, ProgressionStore: progStore, InventoryStore: invStore, WalletStore: walletStore,
	}
}

//go:embed schema/*.sql
var schemaFS embed.FS

// MigrationSource ships the standard schema (entities + built-in component
// tables + system runs) under the "gamekit" module. A game registers it via a
// plugin.Migrator; games that add their own components ship those tables too.
func MigrationSource() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "gamekit", FS: fs.FS(schemaFS), Dir: "schema"}
}
