// Package content holds the game's data-driven content packs (formulas, class
// definitions, ...) and loads them from embedded JSON. Moving these values out
// of Go source is the first step of the data-driven engine: game designers tune
// numbers in content/ rather than editing code, and there is a single source of
// truth for each rule instead of duplicated literals.
package content

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed formulas/derived_stats.json classes/classes.json
var files embed.FS

// StatTerm is one additive term of a derived stat. A term with an empty Source
// is a flat constant of Factor; otherwise it scales the named base stat by
// Factor.
type StatTerm struct {
	Source string  `json:"source"`
	Factor float64 `json:"factor"`
}

// DerivedStats describes how derived stats (HP, ATTACK, ...) are computed from
// base stats. Primary terms drive both the zzstat resolver and the Go fallback;
// Secondary terms drive only the zzstat resolver (the fallback intentionally
// uses primary terms only — see character.FallbackDerivedStats).
type DerivedStats struct {
	Primary   map[string][]StatTerm `json:"primary"`
	Secondary map[string][]StatTerm `json:"secondary"`
}

// ClassDefs maps a class name to its starting base stats.
type ClassDefs map[string]map[string]float64

// LoadDerivedStats reads the embedded derived-stat formula pack.
func LoadDerivedStats() (*DerivedStats, error) {
	raw, err := files.ReadFile("formulas/derived_stats.json")
	if err != nil {
		return nil, fmt.Errorf("read derived_stats.json: %w", err)
	}
	var ds DerivedStats
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, fmt.Errorf("parse derived_stats.json: %w", err)
	}
	return &ds, nil
}

// LoadClasses reads the embedded class-definition pack.
func LoadClasses() (ClassDefs, error) {
	raw, err := files.ReadFile("classes/classes.json")
	if err != nil {
		return nil, fmt.Errorf("read classes.json: %w", err)
	}
	var cd ClassDefs
	if err := json.Unmarshal(raw, &cd); err != nil {
		return nil, fmt.Errorf("parse classes.json: %w", err)
	}
	return cd, nil
}

// MustLoadDerivedStats is LoadDerivedStats but panics on error. The content is
// embedded, so a failure is a build-time programmer error, surfaced at startup.
func MustLoadDerivedStats() *DerivedStats {
	ds, err := LoadDerivedStats()
	if err != nil {
		panic(err)
	}
	return ds
}

// MustLoadClasses is LoadClasses but panics on error.
func MustLoadClasses() ClassDefs {
	cd, err := LoadClasses()
	if err != nil {
		panic(err)
	}
	return cd
}
