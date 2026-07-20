package combat

import (
	"context"
	"errors"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/internal/creature"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/session"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

// KillRewarder handles the side effects of a kill — quest progress, loot roll,
// and applying drops (gold/items) — and returns the rolled loot so the attack
// response can include it. It is a consumer-defined interface so that combat
// need not depend on the quest, loot, or inventory services directly: combat
// owns the mechanism (dealing damage, reserving the kill), while policy (what a
// kill rewards) lives behind this seam. Implemented by internal/killreward.
type KillRewarder interface {
	RewardKill(ctx context.Context, killerID, victimID int64) []loot.DroppedItem
}

// SkillEffect is the server-authoritative effect of a skill. combat resolves a
// requested skill to one of these rather than trusting client-supplied numbers.
type SkillEffect struct {
	Multiplier float64
	FlatDamage float64
	ManaCost   float64
	ClassReq   string // "" = any class
}

// SkillResolver looks up a skill's effect by ID. Consumer-owned so combat depends
// only on the resolution behaviour, not the full skills service. Implemented by a
// thin adapter over internal/skills.
type SkillResolver interface {
	Resolve(skillID string) (SkillEffect, bool)
}

var (
	ErrAttackerNotFound   = errors.New("attacker not found or session inactive")
	ErrDefenderNotFound   = errors.New("defender not found or session inactive")
	ErrAttackerDead       = errors.New("attacker is dead")
	ErrDefenderDead       = errors.New("defender is already dead")
	ErrUnknownSkill       = errors.New("unknown skill")
	ErrSkillClassMismatch = errors.New("skill not usable by this class")
	ErrNotEnoughMana      = errors.New("not enough mana")
)

type AttackRequest struct {
	AttackerID int64 `json:"attacker_id"`
	DefenderID int64 `json:"defender_id"`
	// SkillID names the skill to use (empty = a basic attack). The skill's
	// numbers come from the server's skill pack — clients never send damage
	// multipliers.
	SkillID string `json:"skill_id,omitempty"`
}

type AttackResult struct {
	AttackerID     int64              `json:"attacker_id"`
	DefenderID     int64              `json:"defender_id"`
	IsHit          bool               `json:"is_hit"`
	Damage         int32              `json:"damage"`
	IsCrit         bool               `json:"is_crit"`
	DefenderHP     float64            `json:"defender_hp"`
	DefenderMaxHP  float64            `json:"defender_max_hp"`
	DefenderIsDead bool               `json:"defender_is_dead"`
	Loot           []loot.DroppedItem `json:"loot,omitempty"`
}

type CombatService interface {
	ExecuteAttack(ctx context.Context, req AttackRequest) (*AttackResult, error)
}

type combatService struct {
	creatures  creature.Resolver
	statClient statclient.Client
	registry   *session.Registry
	rewarder   KillRewarder
	eventBus   bus.EventBus
	hooks      *hooks.Hooks
	skills     SkillResolver
}

// NewCombatService builds the combat service. The creature resolver produces both
// the attacker and the defender (character, mob, or pet). eventBus, hks and skills
// may be nil (no events published / no hook filters applied / skill attacks
// rejected, respectively).
func NewCombatService(
	creatures creature.Resolver,
	statClient statclient.Client,
	registry *session.Registry,
	rewarder KillRewarder,
	eventBus bus.EventBus,
	hks *hooks.Hooks,
	skills SkillResolver,
) CombatService {
	return &combatService{
		creatures:  creatures,
		statClient: statClient,
		registry:   registry,
		rewarder:   rewarder,
		eventBus:   eventBus,
		hooks:      hks,
		skills:     skills,
	}
}

// publish emits ev on the bus when one is configured. Publishing is async and
// fire-and-forget, so it never affects combat's synchronous outcome.
func (s *combatService) publish(ctx context.Context, ev bus.Event) {
	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, ev)
	}
}

func (s *combatService) ExecuteAttack(ctx context.Context, req AttackRequest) (*AttackResult, error) {
	// 0. Let plugins veto the attack before anything happens (peaceful zones,
	// stuns, disarms). A returned error aborts the attack.
	if err := hooks.DoAction(s.hooks, ctx, HookPreAttack, PreAttack{
		AttackerID: req.AttackerID,
		DefenderID: req.DefenderID,
		SkillID:    req.SkillID,
	}); err != nil {
		return nil, err
	}

	// 1. Resolve the attacker (character/mob/pet) and its combat stats.
	attacker, ok, err := s.creatures.Resolve(ctx, req.AttackerID)
	if err != nil || !ok {
		return nil, ErrAttackerNotFound
	}
	if sess, exists := s.registry.GetSession(req.AttackerID); exists && sess.IsDead {
		return nil, ErrAttackerDead
	}

	// 1b. Resolve the skill server-side (a basic attack when no skill is named).
	// The multiplier/flat/mana come from the server's skill pack, never the
	// client. Class is gated and mana is spent from the attacker's session.
	var skillMult, skillFlat float64
	if req.SkillID != "" {
		if s.skills == nil {
			return nil, ErrUnknownSkill
		}
		eff, ok := s.skills.Resolve(req.SkillID)
		if !ok {
			return nil, ErrUnknownSkill
		}
		if eff.ClassReq != "" && attacker.Class != eff.ClassReq {
			return nil, ErrSkillClassMismatch
		}
		if eff.ManaCost > 0 && !s.registry.SpendMP(req.AttackerID, eff.ManaCost) {
			return nil, ErrNotEnoughMana
		}
		skillMult = eff.Multiplier
		skillFlat = eff.FlatDamage
	}

	// 2. Resolve the defender and its session state. A mob's session is created
	// (and revived) on demand; a character must be in an active, living session.
	defender, ok, err := s.creatures.Resolve(ctx, req.DefenderID)
	if err != nil || !ok {
		return nil, ErrDefenderNotFound
	}

	var defenderHP, defenderMaxHP float64
	var defenderIsDead bool
	if defender.Kind == creature.KindMob {
		sess, exists := s.registry.GetSession(req.DefenderID)
		if !exists {
			sess = s.registry.StartSession(req.DefenderID, defender.MaxHP, defender.MaxMP)
		} else if sess.IsDead {
			// Auto revive (testing convenience); mutate through the registry so
			// the change is applied under its lock, then re-read.
			s.registry.Revive(req.DefenderID)
			sess, _ = s.registry.GetSession(req.DefenderID)
		}
		defenderHP, defenderMaxHP, defenderIsDead = sess.CurrentHP, sess.MaxHP, sess.IsDead
	} else {
		sess, exists := s.registry.GetSession(req.DefenderID)
		if !exists {
			return nil, ErrDefenderNotFound
		}
		if sess.IsDead {
			return nil, ErrDefenderDead
		}
		defenderHP, defenderMaxHP, defenderIsDead = sess.CurrentHP, sess.MaxHP, sess.IsDead
	}

	// 3. Call Rust zzstat Core via embedded statclient
	calcReq := statclient.CalculateDamageReq{
		Attacker: statclient.CombatStats{
			Level:           attacker.Level,
			Attack:          attacker.Attack,
			Defense:         0,
			Dex:             attacker.Dex,
			CritRate:        attacker.CritRate,
			CritDamageBonus: attacker.CritDmg,
		},
		Defender: statclient.CombatStats{
			Level:   defender.Level,
			Defense: defender.Defense,
			Dex:     defender.Dex,
		},
		SkillMultiplier: skillMult,
		SkillFlatDamage: skillFlat,
	}

	res, err := s.statClient.CalculateDamage(ctx, calcReq)
	if err != nil {
		// Fallback physical formula if embedded client failed
		res = statclient.DamageResult{
			IsHit:  true,
			Damage: int32(attacker.Attack - defender.Defense),
			IsCrit: false,
		}
		if res.Damage < 1 {
			res.Damage = 1
		}
	}

	// 3b. Let plugins filter the final damage before it lands (shields, difficulty
	// modifiers, damage-boost events, ...). Clamp to a non-negative value so a
	// filter can zero damage but never turn it into healing.
	filtered := hooks.ApplyFilters(s.hooks, ctx, HookDamage, DamageFilter{
		AttackerID: req.AttackerID,
		DefenderID: req.DefenderID,
		IsCrit:     res.IsCrit,
		Damage:     res.Damage,
	})
	if filtered.Damage < 0 {
		filtered.Damage = 0
	}
	res.Damage = filtered.Damage

	// 4. If hit, deduct defender HP atomically and learn whether this attack
	// landed the kill (killedNow), so death rewards are credited exactly once.
	var killedNow bool
	if res.IsHit {
		finalHP, finalIsDead, killed := s.registry.DeductHPAndReserveKill(req.DefenderID, float64(res.Damage))
		defenderHP = finalHP
		defenderIsDead = finalIsDead
		killedNow = killed
		s.publish(ctx, CharacterDamaged{
			CharacterID: req.DefenderID,
			Amount:      res.Damage,
			NewHP:       defenderHP,
			IsDead:      defenderIsDead,
		})
	}

	// 5. Trigger death progression (loot, quest progress) only for the attacker
	// that actually killed the defender — prevents double loot/quest rewards when
	// concurrent attackers finish the same target. The orchestration lives behind
	// KillRewarder so combat stays decoupled from the quest/loot/inventory
	// services; it runs synchronously so the rolled loot is part of the response.
	var rolledLoot []loot.DroppedItem
	if killedNow && s.rewarder != nil {
		rolledLoot = s.rewarder.RewardKill(ctx, req.AttackerID, req.DefenderID)
	}

	// 6. Emit domain events for consumers (analytics, achievements, aggro/AI,
	// death penalties, client fan-out). These are additive and async — they do
	// not alter the synchronous reward path above.
	s.publish(ctx, CombatAttackResolved{
		AttackerID:     req.AttackerID,
		DefenderID:     req.DefenderID,
		IsHit:          res.IsHit,
		Damage:         res.Damage,
		IsCrit:         res.IsCrit,
		DefenderHP:     defenderHP,
		DefenderMaxHP:  defenderMaxHP,
		DefenderIsDead: defenderIsDead,
	})
	if killedNow {
		if defender.Kind == creature.KindMob {
			s.publish(ctx, MobKilled{
				KillerID:    req.AttackerID,
				VictimID:    req.DefenderID,
				LootTableID: defender.LootTableID,
			})
		} else {
			s.publish(ctx, PlayerKilled{KillerID: req.AttackerID, VictimID: req.DefenderID})
		}
	}

	return &AttackResult{
		AttackerID:     req.AttackerID,
		DefenderID:     req.DefenderID,
		IsHit:          res.IsHit,
		Damage:         res.Damage,
		IsCrit:         res.IsCrit,
		DefenderHP:     defenderHP,
		DefenderMaxHP:  defenderMaxHP,
		DefenderIsDead: defenderIsDead,
		Loot:           rolledLoot,
	}, nil
}
