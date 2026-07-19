package statclient

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/contracts"
	zzstat "github.com/singoesdeep/zzstat/bindings/go"
)

// derivedStats is the derived-stat formula pack, loaded once from embedded
// content and shared with the character fallback path.
var derivedStats = content.MustLoadDerivedStats()

type CharacterState struct {
	CharacterID int32
	BaseStats   map[string]float64
	Equipment   []Modifier
	Skills      []Modifier
	ActiveBuffs []Modifier
}

// Modifier is an alias for the shared contracts.Modifier.
type Modifier = contracts.Modifier

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
	// rng is a *math/rand.Rand (not concurrent-safe). CalculateDamage runs
	// concurrently across combat goroutines, so rng access is serialized by rngMu.
	rngMu sync.Mutex
	rng   *rand.Rand
}

// randFloat returns a serialized rng.Float64().
func (c *embeddedStatClient) randFloat() float64 {
	c.rngMu.Lock()
	defer c.rngMu.Unlock()
	return c.rng.Float64()
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

	// 3. Register base derived stats from the content pack. Primary terms
	//    (e.g. HP=CON*15) plus secondary terms (e.g. DEX into ATTACK) become
	//    additive scaling transforms; constant terms (e.g. CRIT_RATE=5) become
	//    constant sources. Equivalent to the previously hardcoded formulas, but
	//    now sharing one source of truth with the Go fallback.
	for _, group := range []map[string][]content.StatTerm{derivedStats.Primary, derivedStats.Secondary} {
		for stat, terms := range group {
			for _, t := range terms {
				if t.Source == "" {
					resolver.RegisterConstantSource(stat, t.Factor)
				} else {
					resolver.RegisterScalingTransform(stat, zzstat.PhaseAdditive, zzstat.RuleAdditive, t.Source, t.Factor)
				}
			}
		}
	}

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
	// The hit/dodge, crit, and ±10% variance logic all live in the data-driven
	// combat formula (content/formulas/combat.json), evaluated by zzstat. This Go
	// path only prepares the scalar inputs, supplies the RNG (so seeding/order is
	// preserved), and applies the final round + minimum-1 clamp. There is no
	// duplicated Go combat formula anymore.

	// Base damage is either the raw attack or the skill-modified value.
	baseDmg := req.Attacker.Attack
	if req.SkillMultiplier > 0.0 {
		baseDmg = req.Attacker.Attack*req.SkillMultiplier + req.SkillFlatDamage
	}

	attacker := zzstat.NewResolver()
	defer attacker.Free()
	attacker.RegisterConstantSource("BASE_DMG", baseDmg)
	attacker.RegisterConstantSource("LEVEL", float64(req.Attacker.Level))
	attacker.RegisterConstantSource("ACC", req.Attacker.AccModifiers)
	attacker.RegisterConstantSource("CRIT_RATE", req.Attacker.CritRate)
	attacker.RegisterConstantSource("CRIT_DMG_BONUS", req.Attacker.CritDamageBonus)

	defender := zzstat.NewResolver()
	defer defender.Free()
	defender.RegisterConstantSource("DEF", req.Defender.Defense)
	defender.RegisterConstantSource("DEX", req.Defender.Dex)
	defender.RegisterConstantSource("DODGE_MOD", req.Defender.DodgeModifiers)

	attackerCtx := zzstat.NewContext()
	defer attackerCtx.Free()
	defenderCtx := zzstat.NewContext()
	defer defenderCtx.Free()

	// The RNG callback is drawn in formula-evaluation order: hit, then crit, then
	// variance — matching the previous Go implementation exactly.
	raw, isHit, isCrit, err := zzstat.EvaluateCombatEx(
		content.CombatFormulaJSON(),
		attacker, attackerCtx,
		defender, defenderCtx,
		c.randFloat,
	)
	if err != nil {
		return DamageResult{}, err
	}

	if !isHit {
		return DamageResult{IsHit: false, Damage: 0, IsCrit: false}, nil
	}

	finalDamage := math.Round(raw)
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
