<!-- sha: 59351ea3217dd002ea24f13d1390397ba6749bd0 -->
# zzrpg Codebase Living LLM Wiki

Welcome to the **zzrpg Engine Living Wiki**, automatically maintained and grounded in the source code via Karpathy's LLM Wiki pattern.

## 📚 Wiki Knowledge Base

| Topic / Category | Summary | Primary Code References | Last Synced SHA |
|---|---|---|---|
| 🏛️ [Architecture](architecture.md) | Four-layer structure (engine/platform/game/plugins), game-agnostic kernel, DI registry, typed event bus, hooks, & Redis Streams fanout | `backend/engine/`, `backend/platform/` | `59351ea` |
| 🧩 [Plugin Subsystem](plugins.md) | Composition adapters, `admin.Describor` UI views, engine-gated runtime activation (`admin.StateManager`) | `backend/plugins/`, `backend/game/` | `59351ea` |
| ⚔️ [Combat & Stat Core](combat-engine.md) | Combat damage math, creature resolvers, & embedded Rust `zzstat` FFI | `backend/plugins/combat/`, `backend/platform/statclient/` | `59351ea` |
| 💾 [Database & Outbox](database-outbox.md) | Store/UnitOfWork seam, PostgreSQL schema, outbox relay, & event_log replay | `backend/engine/store/`, `backend/engine/outbox/` | `59351ea` |
| 🎛️ [Admin Dashboard & APIs](admin-dashboard.md) | Web Admin UI, REST endpoints, WebSocket protocol, & Scalar docs | `backend/plugins/core/api/admin.html` | `59351ea` |

---

## 🔍 How to Use & Audit
- Check freshness anytime: `.llmwiki/freshness.sh`
- Automatic post-commit sync: `scripts/install-llmwiki-hook.sh`
- All wiki pages reference exact source lines using GitHub `file://` links.
