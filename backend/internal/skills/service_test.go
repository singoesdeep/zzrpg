package skills

import "testing"

func TestSkillServiceResolveAndList(t *testing.T) {
	s := NewService()

	d, ok := s.Resolve("power_strike")
	if !ok || d.Class != "WARRIOR" || d.Multiplier != 1.6 || d.ManaCost != 12 {
		t.Errorf("power_strike: got %+v ok=%v", d, ok)
	}
	if _, ok := s.Resolve("nope"); ok {
		t.Error("expected unknown skill to be unresolvable")
	}

	list := s.List()
	if len(list) != 4 {
		t.Fatalf("expected 4 skills, got %d", len(list))
	}
	if list[0].ID != "backstab" { // sorted by ID
		t.Errorf("expected list sorted by ID (backstab first), got %s", list[0].ID)
	}
}
