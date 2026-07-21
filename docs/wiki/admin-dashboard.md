<!-- sha: c2efcc086822b24518cb81b047402965170b6c17 -->
# 🎛️ Admin Dashboard & APIs

The `core` plugin serves a single-page Admin Dashboard at `GET /admin`
(`backend/plugins/core/api/admin.html`) and the operational endpoints.

## Endpoints
- `GET /health`, `GET /readyz` — liveness / readiness (DB + Redis).
- `GET /metrics` — Prometheus.
- `GET /admin`, `GET /docs` — dashboard shell & OpenAPI UI (unauthenticated —
  no sensitive data, just the static page).
- `GET /api/v1/admin/plugins` — list plugins + status. **Requires the
  `X-Admin-Bypass-Key` header.**
- `POST /api/v1/admin/plugins/{name}/toggle` — activate/deactivate a plugin.
  **Requires the header.**
- `GET /api/v1/admin/health` — DB/Redis status for the dashboard's header
  strip. **Requires the header.**

## Auth: `ADMIN_BYPASS_KEY`
The dashboard's data endpoints (not the static HTML shell) are gated by a
single operator secret, `ADMIN_BYPASS_KEY`, read from the environment at boot
(`sdk/pkg/config`). It is a deploy-time credential, not a per-user login — the
dashboard prompts for it once and sends it as `X-Admin-Bypass-Key` on every
call (`admin.RequireBypassKey`, `sdk/engine/admin/admin.go`, constant-time
compared). Unset in production is allowed (dashboard data endpoints stay
disabled, 503); if set, production requires ≥20 characters. Development falls
back to a fixed insecure default so the dashboard works locally with zero
config.

## Runtime activation
`admin.StateManager` (`sdk/engine/admin/admin.go`) is the single source of truth
for activation. Toggling a plugin is enforced engine-wide: its HTTP routes return
503, its event subscriptions are suppressed, and its owned WS message types are
skipped. Plugins expose title/icon/category to the dashboard via
`admin.Describor` (`AdminInfo()`). The `core` plugin cannot be disabled.
