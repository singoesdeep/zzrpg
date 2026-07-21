package stats

import "context"

// Term is one additive contribution to a derived stat: a constant (empty Source)
// of Factor, or the named base stat scaled by Factor.
type Term struct {
	Source string  `json:"source"`
	Factor float64 `json:"factor"`
}

// Formulas describes how derived stats are computed from base stats. Primary and
// Secondary terms both accumulate into the same derived stat (mirroring the
// classic primary/secondary split), so e.g. ATTACK can draw from STR and DEX.
type Formulas struct {
	Primary   map[string][]Term `json:"primary"`
	Secondary map[string][]Term `json:"secondary"`
}

// formulaResolver derives stats from data-driven formulas, per entity kind. It
// is pure Go — no native dependency — so gamekit stands alone; a game may still
// plug in a different StatResolver (e.g. a Rust engine).
type formulaResolver struct {
	byKind map[string]Formulas
}

// NewFormulaResolver builds a resolver from per-kind formulas; the "" key is the
// default used when a kind has no specific formulas.
func NewFormulaResolver(byKind map[string]Formulas) StatResolver {
	return formulaResolver{byKind: byKind}
}

func (r formulaResolver) Derive(_ context.Context, kind string, base map[string]float64) (map[string]float64, error) {
	f, ok := r.byKind[kind]
	if !ok {
		f = r.byKind[""]
	}
	out := make(map[string]float64)
	accumulate := func(m map[string][]Term) {
		for stat, terms := range m {
			for _, t := range terms {
				if t.Source == "" {
					out[stat] += t.Factor
				} else {
					out[stat] += base[t.Source] * t.Factor
				}
			}
		}
	}
	accumulate(f.Primary)
	accumulate(f.Secondary)
	return out, nil
}
