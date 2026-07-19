package combat

import (
	"context"
	"errors"

	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

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
	charService  character.CharacterService
	statClient   statclient.Client
	registry     *socket.SessionRegistry
	questSvc     quests.QuestService
	lootSvc      loot.LootService
	inventorySvc inventory.InventoryService
}

func NewCombatService(
	charService character.CharacterService,
	statClient statclient.Client,
	registry *socket.SessionRegistry,
	questSvc quests.QuestService,
	lootSvc loot.LootService,
	inventorySvc inventory.InventoryService,
) CombatService {
	return &combatService{
		charService:  charService,
		statClient:   statClient,
		registry:     registry,
		questSvc:     questSvc,
		lootSvc:      lootSvc,
		inventorySvc: inventorySvc,
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

	// If defender is a training dummy (special ID e.g. 9999)
	if req.DefenderID == 9999 {
		defenderLevel = 10
		defenderDef = 40.0
		defenderDex = 10.0
		defenderMaxHP = 1000.0

		// Find or create dummy session in registry
		dummySess, dummyExists := s.registry.GetSession(9999)
		if !dummyExists {
			dummySess = s.registry.StartSession(9999, 1000.0, 100.0)
		} else if dummySess.IsDead {
			// Auto revive dummy for testing convenience (mutate through the
			// registry so the change is applied under its lock, then re-read).
			s.registry.Revive(9999)
			dummySess, _ = s.registry.GetSession(9999)
		}
		defenderHP = dummySess.CurrentHP
		defenderIsDead = dummySess.IsDead
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
	}

	// 5. Trigger death progression (loot, quest progress) only for the attacker
	// that actually killed the defender — prevents double loot/quest rewards when
	// concurrent attackers finish the same target.
	var rolledLoot []loot.DroppedItem
	if killedNow {
		var tableID string
		if req.DefenderID == 9999 {
			tableID = "dummy_drops"
			_ = s.questSvc.UpdateQuestProgress(ctx, int32(req.AttackerID), "KILL_MOB", "wolf", 1)
		} else {
			tableID = "player_drops" // or default PvP table
			_ = s.questSvc.UpdateQuestProgress(ctx, int32(req.AttackerID), "KILL_MOB", "player", 1)
		}

		// Roll Loot drops!
		if s.lootSvc != nil {
			drops, err := s.lootSvc.RollLoot(ctx, tableID)
			if err == nil {
				rolledLoot = drops
				// Process drops: add gold or items
				for _, drop := range drops {
					if drop.ItemDefinitionID == "gold" {
						_, _, _ = s.charService.AddRewards(ctx, req.AttackerID, int64(drop.Quantity), 0)
					} else {
						if s.inventorySvc != nil {
							invItem := &inventory.InventoryItem{
								CharacterID:      int32(req.AttackerID),
								ItemDefinitionID: drop.ItemDefinitionID,
								Quantity:         drop.Quantity,
								Durability:       100,
							}
							_ = s.inventorySvc.AddItem(ctx, invItem)
						}
					}
				}
			}
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
