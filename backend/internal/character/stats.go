package character

// FallbackDerivedStats computes derived stats from base stats using a simplified
// formula. It is the single source of truth for the fallback path used both at
// character creation (initial derived stats) and when the Rust zzstat resolver
// is unavailable (nil client).
//
// NOTE: the authoritative formula lives in the Rust resolver (see statclient),
// which additionally factors secondary terms (e.g. DEX into ATTACK, STR into
// DEFENSE). This fallback intentionally uses the primary terms only; keep the
// base coefficients here in sync with the resolver if they change.
func FallbackDerivedStats(base map[string]float64) map[string]float64 {
	return map[string]float64{
		"HP":        base["CON"] * 15,
		"MP":        base["INT"] * 10,
		"ATTACK":    base["STR"] * 2,
		"DEFENSE":   base["CON"] * 1,
		"CRIT_RATE": 5, // 5% default
	}
}
