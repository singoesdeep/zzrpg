#!/usr/bin/env bash
# LLM Wiki Ingest & Sync Script
#
# Stamps wiki pages with the reference SHA: the most recent commit that modified
# the documented sources (CODE_PATH). It is idempotent — when the pages already
# carry the reference SHA it rewrites identical bytes, so the working tree stays
# clean. Because docs-only commits (like the restamp this produces) do not
# advance the reference, running from a post-commit hook converges in a single
# restamp commit instead of looping forever.

set -e
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WIKI_DIR="${ROOT_DIR}/docs/wiki"

# The tree the wiki documents; must match CODE_PATH in freshness.sh.
CODE_PATH="backend"

REF_SHA="$(git -C "${ROOT_DIR}" log -1 --format=%H -- "${CODE_PATH}" 2>/dev/null || true)"
[ -z "${REF_SHA}" ] && REF_SHA="$(git -C "${ROOT_DIR}" rev-parse HEAD)"

mkdir -p "${WIKI_DIR}"

echo "=== LLM Wiki Ingestion Triggered ==="
echo "Stamping wiki to last ${CODE_PATH} commit ${REF_SHA:0:7}..."

# Update embedded SHA tags in all wiki markdown files (idempotent).
for file in "${WIKI_DIR}"/*.md; do
  [ -e "$file" ] || continue
  if grep -q "<!-- sha:" "$file"; then
    sed -i -E "s/<!-- sha: [^ ]+ -->/<!-- sha: ${REF_SHA} -->/" "$file"
  fi
done

echo "Refreshing Wiki Index (index.md)..."
cat <<EOF > "${WIKI_DIR}/index.md"
<!-- sha: ${REF_SHA} -->
# zzrpg Codebase Living LLM Wiki

Welcome to the **zzrpg Engine Living Wiki**, automatically maintained and grounded in the source code via Karpathy's LLM Wiki pattern.

## 📚 Wiki Knowledge Base

| Topic / Category | Summary | Primary Code References | Last Synced SHA |
|---|---|---|---|
| 🏛️ [Architecture](architecture.md) | Four-layer structure (engine/platform/game/plugins), game-agnostic kernel, DI registry, typed event bus, hooks, & Redis Streams fanout | \`backend/engine/\`, \`backend/platform/\` | \`${REF_SHA:0:7}\` |
| 🧩 [Plugin Subsystem](plugins.md) | Composition adapters, \`admin.Describor\` UI views, engine-gated runtime activation (\`admin.StateManager\`) | \`backend/plugins/\`, \`backend/game/\` | \`${REF_SHA:0:7}\` |
| ⚔️ [Combat & Stat Core](combat-engine.md) | Combat damage math, creature resolvers, & embedded Rust \`zzstat\` FFI | \`backend/plugins/combat/\`, \`backend/platform/statclient/\` | \`${REF_SHA:0:7}\` |
| 💾 [Database & Outbox](database-outbox.md) | Store/UnitOfWork seam, PostgreSQL schema, outbox relay, & event_log replay | \`backend/engine/store/\`, \`backend/engine/outbox/\` | \`${REF_SHA:0:7}\` |
| 🎛️ [Admin Dashboard & APIs](admin-dashboard.md) | Web Admin UI, REST endpoints, WebSocket protocol, & Scalar docs | \`backend/plugins/core/api/admin.html\` | \`${REF_SHA:0:7}\` |

---

## 🔍 How to Use & Audit
- Check freshness anytime: \`.llmwiki/freshness.sh\`
- Automatic post-commit sync: \`scripts/install-llmwiki-hook.sh\`
- All wiki pages reference exact source lines using GitHub \`file://\` links.
EOF

echo "=== LLM Wiki Ingestion Complete (stamped ${REF_SHA:0:7}) ==="
