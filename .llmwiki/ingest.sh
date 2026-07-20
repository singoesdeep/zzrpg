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
# zzrpg Living Wiki

Code-grounded reference for the zzrpg engine and the games built on it. The repo
is two Go modules: **\`sdk/\`** (the game-agnostic engine + utilities) and
**\`backend/\`** (game domains, platform infra, plugins, and the runnable games).
For prose guides see [../ARCHITECTURE.md](../ARCHITECTURE.md) and
[../PLUGIN_GUIDE.md](../PLUGIN_GUIDE.md).

## 📚 Knowledge Base

| Topic | Summary | Primary Code | SHA |
|---|---|---|---|
| 🏛️ [Engine Core & Kernel](architecture.md) | Game-agnostic kernel, plugin lifecycle, DI registry, event bus, hooks, idle framework, activation gate | \`sdk/engine/\`, \`sdk/pkg/\` | \`${REF_SHA:0:7}\` |
| 🧩 [Plugin Subsystem](plugins.md) | Plugin contract, per-plugin schema/content/routes/events, domain-agnostic core | \`backend/plugins/\`, \`backend/game/\` | \`${REF_SHA:0:7}\` |
| ⚔️ [Combat, Stats & Idle](combat-engine.md) | Optional zzstat plugin, combat flow, content-driven idle (stages/lifeskills/generators) | \`backend/plugins/{stat,combat,idle}/\` | \`${REF_SHA:0:7}\` |
| 💾 [Persistence & Outbox](database-outbox.md) | Store seam, transactional outbox, per-module migrations | \`sdk/engine/{store,outbox}/\`, \`backend/platform/database/\` | \`${REF_SHA:0:7}\` |
| 🎛️ [Admin Dashboard & APIs](admin-dashboard.md) | Web admin UI, operational endpoints, runtime plugin activation | \`backend/plugins/core/\` | \`${REF_SHA:0:7}\` |

---

## 🔍 Freshness
- Check anytime: \`.llmwiki/freshness.sh\` (stamps track the last \`backend/\` commit).
- Auto-sync on commit: \`scripts/install-llmwiki-hook.sh\`.
EOF

echo "=== LLM Wiki Ingestion Complete (stamped ${REF_SHA:0:7}) ==="
