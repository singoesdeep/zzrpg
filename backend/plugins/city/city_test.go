package city

import (
	"testing"

	eidle "github.com/singoesdeep/zzrpg/backend/engine/idle"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
)

func TestBuildingProducer_ScalesWithLevel(t *testing.T) {
	p := buildingProducer{def: BuildingDef{Resource: "wood", BasePerMin: 12, PerLevel: 6}}
	// level 1 -> 18/min over 5 min = 90
	out := p.Produce(5, eidle.State{Vars: map[string]float64{"level": 1}}, nil)
	if out.Amounts["wood"] != 90 {
		t.Fatalf("level 1 over 5min = 90 wood, got %d", out.Amounts["wood"])
	}
	// level 3 -> 30/min over 2 min = 60
	out = p.Produce(2, eidle.State{Vars: map[string]float64{"level": 3}}, nil)
	if out.Amounts["wood"] != 60 {
		t.Fatalf("level 3 over 2min = 60 wood, got %d", out.Amounts["wood"])
	}
}

func TestNewService_LoadsContentViaRegistry(t *testing.T) {
	reg := registry.New()
	svc, err := NewService(nil, reg, buildingsJSON)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if len(svc.Buildings()) != 3 {
		t.Fatalf("expected 3 buildings from content, got %d", len(svc.Buildings()))
	}
	// The generic content registry actually holds the typed BuildingDef.
	def, ok := registry.Content[BuildingDef](reg, ContentKind, "gold_mine")
	if !ok || def.Resource != "gold" {
		t.Fatalf("gold_mine content not registered correctly: %+v ok=%v", def, ok)
	}
}
