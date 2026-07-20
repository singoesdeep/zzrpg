package combat

import (
	"context"
	"errors"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/game/creature"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
	"github.com/singoesdeep/zzrpg/backend/platform/session"
	"github.com/singoesdeep/zzrpg/backend/platform/statclient"
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
	// 0. Veto attack via pre-attack hooks.
	if err := hooks.DoAction(s.hooks, ctx, HookPreAttack, PreAttack{
		AttackerID: req.AttackerID,
		DefenderID: req.DefenderID,
		SkillID:    req.SkillID,
	}); err != nil {
		return nil, err
	}

	// 1. Resolve attacker & skill.
	attacker, skillMult, skillFlat, err := s.resolveAttackerAndSkill(ctx, req)
	if err != nil {
		return nil, err
	}

	// 2. Resolve defender & session state.
	defender, defenderHP, defenderMaxHP, defenderIsDead, err := s.resolveDefender(ctx, req.DefenderID)
	if err != nil {
		return nil, err
	}

	// 3. Compute damage via zzstat and apply damage filters.
	res, err := s.computeDamage(ctx, req, attacker, defender, skillMult, skillFlat)
	if err != nil {
		return nil, err
	}

	// 4. Deduct HP, grant kill rewards, and publish domain events.
	return s.applyDamageAndEvents(ctx, req, attacker, defender, res, defenderHP, defenderMaxHP, defenderIsDead)
}

func (s *combatService) resolveAttackerAndSkill(ctx context.Context, req AttackRequest) (creature.Creature, float64, float64, error) {
	attacker, ok, err := s.creatures.Resolve(ctx, req.AttackerID)
	if err != nil || !ok {
		return creature.Creature{}, 0, 0, ErrAttackerNotFound
	}
	if sess, exists := s.registry.GetSession(req.AttackerID); exists && sess.IsDead {
		return creature.Creature{}, 0, 0, ErrAttackerDead
	}

	var skillMult, skillFlat float64
	if req.SkillID != "" {
		if s.skills == nil {
			return creature.Creature{}, 0, 0, ErrUnknownSkill
		}
		eff, ok := s.skills.Resolve(req.SkillID)
		if !ok {
			return creature.Creature{}, 0, 0, ErrUnknownSkill
		}
		if eff.ClassReq != "" && attacker.Class != eff.ClassReq {
			return creature.Creature{}, 0, 0, ErrSkillClassMismatch
		}
		if eff.ManaCost > 0 && !s.registry.SpendMP(req.AttackerID, eff.ManaCost) {
			return creature.Creature{}, 0, 0, ErrNotEnoughMana
		}
		skillMult = eff.Multiplier
		skillFlat = eff.FlatDamage
	}
	return attacker, skillMult, skillFlat, nil
}

func (s *combatService) resolveDefender(ctx context.Context, defenderID int64) (creature.Creature, float64, float64, bool, error) {
	defender, ok, err := s.creatures.Resolve(ctx, defenderID)
	if err != nil || !ok {
		return creature.Creature{}, 0, 0, false, ErrDefenderNotFound
	}

	if defender.Kind == creature.KindMob {
		sess, exists := s.registry.GetSession(defenderID)
		if !exists {
			sess = s.registry.StartSession(defenderID, defender.MaxHP, defender.MaxMP)
		} else if sess.IsDead {
			s.registry.Revive(defenderID)
			sess, _ = s.registry.GetSession(defenderID)
		}
		return defender, sess.CurrentHP, sess.MaxHP, sess.IsDead, nil
	}

	sess, exists := s.registry.GetSession(defenderID)
	if !exists {
		return creature.Creature{}, 0, 0, false, ErrDefenderNotFound
	}
	if sess.IsDead {
		return creature.Creature{}, 0, 0, false, ErrDefenderDead
	}
	return defender, sess.CurrentHP, sess.MaxHP, sess.IsDead, nil
}

func (s *combatService) computeDamage(ctx context.Context, req AttackRequest, attacker, defender creature.Creature, skillMult, skillFlat float64) (statclient.DamageResult, error) {
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
		return statclient.DamageResult{}, err
	}

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
	return res, nil
}

func (s *combatService) applyDamageAndEvents(ctx context.Context, req AttackRequest, attacker, defender creature.Creature, res statclient.DamageResult, defenderHP, defenderMaxHP float64, defenderIsDead bool) (*AttackResult, error) {
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

	var rolledLoot []loot.DroppedItem
	if killedNow && s.rewarder != nil {
		rolledLoot = s.rewarder.RewardKill(ctx, req.AttackerID, req.DefenderID)
	}

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
