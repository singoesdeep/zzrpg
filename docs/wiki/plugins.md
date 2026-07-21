<!-- sha: a3f8b3e8d9c3cff7aba9256eda5f5c991eda1007 -->
# 🧩 Plugin Subsystem

Every feature is a plugin implementing `plugin.Plugin`
(`sdk/engine/plugin/plugin.go`): `Meta` (name + requires), `Init`, `Start`, `Stop`.
Composition adapters live in `backend/plugins/`; each wires a game domain
(`backend/game/`) and platform infra (`backend/platform/`) into the kernel.

## What a plugin can own (without touching engine/platform)
- **Schema** — implement `plugin.Migrator` → `MigrationSource{Module,FS,Dir}`;
  migrations are namespaced per module in `schema_migrations`. Example:
  `backend/plugins/idlekit/migrations`, `backend/plugins/buildings/migrations`.
- **Content types** — `registry.DefineContent[T]` + `LoadContent` + `Content[T]`.
- **Routes** — `ic.Mux()` (gated on activation); **WS** —
  `socket.MessageRouter.HandleOwned(type, owner, h)`.
- **Events** — `ic.Bus().Subscribe/Publish`.
- **Admin view** — implement `admin.Describor`.

## Registered plugins (RPG server)
`core` · `stat` (optional zzstat) · `auth` · `items` · `character` · `inventory`
· `loot` · `quests` · `combat` · `idle`. The city game registers only
`core` + `city`.

## Domain-agnostic core
`backend/plugins/core` imports **zero** game domains: event decoders are
registered by each domain plugin, the disconnect→logout event is wired by the
character plugin, and the WS authenticator is provided by the auth plugin.
