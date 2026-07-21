package gamedemo

import (
	"context"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/stats"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// Combat is a game-side System — an example the framework does not impose. A
// city-builder simply never registers it. It shows the two combat extension
// points (HookDamage filter, HookKill action) and reuses gamekit's stats +
// component seams.

const (
	// HookDamage is a Filter over the computed damage (weapon bonuses, crits, …).
	HookDamage = "combat.damage"
	// HookKill is an Action fired when an attack kills the defender.
	HookKill = "combat.kill"
)

// Health is the combat component.
type Health struct {
	Current float64 `json:"current"`
	Max     float64 `json:"max"`
}

// Damage is the filter payload: who's involved and the amount so far.
type Damage struct {
	AttackerID int64
	DefenderID int64
	Amount     float64
}

// Kill is the action payload on a lethal blow.
type Kill struct {
	AttackerID int64
	VictimID   int64
}

// AttackResult is returned to the caller.
type AttackResult struct {
	Damage     float64 `json:"damage"`
	DefenderHP float64 `json:"defender_hp"`
	Killed     bool    `json:"killed"`
}

// Combat resolves attacks using stats (ATTACK vs DEFENSE) and the health
// component, routed through the damage filter and the kill action.
type Combat struct {
	stats  *stats.Service
	health component.Store[Health]
	hooks  *hooks.Hooks
}

// NewCombat builds the combat system.
func NewCombat(s *stats.Service, health component.Store[Health], h *hooks.Hooks) *Combat {
	return &Combat{stats: s, health: health, hooks: h}
}

// Attack computes and applies damage from attacker to defender.
func (c *Combat) Attack(ctx context.Context, attackerID, defenderID int64) (AttackResult, error) {
	atk, _, err := c.stats.Get(ctx, attackerID)
	if err != nil {
		return AttackResult{}, err
	}
	def, _, err := c.stats.Get(ctx, defenderID)
	if err != nil {
		return AttackResult{}, err
	}

	amount := atk.Derived["ATTACK"] - def.Derived["DEFENSE"]
	if amount < 0 {
		amount = 0
	}
	d := hooks.ApplyFilters(c.hooks, ctx, HookDamage, Damage{AttackerID: attackerID, DefenderID: defenderID, Amount: amount})

	h, _, _ := c.health.Get(ctx, defenderID)
	h.Current -= d.Amount
	killed := h.Current <= 0
	if killed {
		h.Current = 0
	}
	if err := c.health.Set(ctx, defenderID, h); err != nil {
		return AttackResult{}, err
	}
	if killed {
		_ = hooks.DoAction(c.hooks, ctx, HookKill, Kill{AttackerID: attackerID, VictimID: defenderID})
	}
	return AttackResult{Damage: d.Amount, DefenderHP: h.Current, Killed: killed}, nil
}
