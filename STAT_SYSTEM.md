# Stat System Design: Embedded Rust zzstat Engine FFI Integration

All stat calculations for the game are outsourced to the embedded Rust `zzstat` library. The Go backend does not perform stat math itself. It acts as an orchestrator that gathers active modifiers, registers them to the in-process resolver, and caches the resolved values.

---

## 1. Stat Pipeline

```
[Character Base Stats]   (STR, INT, DEX, CON)
        │
        ▼
[Gather Modifiers]        (Equipment + Skills + Buffs + Guild + Temp Effects)
        │
        ▼
[Format payload]          (Construct StatModifier list with Priority & Source)
        │
        ▼
[FFI: In-Process Calls]  ───(Go Client)───► [Embedded Rust Core]
                                                │
                                                ├── Group by Stat Type
                                                ├── Order by Priority (0 -> Base, 1 -> Add, 2 -> Multiply)
                                                ├── Apply Stacking Logic (Refuse duplicate Buff SourceIDs)
                                                └── Compute formulas (Base -> Primary -> Derived -> Final)
                                                │
[Update Cache & DB]      ◄──(Go Backend)───────┘
```

---

## 2. Modifiers & Operations

Each modifier has:
- **Stat**: The target statistic (e.g. `HP`, `ATTACK`, `DEFENSE`, `CRIT_RATE`, `STR`, `INT`).
- **Operation**: `ADD` or `MULTIPLY`.
- **Value**: The numerical changes (e.g., `120` for ADD, `0.15` for 15% MULTIPLY).
- **Priority**: Determines calculation order.
  - Priority `10`: Base values.
  - Priority `20`: Flat additive changes from equipment/skills (e.g. `+100 ATTACK`).
  - Priority `30`: Percentage modifiers (e.g. `+20% ATTACK`).
  - Priority `40`: Late-stage final overrides/reductions (e.g. shield wall `-50% Damage Received`).
- **Source Tracking**: Identifies where the modifier came from (e.g. `Equipment:dragon_sword_0`). Used by the Rust engine to resolve stacking rules.

---

## 3. Stacking Rules & Overrides

When multiple effects modifying the same stat are active, Rust applies these stacking rules:
- **Unique Source Stacking**: Equipment and skills with unique `source_id` values stack additively/multiplicatively.
- **Buff Overrides**: Buffs from the same `buff_id` (e.g. two `aura_of_sword` buffs cast by different players) do NOT stack. Rust will keep only the one with the highest value or the longest remaining duration.
- **Debuff Cap**: Debuffs like `poison` may have intensity caps (e.g., maximum speed reduction cannot exceed 50%).

---

## 4. Stat Formulas (Data-Driven Derived Stats)

To keep calculations completely decoupled, the Go client configures the embedded zzstat engine with standard MMORPG derived formulas. 
The input consists of Base Stats: `STR` (Strength), `INT` (Intelligence), `DEX` (Dexterity), `CON` (Constitution/Vitality).

1. **HP Calculation**:
   $$\text{Final HP} = (\text{CON} \times 15 + \sum \text{HP\_ADD}) \times (1 + \sum \text{HP\_MULTIPLY})$$

2. **MP Calculation**:
   $$\text{Final MP} = (\text{INT} \times 10 + \sum \text{MP\_ADD}) \times (1 + \sum \text{MP\_MULTIPLY})$$

3. **Attack Power Calculation**:
   $$\text{Base Attack} = (\text{STR} \times 2.0) + (\text{DEX} \times 0.5)$$
   $$\text{Final Attack} = (\text{Base Attack} + \sum \text{ATTACK\_ADD}) \times (1 + \sum \text{ATTACK\_MULTIPLY})$$

4. **Defense Calculation**:
   $$\text{Base Defense} = (\text{CON} \times 1.0) + (\text{STR} \times 0.2)$$
   $$\text{Final Defense} = (\text{Base Defense} + \sum \text{DEFENSE\_ADD}) \times (1 + \sum \text{DEFENSE\_MULTIPLY})$$

---

## 5. Go Client Interface (`statclient`)

Go backend defines the following client abstraction to decouple dependencies:

```go
package statclient

import "context"

type CharacterState struct {
	CharacterID int32
	BaseStats   map[string]float64
	Equipment   []Modifier
	Skills      []Modifier
	ActiveBuffs []Modifier
}

type Modifier struct {
	Stat      string  // e.g. "HP", "STR", "ATTACK"
	Operation string  // "ADD", "MULTIPLY"
	Value     float64
	Priority  int32
	SourceID  string  // e.g. "item:dragon_sword"
}

type Client interface {
	Calculate(ctx context.Context, state CharacterState) (map[string]float64, error)
}
```

The Go backend calls this interface inside transaction scopes or event listeners (e.g. on equipping an item, on leveling up) to update cached player stats.
