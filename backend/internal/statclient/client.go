package statclient

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"

	zzstat "github.com/singoesdeep/zzstat/bindings/go"
)

type CharacterState struct {
	CharacterID int32
	BaseStats   map[string]float64
	Equipment   []Modifier
	Skills      []Modifier
	ActiveBuffs []Modifier
}

type Modifier struct {
	Stat      string
	Operation string
	Value     float64
	Priority  int32
	SourceID  string
}

type CombatStats struct {
	Level           int32
	Attack          float64
	Defense         float64
	Dex             float64
	CritRate        float64
	CritDamageBonus float64
	AccModifiers    float64
	DodgeModifiers  float64
}

type CalculateDamageReq struct {
	Attacker        CombatStats
	Defender        CombatStats
	SkillMultiplier float64
	SkillFlatDamage float64
}

type DamageResult struct {
	IsHit  bool
	Damage int32
	IsCrit bool
}

type Client interface {
	Calculate(ctx context.Context, state CharacterState) (map[string]float64, error)
	CalculateDamage(ctx context.Context, req CalculateDamageReq) (DamageResult, error)
	Close() error
}

type embeddedStatClient struct {
	rng *rand.Rand
}

var (
	loadOnce sync.Once
	loadErr  error
)

func NewClient(addr string) (Client, error) {
	// Dynamically load the library once
	loadOnce.Do(func() {
		// Attempt fallback search paths
		paths := []string{
			os.Getenv("ZZSTAT_LIB_PATH"),
			"/home/singo/github.com/singoesdeep/zzstat/target/release/libzzstat_ffi.so",
			"./libzzstat_ffi.so",
			"libzzstat_ffi.so",
		}

		var err error
		for _, path := range paths {
			if path == "" {
				continue
			}
			if _, statErr := os.Stat(path); statErr == nil || path == "libzzstat_ffi.so" {
				err = zzstat.LoadLibrary(path)
				if err == nil {
					break
				}
			}
		}
		if err != nil {
			loadErr = fmt.Errorf("failed to load zzstat library: %w", err)
		}
	})

	if loadErr != nil {
		return nil, loadErr
	}

	return &embeddedStatClient{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func (c *embeddedStatClient) Calculate(ctx context.Context, state CharacterState) (map[string]float64, error) {
	resolver := zzstat.NewResolver()
	defer resolver.Free()

	sCtx := zzstat.NewContext()
	defer sCtx.Free()

	// 1. Group modifiers by stat target
	modifiersByStat := make(map[string][]Modifier)

	// Add base stats as modifiers (priority 10, source "base")
	for stat, val := range state.BaseStats {
		modifiersByStat[stat] = append(modifiersByStat[stat], Modifier{
			Stat:      stat,
			Operation: "ADD",
			Value:     val,
			Priority:  10,
			SourceID:  "base_stat",
		})
	}

	for _, m := range state.Equipment {
		modifiersByStat[m.Stat] = append(modifiersByStat[m.Stat], m)
	}
	for _, m := range state.Skills {
		modifiersByStat[m.Stat] = append(modifiersByStat[m.Stat], m)
	}
	for _, m := range state.ActiveBuffs {
		modifiersByStat[m.Stat] = append(modifiersByStat[m.Stat], m)
	}

	// 2. Setup STR, INT, DEX, CON first as constant sources + multiplicative transforms
	for _, prim := range []string{"STR", "INT", "DEX", "CON"} {
		var addSum float64
		var multSum float64
		hasSource := false
		for _, m := range modifiersByStat[prim] {
			if m.Operation == "ADD" {
				addSum += m.Value
				hasSource = true
			} else if m.Operation == "MULTIPLY" {
				multSum += m.Value
			}
		}
		if hasSource {
			resolver.RegisterConstantSource(prim, addSum)
		} else {
			resolver.RegisterConstantSource(prim, 0.0)
		}
		if multSum != 0.0 {
			resolver.RegisterMultiplicativeTransform(prim, zzstat.PhaseMultiplicative, zzstat.RuleMultiplicative, 1.0+multSum)
		}
	}

	// 3. Register base derived stats as scaling transforms or constants
	// HP = CON * 15.0
	resolver.RegisterScalingTransform("HP", zzstat.PhaseAdditive, zzstat.RuleAdditive, "CON", 15.0)
	// MP = INT * 10.0
	resolver.RegisterScalingTransform("MP", zzstat.PhaseAdditive, zzstat.RuleAdditive, "INT", 10.0)
	// ATTACK = STR * 2.0 + DEX * 0.5
	resolver.RegisterScalingTransform("ATTACK", zzstat.PhaseAdditive, zzstat.RuleAdditive, "STR", 2.0)
	resolver.RegisterScalingTransform("ATTACK", zzstat.PhaseAdditive, zzstat.RuleAdditive, "DEX", 0.5)
	// DEFENSE = CON * 1.0 + STR * 0.2
	resolver.RegisterScalingTransform("DEFENSE", zzstat.PhaseAdditive, zzstat.RuleAdditive, "CON", 1.0)
	resolver.RegisterScalingTransform("DEFENSE", zzstat.PhaseAdditive, zzstat.RuleAdditive, "STR", 0.2)
	// CRIT_RATE = 5.0 base
	resolver.RegisterConstantSource("CRIT_RATE", 5.0)

	// 4. Register modifiers for derived stats (HP, MP, ATTACK, DEFENSE, CRIT_RATE)
	for _, derived := range []string{"HP", "MP", "ATTACK", "DEFENSE", "CRIT_RATE"} {
		var addSum float64
		var multSum float64
		for _, m := range modifiersByStat[derived] {
			switch m.Operation {
			case "ADD":
				addSum += m.Value
			case "MULTIPLY":
				multSum += m.Value
			}
		}
		if addSum > 0.0 {
			resolver.RegisterConstantSource(derived, addSum)
		}
		if multSum != 0.0 {
			resolver.RegisterMultiplicativeTransform(derived, zzstat.PhaseMultiplicative, zzstat.RuleMultiplicative, 1.0+multSum)
		}
	}

	// 5. Resolve final values
	results := make(map[string]float64)
	allStats := []string{"STR", "INT", "DEX", "CON", "HP", "MP", "ATTACK", "DEFENSE", "CRIT_RATE"}
	for _, stat := range allStats {
		val, err := resolver.Resolve(stat, sCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve stat %s: %w", stat, err)
		}
		results[stat] = val
	}

	return results, nil
}

func (c *embeddedStatClient) CalculateDamage(ctx context.Context, req CalculateDamageReq) (DamageResult, error) {
	// 1. Calculate accuracy and check hit/miss
	// Dodge Rate = (Defender DEX * 0.2 + Defender Dodge Modifiers) / (Attacker Level * 1.5)
	dodgeRate := (req.Defender.Dex*0.2 + req.Defender.DodgeModifiers) / (float64(req.Attacker.Level) * 1.5)
	baseHitChance := (1.0 - dodgeRate) + req.Attacker.AccModifiers

	// Cap hit chance between 70% (0.70) and 99% (0.99)
	hitChance := baseHitChance
	if hitChance < 0.70 {
		hitChance = 0.70
	} else if hitChance > 0.99 {
		hitChance = 0.99
	}

	// Roll for hit
	hitRoll := c.rng.Float64()
	if hitRoll >= hitChance {
		return DamageResult{
			IsHit:  false,
			Damage: 0,
			IsCrit: false,
		}, nil
	}

	// 2. Calculate base damage
	baseDmg := req.Attacker.Attack
	if req.SkillMultiplier > 0.0 {
		baseDmg = req.Attacker.Attack*req.SkillMultiplier + req.SkillFlatDamage
	}

	// Damage = max(1, Base - Defender Defense)
	damage := baseDmg - req.Defender.Defense
	if damage < 1.0 {
		damage = 1.0
	}

	// 3. Roll critical strike
	critRoll := c.rng.Float64() * 100.0
	isCrit := critRoll < req.Attacker.CritRate
	if isCrit {
		damage = damage * 1.5 * (1.0 + req.Attacker.CritDamageBonus)
	}

	// 4. RNG Variance (±10%)
	variance := 0.9 + c.rng.Float64()*0.2
	finalDamage := math.Round(damage * variance)
	if finalDamage < 1 {
		finalDamage = 1
	}

	return DamageResult{
		IsHit:  true,
		Damage: int32(finalDamage),
		IsCrit: isCrit,
	}, nil
}

func (c *embeddedStatClient) Close() error {
	return nil
}
