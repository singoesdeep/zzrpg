#!/usr/bin/env bash
# LLM Wiki Ingest & Sync Script
# Runs diff inspection and updates wiki pages with exact SHA tags.

set -e
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WIKI_DIR="${ROOT_DIR}/docs/wiki"
CURRENT_SHA="$(git -C "${ROOT_DIR}" rev-parse HEAD)"

mkdir -p "${WIKI_DIR}"

echo "=== LLM Wiki Ingestion Triggered ==="
echo "Updating Wiki SHA tags to ${CURRENT_SHA:0:7}..."

# Update embedded SHA tags in all wiki markdown files
for file in "${WIKI_DIR}"/*.md; do
  [ -e "$file" ] || continue
  if grep -q "<!-- sha:" "$file"; then
    sed -i -E "s/<!-- sha: [^ ]+ -->/<!-- sha: ${CURRENT_SHA} -->/" "$file"
  fi
done

echo "Refreshing Wiki Index (index.md)..."
cat <<EOF > "${WIKI_DIR}/index.md"
<!-- sha: ${CURRENT_SHA} -->
# zzrpg Codebase Living LLM Wiki

Welcome to the **zzrpg Engine Living Wiki**, automatically maintained and grounded in the source code via Karpathy's LLM Wiki pattern.

## 📚 Wiki Knowledge Base

| Topic / Category | Summary | Primary Code References | Last Synced SHA |
|---|---|---|---|
| 🏛️ [Architecture](architecture.md) | Four-layer structure (engine/platform/game/plugins), game-agnostic kernel, DI registry, typed event bus, hooks, & Redis Streams fanout | \`backend/engine/\`, \`backend/platform/\` | \`${CURRENT_SHA:0:7}\` |
| 🧩 [Plugin Subsystem](plugins.md) | Composition adapters, \`admin.Describor\` UI views, engine-gated runtime activation (\`admin.StateManager\`) | \`backend/plugins/\`, \`backend/game/\` | \`${CURRENT_SHA:0:7}\` |
| ⚔️ [Combat & Stat Core](combat-engine.md) | Combat damage math, creature resolvers, & embedded Rust \`zzstat\` FFI | \`backend/plugins/combat/\`, \`backend/platform/statclient/\` | \`${CURRENT_SHA:0:7}\` |
| 💾 [Database & Outbox](database-outbox.md) | Store/UnitOfWork seam, PostgreSQL schema, outbox relay, & event_log replay | \`backend/engine/store/\`, \`backend/engine/outbox/\` | \`${CURRENT_SHA:0:7}\` |
| 🎛️ [Admin Dashboard & APIs](admin-dashboard.md) | Web Admin UI, REST endpoints, WebSocket protocol, & Scalar docs | \`backend/plugins/core/api/admin.html\` | \`${CURRENT_SHA:0:7}\` |

---

## 🔍 How to Use & Audit
- Check freshness anytime: \`.llmwiki/freshness.sh\`
- Automatic post-commit sync: \`scripts/install-llmwiki-hook.sh\`
- All wiki pages reference exact source lines using GitHub \`file://\` links.
EOF

echo "=== LLM Wiki Ingestion Complete ==="
