// Package skills is a data-driven, server-authoritative skill system. Skill
// definitions live in a content pack (content/skills/skills.json); the server —
// not the client — decides a skill's damage multiplier, flat bonus, mana cost and
// class restriction, so a client can only ask to use a skill by ID.
package skills

import (
	"sort"

	"github.com/singoesdeep/zzrpg/backend/content"
)

// SkillDef is a resolved skill definition.
type SkillDef struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Class      string  `json:"class"` // "" = any class
	Multiplier float64 `json:"multiplier"`
	FlatDamage float64 `json:"flat_damage"`
	ManaCost   float64 `json:"mana_cost"`
}

// Service resolves and lists the skills defined in the content pack.
type Service struct {
	defs map[string]SkillDef
	list []SkillDef
}

// NewService loads the skill pack from embedded content (honouring content
// overrides). It panics on a malformed pack (a build-time/config error).
func NewService() *Service {
	packs := content.MustLoadSkills()
	s := &Service{defs: make(map[string]SkillDef, len(packs))}
	for id, sk := range packs {
		d := SkillDef{
			ID:         id,
			Name:       sk.Name,
			Class:      sk.Class,
			Multiplier: sk.Multiplier,
			FlatDamage: sk.FlatDamage,
			ManaCost:   sk.ManaCost,
		}
		s.defs[id] = d
		s.list = append(s.list, d)
	}
	sort.Slice(s.list, func(i, j int) bool { return s.list[i].ID < s.list[j].ID })
	return s
}

// Resolve returns the definition for skillID.
func (s *Service) Resolve(skillID string) (SkillDef, bool) {
	d, ok := s.defs[skillID]
	return d, ok
}

// List returns all skill definitions, ordered by ID.
func (s *Service) List() []SkillDef {
	out := make([]SkillDef, len(s.list))
	copy(out, s.list)
	return out
}
