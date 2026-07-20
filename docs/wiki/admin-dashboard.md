<!-- sha: 46fe7591c909ac7cd42c3311e13517a798b43972 -->
# 🎛️ Web Admin Dashboard & APIs

The `zzrpg` server embeds a single-page Web Admin Dashboard served at `GET /admin` by `corePlugin` ([backend/plugins/core/plugin.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/core/plugin.go#L258-L267)).

## 1. Embedded Admin Dashboard (`/admin`)

The admin interface ([backend/plugins/core/api/admin.html](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/core/api/admin.html)) is built with Vanilla HTML5/CSS3 and JavaScript without external build tools:

- **Dashboard Overview:** Real-time health metrics, DB connection status, Redis status.
- **Items Catalog Manager:** Create, update, list, and delete item definitions.
- **Loot Tables Inspector:** View probability-based drop tables.
- **Quest Chain Manager:** Create and view multi-step quest chains.
- **Character Inspector & Grant:** Inspect stats, active inventory, and grant items.
- **Plugins Catalog:** Dynamic list of registered plugins, dependencies, exposed endpoints, and live **Activate / Deactivate** toggle switches.
- **Event Console & WS Stream:** Real-time WebSocket terminal for testing `CHAT`, `COMBAT_ATTACK`, and `SELECT_CHARACTER` packets.

## 2. API Endpoints

- `GET /health` — Simple database ping health check.
- `GET /readyz` — Database + Redis readiness probe.
- `GET /admin` — Single-page HTML Admin Dashboard.
- `GET /docs` — Embedded Scalar OpenAPI documentation interactive UI.
- `GET /api/openapi.json` — OpenAPI 3.0 specification.
- `GET /api/v1/admin/plugins` — List registered plugins and status.
- `POST /api/v1/admin/plugins/{name}/toggle` — Toggle plugin active/disabled status.

## 3. Grounding & Code References

- Admin HTML Source: [backend/plugins/core/api/admin.html](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/core/api/admin.html)
- Core Plugin Handlers: [backend/plugins/core/plugin.go:L258-L300](file:///home/singo/github.com/singoesdeep/zzrpg/backend/plugins/core/plugin.go#L258-L300)
