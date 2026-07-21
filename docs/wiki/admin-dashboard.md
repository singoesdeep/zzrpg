<!-- sha: 1ec913a58ecaea4aad8d0f10d528442c4922f119 -->
# 🎛️ Admin Dashboard & APIs

The `core` plugin serves a single-page Admin Dashboard at `GET /admin`
(`backend/plugins/core/api/admin.html`) and the operational endpoints.

## Endpoints
- `GET /health`, `GET /readyz` — liveness / readiness (DB + Redis).
- `GET /metrics` — Prometheus.
- `GET /admin`, `GET /docs` — dashboard & OpenAPI UI.
- `GET /api/v1/admin/plugins` — list plugins + status.
- `POST /api/v1/admin/plugins/{name}/toggle` — activate/deactivate a plugin.

## Runtime activation
`admin.StateManager` (`sdk/engine/admin/admin.go`) is the single source of truth
for activation. Toggling a plugin is enforced engine-wide: its HTTP routes return
503, its event subscriptions are suppressed, and its owned WS message types are
skipped. Plugins expose title/icon/category to the dashboard via
`admin.Describor` (`AdminInfo()`). The `core` plugin cannot be disabled.
