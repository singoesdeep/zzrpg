<!-- sha: 4c28a0dea584f6f3eb0dc2c49502998883d15ed8 -->
# ⚔️ Combat, Stats & Idle

## Stat engine (optional plugin)
Derived-stat and damage math run through the embedded Rust **zzstat** library
via purego FFI (`backend/platform/statclient/client.go`). It is loaded by the
optional `backend/plugins/stat` plugin, which provides the `stat` service. A
game that needs no stat math omits this plugin and boots without the `.so`.

## Combat
The `combat` plugin (`backend/plugins/combat/plugin.go`) resolves attacker/
defender via a `CreatureResolver`, computes damage through `statclient`, and on a
kill triggers loot rolls and quest progress. WS `COMBAT_ATTACK` → `COMBAT_DAMAGE`.

## Idle progression (content-driven)
The idle domain (`backend/game/idle`) builds on the engine accrual framework
(`sdk/engine/idle`). Three producer kinds:
- **Stage** — combat idle; reward scales with combat *power* vs stage difficulty
  (efficiency floor gates the too-weak, cap gives diminishing returns).
- **Lifeskill** — gathering; yield/nodes scale with the lifeskill level.
- **Generator** — RTS-style passive resource output, scaled by a building level.

`Service.Accrue` runs the active focus plus every built generator in parallel,
then applies gold/exp, loot, gathered items, lifeskill xp, and wallet resources.
Both offline (on login) and a periodic online tick use the same path; building
upgrades are paid in gold and/or wallet resources. Content:
`backend/content/idle/*.json`.
