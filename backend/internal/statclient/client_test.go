package statclient

import (
	"context"
	"testing"
)

func TestStatClient(t *testing.T) {
	// 1. Setup client (loads the FFI library dynamically)
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// 2. Test stats calculation
	state := CharacterState{
		CharacterID: 101,
		BaseStats: map[string]float64{
			"STR": 15, "CON": 15, "INT": 5, "DEX": 10,
		},
		Equipment: []Modifier{
			{Stat: "ATTACK", Operation: "ADD", Value: 100, Priority: 20, SourceID: "sword_01"},
			{Stat: "ATTACK", Operation: "MULTIPLY", Value: 0.20, Priority: 30, SourceID: "buff_atk"},
		},
	}

	result, err := client.Calculate(context.Background(), state)
	if err != nil {
		t.Fatalf("calculation failed: %v", err)
	}


	// STR=15, DEX=10 -> base_attack = 15*2.0 + 10*0.5 = 35.0
	// Equipment: flat +100.0, mult +20%
	// Expected attack: (35.0 + 100.0) * (1.0 + 0.20) = 135.0 * 1.2 = 162.0
	expectedAttack := 162.0
	if result["ATTACK"] != expectedAttack {
		t.Errorf("unexpected calculation result for ATTACK: expected %f, got %f", expectedAttack, result["ATTACK"])
	}

	// CON=15 -> base_hp = 15*15.0 = 225.0
	expectedHP := 225.0
	if result["HP"] != expectedHP {
		t.Errorf("unexpected calculation result for HP: expected %f, got %f", expectedHP, result["HP"])
	}

	// 3. Test combat damage calculation
	combatReq := CalculateDamageReq{
		Attacker: CombatStats{
			Level:           10,
			Attack:          150,
			CritRate:        100.0, // 100% crit rate
			CritDamageBonus: 0.5,   // +50% Crit Damage (so total 2.0x multiplier)
		},
		Defender: CombatStats{
			Level:   10,
			Defense: 50,
			Dex:     10,
		},
	}

	combatRes, err := client.CalculateDamage(context.Background(), combatReq)
	if err != nil {
		t.Fatalf("combat calculation failed: %v", err)
	}

	// Normal base damage = max(1, 150 - 50) = 100.
	// Critical strike: 100% crit rate -> damage = 100 * 1.5 * (1 + 0.5) = 225.
	// Variance: 225 * RNG(0.9..1.1) -> damage must be between 202 and 248.
	if !combatRes.IsHit {
		t.Errorf("expected hit, got miss")
	}
	if !combatRes.IsCrit {
		t.Errorf("expected crit, got normal")
	}
	if combatRes.Damage < 202 || combatRes.Damage > 248 {
		t.Errorf("unexpected combat damage result: got %d, expected range [202, 248]", combatRes.Damage)
	}
}
