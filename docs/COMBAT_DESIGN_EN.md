# Combat System Design: Data-Driven Mechanics (EN)

The combat module orchestrates real-time combat calculations. By retrieving dynamic stats from PostgreSQL/Redis (precalculated in-process via Go-Rust FFI using the `zzstat` engine), the combat system applies damage, critical hits, misses, blocks, and status effects.

---

## 1. Combat Execution Pipeline

```
          [Client Attack Trigger] (WebSocket)
                     │
                     ▼
          [Load Attacker & Target]
         (Fetch Cached Derived Stats)
                     │
                     ▼
         [Calculate Accuracy / Hit]
        (DEX and Dodge rate check)
        /                          \
    (Hit)                          (Miss) ──► [Broadcast Miss Event]
      │
      ▼
[Calculate Raw Damage] (Base Attack - Defense)
      │
      ▼
[Apply Skill Multipliers & CRIT]
      │
      ▼
[Apply Final Multipliers & Variance] (±10% RNG)
      │
      ▼
  [Determine Status Effects] (Poison, Burn, Stun)
      │
      ▼
   [Apply HP Reductions & Save]
      │
      ├────────────────────────┐
      ▼                        ▼
[Broadcast Damage]      [Target Dead?] ──► [Trigger Loot, EXP, Quests]
```

---

## 2. Hit and Miss Mechanics

To determine whether an attack successfully hits:

$$\text{Dodge Rate} = \frac{\text{Defender DEX} \times 0.2 + \text{Defender Dodge Modifiers}}{\text{Attacker Level} \times 1.5}$$
$$\text{Base Hit Chance} = (100\% - \text{Dodge Rate}) + \text{Attacker Acc Modifiers}$$

- **Cap Constraints**: Hit chance is bounded between $70\%$ (minimum guarantee to hit) and $99\%$ (always a slight chance to miss).
- If the attack misses, execution stops, and a websocket `COMBAT_DAMAGE` packet is sent with `is_miss: true` and `damage: 0`.

---

## 3. Damage Calculation Formulas

### 3.1 Normal Physical Attack
$$\text{Damage}_{\text{normal}} = \max(1, \text{Attacker Attack} - \text{Defender Defense})$$

### 3.2 Skill Attack (e.g. Fireball)
Skill multipliers are fetched from the database `skill_definitions` using the player's current skill level.
$$\text{Base Damage} = \text{Attacker Attack} \times \text{SkillMultiplier}_{\text{level}} + \text{SkillFlatDamage}_{\text{level}}$$
$$\text{Damage}_{\text{skill}} = \max(1, \text{Base Damage} - \text{Defender Defense})$$

### 3.3 Critical Strike & Damage Variance
- **Critical Strike**: If a random roll is below the attacker's `CRIT_RATE`, damage is multiplied:
  $$\text{Damage}_{\text{crit}} = \text{Damage} \times 1.5 \times (1 + \text{CritDamageBonus})$$
- **RNG Variance**: To avoid static damage values, a final variance of $\pm10\%$ is applied:
  $$\text{Final Damage} = \text{Damage} \times \text{UniformRandom}(0.9, 1.1)$$

---

## 4. Status Effects and Debuffs

Skills can inflict secondary status effects, defined entirely in the database `skill_definitions` metadata:
```json
{
  "status_effects": [
    {
      "buff_definition_id": "poison",
      "chance": 0.20,
      "duration_ms": 10000
    }
  ]
}
```
If the status effect triggers:
1. The Go backend inserts the active debuff into `active_buffs`.
2. This invalidates the defender's stats.
3. The defender's stats are recalculated in-process via the `zzstat` engine (e.g. speed reduced by 30%, or HP ticks lost every second).
4. An update is broadcast to the map channel.

---

## 5. Death and Loot Dispatch

If the target's HP drops to $\le 0$:
1. The combat module flags the character/mob as dead.
2. An asynchronous event is dispatched via `EventBus` (`events.CharacterKilled` or `events.MonsterKilled`).
3. The **Loot Module** catches this event, rolls on the creature's `loot_table`, and spawns the drops.
4. The **Quest Module** catches this event, increments kill counts for active quests, and sends progress updates.
5. The **Character Module** awards experience points and gold to the attacker.
