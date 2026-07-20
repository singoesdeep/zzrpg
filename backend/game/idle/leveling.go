package idle

import (
	"math"

	"github.com/singoesdeep/zzrpg/backend/content"
)

// XPForLevel returns the xp required to advance from the given lifeskill level,
// following the content curve (Base * level^Exp). Mirrors the character leveling
// rule so lifeskills level on the same predictable shape, just tuned separately.
func XPForLevel(curve content.LifeskillCurve, level int32) int64 {
	if level < 1 {
		level = 1
	}
	return int64(curve.Base * math.Pow(float64(level), curve.Exp))
}

// ApplyLifeskillXP applies gained xp on top of a current level/xp and returns the
// resulting level, leftover xp, and whether any level was gained. Pure domain
// rule, kept out of persistence so it is unit-testable.
func ApplyLifeskillXP(curve content.LifeskillCurve, level int32, currentXP, gained int64) (newLevel int32, newXP int64, leveledUp bool) {
	newLevel = level
	if newLevel < 1 {
		newLevel = 1
	}
	newXP = currentXP + gained
	for {
		req := XPForLevel(curve, newLevel)
		if req > 0 && newXP >= req {
			newXP -= req
			newLevel++
			leveledUp = true
		} else {
			break
		}
	}
	return newLevel, newXP, leveledUp
}
