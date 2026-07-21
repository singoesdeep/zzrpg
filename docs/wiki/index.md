<!-- sha: de0c8047441434948fa45002bc4aaf20b005c6d1 -->
# zzrpg Living Wiki

Code-grounded reference for the zzrpg engine and the games built on it. The repo
is two Go modules: **`sdk/`** (the game-agnostic engine + utilities) and
**`backend/`** (game domains, platform infra, plugins, and the runnable games).
For prose guides see [../ARCHITECTURE.md](../ARCHITECTURE.md) and
[../PLUGIN_GUIDE.md](../PLUGIN_GUIDE.md).

## 📚 Knowledge Base

| Topic | Summary | Primary Code | SHA |
|---|---|---|---|
| 🏛️ [Engine Core & Kernel](architecture.md) | Game-agnostic kernel, plugin lifecycle, DI registry, event bus, hooks, idle framework, activation gate | `sdk/engine/`, `sdk/pkg/` | `de0c804` |
| 🧩 [Plugin Subsystem](plugins.md) | Plugin contract, per-plugin schema/content/routes/events, domain-agnostic core | `backend/plugins/`, `backend/game/` | `de0c804` |
| ⚔️ [Combat, Stats & Idle](combat-engine.md) | Optional zzstat plugin, combat flow, content-driven idle (stages/lifeskills/generators) | `backend/plugins/{stat,combat,idle}/` | `de0c804` |
| 💾 [Persistence & Outbox](database-outbox.md) | Store seam, transactional outbox, per-module migrations | `sdk/engine/{store,outbox}/`, `backend/platform/database/` | `de0c804` |
| 🎛️ [Admin Dashboard & APIs](admin-dashboard.md) | Web admin UI, operational endpoints, runtime plugin activation | `backend/plugins/core/` | `de0c804` |

---

## 🔍 Freshness
- Check anytime: `.llmwiki/freshness.sh` (stamps track the last `backend/` commit).
- Auto-sync on commit: `scripts/install-llmwiki-hook.sh`.
