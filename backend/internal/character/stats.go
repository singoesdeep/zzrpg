package character

import "github.com/singoesdeep/zzrpg/backend/content"

// derivedStats is the derived-stat formula pack, loaded once from embedded
// content at startup.
var derivedStats = content.MustLoadDerivedStats()

// FallbackDerivedStats computes derived stats from base stats using the primary
// terms of the derived-stat content pack. It is the single source of truth for
// the fallback path used both at character creation (initial derived stats) and
// when the Rust zzstat resolver is unavailable (nil client).
//
// The authoritative formula lives in the Rust resolver (see statclient), which
// additionally applies the pack's Secondary terms (e.g. DEX into ATTACK, STR
// into DEFENSE). This fallback intentionally uses the primary terms only; both
// paths now read the same content pack (content/formulas/derived_stats.json),
// so the coefficients cannot drift.
func FallbackDerivedStats(base map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(derivedStats.Primary))
	for stat, terms := range derivedStats.Primary {
		var sum float64
		for _, t := range terms {
			if t.Source == "" {
				sum += t.Factor
			} else {
				sum += base[t.Source] * t.Factor
			}
		}
		out[stat] = sum
	}
	return out
}
