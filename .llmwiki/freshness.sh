#!/usr/bin/env bash
# LLM Wiki Freshness Checker
# Compares commit SHAs embedded in docs/wiki/*.md against codebase HEAD.

set -e
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WIKI_DIR="${ROOT_DIR}/docs/wiki"
CURRENT_SHA="$(git -C "${ROOT_DIR}" rev-parse HEAD)"

echo "=== LLM Wiki Freshness Audit ==="
echo "Current Codebase HEAD: ${CURRENT_SHA}"
echo "-----------------------------------"

if [ ! -d "${WIKI_DIR}" ]; then
  echo "Wiki directory not found at ${WIKI_DIR}"
  exit 1
fi

STALE_COUNT=0
FRESH_COUNT=0
NO_SHA_COUNT=0

for file in "${WIKI_DIR}"/*.md; do
  [ -e "$file" ] || continue
  filename="$(basename "$file")"
  
  if grep -q "<!-- sha:" "$file"; then
    file_sha=$(grep "<!-- sha:" "$file" | head -n 1 | sed -E 's/.*<!-- sha: ([a-f0-9]+) -->.*/\1/')
    if [ "${file_sha}" = "${CURRENT_SHA}" ]; then
      echo "  ✅ ${filename} — FRESH (SHA: ${file_sha:0:7})"
      FRESH_COUNT=$((FRESH_COUNT + 1))
    else
      commit_diff_count=$(git -C "${ROOT_DIR}" rev-list --count "${file_sha}..HEAD" 2>/dev/null || echo "N/A")
      echo "  ⚠️  ${filename} — STALE (Wiki SHA: ${file_sha:0:7}, ${commit_diff_count} commits behind HEAD)"
      STALE_COUNT=$((STALE_COUNT + 1))
    fi
  else
    echo "  ❓ ${filename} — UNTRACKED (No SHA tag found)"
    NO_SHA_COUNT=$((NO_SHA_COUNT + 1))
  fi
done

echo "-----------------------------------"
echo "Summary: ${FRESH_COUNT} fresh, ${STALE_COUNT} stale, ${NO_SHA_COUNT} untracked."
if [ ${STALE_COUNT} -gt 0 ]; then
  echo "Tip: Run '.llmwiki/ingest.sh' to re-sync stale wiki pages with recent commits."
fi
