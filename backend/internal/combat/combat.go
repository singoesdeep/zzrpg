package combat

import (
	"context"
	"errors"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/session"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

// mobDefs is the mob content pack, loaded once from embedded content.
var mobDefs = content.MustLoadMobs()

// KillRewarder handles the side effects of a kill — quest progress, loot roll,
// and applying drops (gold/items) — and returns the rolled loot so the attack
// response can include it. It is a consumer-defined interface so that combat
// need not depend on the quest, loot, or inventory services directly: combat
// owns the mechanism (dealing damage, reserving the kill), while policy (what a
// kill rewards) lives behind this seam. Implemented by internal/killreward.
type KillRewarder interface {
	RewardKill(ctx context.Context, killerID, victimID int64) []loot.DroppedItem
}

// CharacterReader is the minimal character-service surface combat needs: fetching
// a combatant's level and stats by ID. Declared here (consumer-owned) so combat
// depends on the behaviour it uses, not the full character.CharacterService.
type CharacterReader interface {
	GetByID(ctx context.Context, id int64) (*character.CharacterWithStats, error)
}

var (
	ErrAttackerNotFound = errors.New("attacker not found or session inactive")
	ErrDefenderNotFound = errors.New("defender not found or session inactive")
	ErrAttackerDead     = errors.New("attacker is dead")
	ErrDefenderDead     = errors.New("defender is already dead")
)

type AttackRequest struct {
	AttackerID      int64   `json:"attacker_id"`
	DefenderID      int64   `json:"defender_id"`
	SkillID         string  `json:"skill_id,omitempty"`
	SkillMultiplier float64 `json:"skill_multiplier,omitempty"`
	SkillFlatDamage float64 `json:"skill_flat_damage,omitempty"`
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
	charService CharacterReader
	statClient  statclient.Client
	registry    *session.Registry
	rewarder    KillRewarder
	eventBus    bus.EventBus
}

// NewCombatService builds the combat service. eventBus may be nil, in which case
// no domain events are published (the service is otherwise unchanged).
func NewCombatService(
	charService CharacterReader,
	statClient statclient.Client,
	registry *session.Registry,
	rewarder KillRewarder,
	eventBus bus.EventBus,
) CombatService {
	return &combatService{
		charService: charService,
		statClient:  statClient,
		registry:    registry,
		rewarder:    rewarder,
		eventBus:    eventBus,
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
	// 1. Resolve attacker session or details
	attackerSess, exists := s.registry.GetSession(req.AttackerID)
	var attackerLevel int32
	var attackerAtk, attackerDex, attackerCritRate, attackerCritDmg float64

	if exists {
		if attackerSess.IsDead {
			return nil, ErrAttackerDead
		}
	}

	attackerChar, err := s.charService.GetByID(ctx, req.AttackerID)
	if err != nil {
		return nil, ErrAttackerNotFound
	}
	attackerLevel = attackerChar.Level
	attackerAtk = attackerChar.Stats.DerivedStats["ATTACK"]
	attackerDex = attackerChar.Stats.BaseStats["DEX"]
	attackerCritRate = attackerChar.Stats.DerivedStats["CRIT_RATE"]
	// default crit damage bonus is 0 for base
	attackerCritDmg = 0.0

	// 2. Resolve defender session and details
	var defenderLevel int32
	var defenderDef, defenderDex float64
	var defenderHP, defenderMaxHP float64
	var defenderIsDead bool
	var defenderIsMob bool
	var defenderLootTable string

	// If the defender is a defined mob (e.g. the training dummy 9999), use its
	// data-driven stats; otherwise treat it as a PvP target (a real character).
	if mob, ok := mobDefs.Mobs[strconv.FormatInt(req.DefenderID, 10)]; ok {
		defenderIsMob = true
		defenderLootTable = mob.LootTableID
		defenderLevel = mob.Level
		defenderDef = mob.Defense
		defenderDex = mob.Dex
		defenderMaxHP = mob.MaxHP

		// Find or create the mob session in registry
		mobSess, mobExists := s.registry.GetSession(req.DefenderID)
		if !mobExists {
			mobSess = s.registry.StartSession(req.DefenderID, mob.MaxHP, mob.MaxMP)
		} else if mobSess.IsDead {
			// Auto revive dummy for testing convenience (mutate through the
			// registry so the change is applied under its lock, then re-read).
			s.registry.Revive(req.DefenderID)
			mobSess, _ = s.registry.GetSession(req.DefenderID)
		}
		defenderHP = mobSess.CurrentHP
		defenderIsDead = mobSess.IsDead
	} else {
		// PvP Target
		defSess, defExists := s.registry.GetSession(req.DefenderID)
		if !defExists {
			return nil, ErrDefenderNotFound
		}
		if defSess.IsDead {
			return nil, ErrDefenderDead
		}

		defenderChar, err := s.charService.GetByID(ctx, req.DefenderID)
		if err != nil {
			return nil, ErrDefenderNotFound
		}
		defenderLevel = defenderChar.Level
		defenderDef = defenderChar.Stats.DerivedStats["DEFENSE"]
		defenderDex = defenderChar.Stats.BaseStats["DEX"]
		defenderHP = defSess.CurrentHP
		defenderMaxHP = defSess.MaxHP
		defenderIsDead = defSess.IsDead
	}

	// 3. Call Rust zzstat Core via embedded statclient
	calcReq := statclient.CalculateDamageReq{
		Attacker: statclient.CombatStats{
			Level:           attackerLevel,
			Attack:          attackerAtk,
			Defense:         0,
			Dex:             attackerDex,
			CritRate:        attackerCritRate,
			CritDamageBonus: attackerCritDmg,
		},
		Defender: statclient.CombatStats{
			Level:   defenderLevel,
			Defense: defenderDef,
			Dex:     defenderDex,
		},
		SkillMultiplier: req.SkillMultiplier,
		SkillFlatDamage: req.SkillFlatDamage,
	}

	res, err := s.statClient.CalculateDamage(ctx, calcReq)
	if err != nil {
		// Fallback physical formula if embedded client failed
		res = statclient.DamageResult{
			IsHit:  true,
			Damage: int32(attackerAtk - defenderDef),
			IsCrit: false,
		}
		if res.Damage < 1 {
			res.Damage = 1
		}
	}

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
		if defenderIsMob {
			s.publish(ctx, MobKilled{
				KillerID:    req.AttackerID,
				VictimID:    req.DefenderID,
				LootTableID: defenderLootTable,
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
