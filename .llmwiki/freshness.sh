#!/usr/bin/env bash
# LLM Wiki Freshness Checker
#
# Compares the commit SHA embedded in docs/wiki/*.md against the reference
# commit: the most recent commit that modified the documented sources
# (CODE_PATH). Docs-only commits (e.g. the freshness restamp itself) do not
# advance the reference, so the wiki stays FRESH and the sync converges instead
# of drifting one commit behind after every commit.

set -e
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WIKI_DIR="${ROOT_DIR}/docs/wiki"

# The tree the wiki documents. The reference SHA is the latest commit touching
# it; commits that change only docs/wiki or tooling do not count.
CODE_PATH="backend"

ref_sha() {
  local sha
  sha="$(git -C "${ROOT_DIR}" log -1 --format=%H -- "${CODE_PATH}" 2>/dev/null || true)"
  [ -z "${sha}" ] && sha="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  echo "${sha}"
}

REF_SHA="$(ref_sha)"

echo "=== LLM Wiki Freshness Audit ==="
echo "Reference (last ${CODE_PATH} commit): ${REF_SHA}"
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
    if [ "${file_sha}" = "${REF_SHA}" ]; then
      echo "  ✅ ${filename} — FRESH (SHA: ${file_sha:0:7})"
      FRESH_COUNT=$((FRESH_COUNT + 1))
    else
      # Count only code commits between the stamped SHA and the reference.
      commit_diff_count=$(git -C "${ROOT_DIR}" rev-list --count "${file_sha}..${REF_SHA}" -- "${CODE_PATH}" 2>/dev/null || echo "N/A")
      echo "  ⚠️  ${filename} — STALE (Wiki SHA: ${file_sha:0:7}, ${commit_diff_count} code commits behind)"
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
  echo "Tip: Run '.llmwiki/ingest.sh' to re-sync stale wiki pages, then commit the restamp."
fi
