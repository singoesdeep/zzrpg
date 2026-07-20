<!-- sha: 46fe7591c909ac7cd42c3311e13517a798b43972 -->
# zzrpg Codebase Living LLM Wiki

Welcome to the **zzrpg Engine Living Wiki**, automatically maintained and grounded in the source code via Karpathy's LLM Wiki pattern.

## 📚 Wiki Knowledge Base

| Topic / Category | Summary | Primary Code References | Last Synced SHA |
|---|---|---|---|
| 🏛️ [Architecture](architecture.md) | Four-layer structure (engine/platform/game/plugins), game-agnostic kernel, DI registry, typed event bus, hooks, & Redis Streams fanout | `backend/engine/`, `backend/platform/` | `46fe759` |
| 🧩 [Plugin Subsystem](plugins.md) | Composition adapters, `admin.Describor` UI views, engine-gated runtime activation (`admin.StateManager`) | `backend/plugins/`, `backend/game/` | `46fe759` |
| ⚔️ [Combat & Stat Core](combat-engine.md) | Combat damage math, creature resolvers, & embedded Rust `zzstat` FFI | `backend/plugins/combat/`, `backend/platform/statclient/` | `46fe759` |
| 💾 [Database & Outbox](database-outbox.md) | Store/UnitOfWork seam, PostgreSQL schema, outbox relay, & event_log replay | `backend/engine/store/`, `backend/engine/outbox/` | `46fe759` |
| 🎛️ [Admin Dashboard & APIs](admin-dashboard.md) | Web Admin UI, REST endpoints, WebSocket protocol, & Scalar docs | `backend/plugins/core/api/admin.html` | `46fe759` |

---

## 🔍 How to Use & Audit
- Check freshness anytime: `.llmwiki/freshness.sh`
- Automatic post-commit sync: `scripts/install-llmwiki-hook.sh`
- All wiki pages reference exact source lines using GitHub `file://` links.
