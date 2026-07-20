// Package xpboost is a complete, self-contained example zzrpg plugin. It shows
// every extension mechanism a third-party plugin can use — WITHOUT modifying the
// engine core:
//
//   - a HOOK FILTER (character.rewards) to double gold rewards;
//   - a HOOK ACTION/VETO (combat.pre_attack) to block attacks on a "protected"
//     target (a toy peaceful-zone rule);
//   - an EVENT SUBSCRIPTION (combat.MobKilled) to react to kills;
//   - an HTTP ROUTE for a small status endpoint;
//   - the plugin LIFECYCLE (Meta/Init, with no-op Start/Stop via plugin.Base).
//
// To enable it, register it with the kernel alongside the built-in plugins:
//
//	k.Register(&xpboost.Plugin{ProtectedID: 1})
//
// See docs/PLUGIN_GUIDE.md for the full walkthrough.
package xpboost

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
)

// Plugin doubles gold rewards, protects one character from attacks, and logs
// kills. ProtectedID (0 = none) is the character that cannot be attacked.
type Plugin struct {
	plugin.Base // no-op Start/Stop; we only need Init
	ProtectedID int64
}

// Meta declares the plugin's identity and that it must initialise after the core
// and character plugins (whose hook/event surface it uses).
func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "xpboost", Requires: []string{"core", "character"}}
}

// Init wires the plugin's extensions. It runs after every Requires plugin, so the
// hooks, bus, and routes it touches are ready.
func (p *Plugin) Init(ic plugin.InitContext) error {
	log := ic.Logger()

	// FILTER: double gold on every reward grant (kills, quests, idle gains all
	// funnel through character.AddRewards).
	hooks.AddFilter(ic.Hooks(), character.HookRewards, 10,
		func(_ context.Context, r character.RewardsFilter) character.RewardsFilter {
			r.Gold *= 2
			return r
		})

	// ACTION / VETO: forbid attacks against the protected character.
	hooks.AddAction(ic.Hooks(), combat.HookPreAttack, 10,
		func(_ context.Context, a combat.PreAttack) error {
			if p.ProtectedID != 0 && a.DefenderID == p.ProtectedID {
				return errors.New("xpboost: this target is protected")
			}
			return nil
		})

	// EVENT: react to kills (fire-and-forget; never blocks combat).
	ic.Bus().Subscribe(combat.EventMobKilled, func(_ context.Context, ev bus.Event) {
		if k, ok := ev.(combat.MobKilled); ok {
			log.Info("xpboost: mob killed", "killer", k.KillerID, "victim", k.VictimID, "table", k.LootTableID)
		}
	})

	// ROUTE: a tiny status endpoint the plugin owns.
	ic.Mux().HandleFunc("GET /api/v1/example/xpboost", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"plugin":"xpboost","gold_multiplier":2,"protected_id":%d}`, p.ProtectedID)
	})

	log.Info("xpboost example plugin initialised", "protected_id", p.ProtectedID)
	return nil
}
