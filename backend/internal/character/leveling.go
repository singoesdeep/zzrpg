package character

// StatGainPerLevel is the flat amount added to each primary base stat per level.
const StatGainPerLevel = 2

// ExperienceForLevel returns the experience required to advance from the given
// level. The curve is level N requires N*N*100 EXP.
func ExperienceForLevel(level int32) int64 {
	return int64(level) * int64(level) * 100
}

// ApplyExperience applies gainedExp on top of the current level/experience and
// returns the resulting level, leftover experience, and whether any level was
// gained. This is the domain rule for progression, kept out of the persistence
// layer so it can be unit-tested and evolved without touching SQL.
func ApplyExperience(level int32, currentExp, gainedExp int64) (newLevel int32, newExp int64, leveledUp bool) {
	newLevel = level
	newExp = currentExp + gainedExp
	for {
		reqExp := ExperienceForLevel(newLevel)
		if newExp >= reqExp {
			newExp -= reqExp
			newLevel++
			leveledUp = true
		} else {
			break
		}
	}
	return newLevel, newExp, leveledUp
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
