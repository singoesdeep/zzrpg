# Developer Guide: Customizing Stats, Items, and Loot (EN)

This guide describes how to customize item definitions, stat formulas, combat mechanics, and monster drop rates in **zzrpg** with or without writing code.

---

## 1. Customizing Item Definitions and Stats
Item definitions are completely **data-driven**. They are not hardcoded; they are stored as records in the database.

### Method A: Via Scalar API Docs (Live Update)
You can customize or add items while the server is running by using the Scalar Docs (`http://localhost:8080/docs`) client:
* **Add a New Item**: Send a `POST /api/v1/admin/items` request with the following JSON payload:
  ```json
  {
    "id": "dragon_sword_9",
    "name": "Dragon Sword +9",
    "item_type": "WEAPON",
    "slot_type": "WEAPON",
    "min_level": 50,
    "class_restriction": "WARRIOR",
    "base_durability": 150,
    "stats": {
      "ATTACK": 150,
      "DEX": 10
    }
  }
  ```
* **Update an Existing Item**: Use `PUT /api/v1/admin/items/{id}` to modify stats or level restrictions live.

### Method B: Via SQL Migrations (Permanent)
To ensure the item is automatically registered across all development environments, create a database migration:
1. Create a new SQL file in `backend/internal/database/migrations/`.
2. Add the insert statement:
   ```sql
   INSERT INTO item_definitions (id, name, item_type, slot_type, min_level, class_restriction, base_durability, stats)
   VALUES ('phoenix_armor', 'Phoenix Armor', 'ARMOR', 'ARMOR', 40, 'MAGE', 120, '{"DEFENSE": 85, "HP": 200}');
   ```

---

## 2. Modifying Stat Formulas
Derived Stats (HP, MP, Attack, Defense, Crit) are computed in-process via Go-Rust FFI bindings, defined inside `backend/internal/statclient/client.go` using the `zzstat` engine's DSL.

### Steps to Modify Formulas:
1. **Open File**: Open [backend/internal/statclient/client.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/internal/statclient/client.go).
2. **Locate Formula Blocks**: Locate the derived stat definitions inside the `Calculate` method:
   ```go
   // HP = CON * 15.0
   resolver.RegisterScalingTransform("HP", zzstat.PhaseAdditive, zzstat.RuleAdditive, "CON", 15.0)
   // MP = INT * 10.0
   resolver.RegisterScalingTransform("MP", zzstat.PhaseAdditive, zzstat.RuleAdditive, "INT", 10.0)
   // ATTACK = STR * 2.0 + DEX * 0.5
   resolver.RegisterScalingTransform("ATTACK", zzstat.PhaseAdditive, zzstat.RuleAdditive, "STR", 2.0)
   resolver.RegisterScalingTransform("ATTACK", zzstat.PhaseAdditive, zzstat.RuleAdditive, "DEX", 0.5)
   // DEFENSE = CON * 1.0 + STR * 0.2
   resolver.RegisterScalingTransform("DEFENSE", zzstat.PhaseAdditive, zzstat.RuleAdditive, "CON", 1.0)
   resolver.RegisterScalingTransform("DEFENSE", zzstat.PhaseAdditive, zzstat.RuleAdditive, "STR", 0.2)
   ```
3. **Edit Formula**: For example, to adjust Defense, update the scaling factors for `"CON"` and `"STR"`.
4. **Run Go Tests**:
   ```bash
   cd backend/internal/statclient
   go test -v .
   ```

---

## 3. Modifying Combat Formulas (Accuracy, Crit, Variance)
Dodge rates, critical multipliers, and damage variances are calculated inside Go backend (`backend/internal/statclient/client.go`) within the `CalculateDamage` function.

### Customization:
Modify the `CalculateDamage` function in [backend/internal/statclient/client.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/internal/statclient/client.go):

* **Hit Chance Capping**:
  By default, hit rate is capped between 70% and 99%. To relax this:
  ```go
  // Cap hit chance between 70% (0.70) and 99% (0.99)
  hitChance := baseHitChance
  if hitChance < 0.70 {
  	hitChance = 0.70
  } else if hitChance > 0.99 {
  	hitChance = 0.99
  }
  ```
* **Critical Multiplier**:
  When a critical strike rolls successfully, damage is multiplied by 1.5. To make it 2.0:
  ```go
  isCrit := critRoll < req.Attacker.CritRate
  if isCrit {
  	damage = damage * 2.0 * (1.0 + req.Attacker.CritDamageBonus)
  }
  ```
* **Damage RNG Variance**:
  To reduce damage fluctuations from $\pm\%10$ to $\pm\%5$:
  ```go
  // RNG Variance (±10%)
  variance := 0.9 + c.rng.Float64()*0.2
  ```

---

## 4. Modifying Loot Tables
Monster drop rates are stored as JSONB data in `loot_tables` database records.

### JSONB Structure:
Each loot table holds an array of drop definitions:
```json
[
  {"item_definition_id": "gold", "rate": 5000, "min": 50, "max": 150},
  {"item_definition_id": "dragon_sword_0", "rate": 500, "min": 1, "max": 1}
]
```
* `rate`: drop probability out of 10,000 (e.g. 5000 = 50%, 500 = 5%).
* `min` and `max`: drop quantity boundaries.

### Update Table via REST:
Send a `POST /api/v1/admin/loot` request:
```json
{
  "id": "dummy_drops",
  "description": "Training Dummy Loot Table",
  "entries": [
    {
      "item_definition_id": "gold",
      "rate": 10000,
      "min": 100,
      "max": 200
    },
    {
      "item_definition_id": "dragon_sword_0",
      "rate": 1000,
      "min": 1,
      "max": 1
    }
  ]
}
```

---

## 5. Running Verification Tests
Always run test suites to verify Go system logic integrity after making changes:

* **Verify Go Logic & E2E Sockets**:
  ```bash
  cd backend
  go test -v ./...
  ```
