<!-- sha: c1802da1a088d9e83667ec14327f919135081e7f -->
# ⚔️ Combat Engine & Stat Core

The combat engine is driven by the `combat` plugin ([backend/plugins/combat/plugin.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/combat/plugin.go)) and relies on embedded CGo FFI bindings to the Rust `zzstat` high-performance stat calculation engine.

## 1. High-Performance Rust FFI (`zzstat`)

Attribute evaluation (Strength, Agility, Intelligence -> Base HP/MP, Damage, Defense, Critical Multipliers) is performed via CGo calling Rust `libzzstat_ffi.so` ([backend/platform/statclient/client.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/platform/statclient/client.go#L1-L60)).

- **StatHolder Seam:** Wraps `statclient.Client` interface to avoid registry type assertion ambiguity ([backend/platform/statclient/client.go#L40-L60](file:///home/singo/github.com/singoesdeep/zzrpg/backend/platform/statclient/client.go#L40-L60)).

## 2. Creature Resolver & Mob Spawns

Combat supports both **Player vs Player (PvP)** and **Player vs Environment (PvE)**.

- **Creature Resolver:** Mobs/creatures (e.g. Wolf, Goblin, Dragon) are resolved through `CreatureResolver` interface in [backend/plugins/combat/creatures.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/combat/creatures.go#L1-L45).
- Defender IDs in range `9000+` map directly to instantiated mob template definitions.

## 3. Combat Flow & WebSocket Packets

1. Client sends `COMBAT_ATTACK` WebSocket frame (`defender_id`, `skill_id`).
2. Combat plugin resolves attacker & defender stats via `statclient`.
3. Damage math calculates hit result, critical chance, and variance.
4. If defender HP reaches 0, `CombatKilled` event triggers loot rolls via `loot` plugin and quest progress via `quests` plugin.
5. Server broadcasts `COMBAT_DAMAGE` packet back to client.

## 4. Grounding & Code References

- Combat Plugin & WebSocket Handler: [plugins/combat/plugin.go:L1-L100](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/combat/plugin.go#L1-L100)
- Creature Resolvers: [plugins/combat/creatures.go:L1-L45](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/combat/creatures.go#L1-L45)
- StatClient FFI Binding: [statclient/client.go:L1-L60](file:///home/singo/github.com/singoesdeep/zzrpg/backend/platform/statclient/client.go#L1-L60)
