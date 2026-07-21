package character

import gprogression "github.com/singoesdeep/zzrpg/gamekit/progression"

// StatGainPerLevel is the flat amount added to each primary base stat per level.
const StatGainPerLevel = 2

// xpCurve is this RPG's leveling curve — level N requires N*N*100 EXP — expressed
// as gamekit/progression's generic Base*N^Exp curve (Base=100, Exp=2). The
// progression MATH (ExperienceForLevel/ApplyExperience below) is gamekit's; this
// package supplies only the curve's parameters and its own per-level stat gains
// (an RPG-specific design choice, not a mechanism gamekit owns).
var xpCurve = gprogression.Curve{Base: 100, Exp: 2}

// ExperienceForLevel returns the experience required to advance from the given
// level.
func ExperienceForLevel(level int32) int64 {
	return gprogression.XPForLevel(xpCurve, level)
}

// ApplyExperience applies gainedExp on top of the current level/experience and
// returns the resulting level, leftover experience, and whether any level was
// gained. This is the domain rule for progression, kept out of the persistence
// layer so it can be unit-tested and evolved without touching SQL.
func ApplyExperience(level int32, currentExp, gainedExp int64) (newLevel int32, newExp int64, leveledUp bool) {
	newLevel, newExp, gained := gprogression.Apply(xpCurve, level, currentExp, gainedExp)
	return newLevel, newExp, gained > 0
}

// ApplyLevelUpStatGains raises each primary base stat by StatGainPerLevel for
// every level gained. It mutates base in place and is a no-op when no levels
// were gained.
func ApplyLevelUpStatGains(base map[string]float64, levelsGained int32) {
	if levelsGained <= 0 {
		return
	}
	gain := float64(levelsGained * StatGainPerLevel)
	for _, s := range []string{"STR", "INT", "DEX", "CON"} {
		base[s] += gain
	}
}
