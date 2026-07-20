#!/usr/bin/env bash
# Install script for LLM Wiki git post-commit hook

set -e
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOK_DEST="${ROOT_DIR}/.git/hooks/post-commit"
HOOK_SRC="${ROOT_DIR}/.githooks/post-commit"

echo "Installing LLM Wiki post-commit hook..."
chmod +x "${HOOK_SRC}"
cp "${HOOK_SRC}" "${HOOK_DEST}"
chmod +x "${HOOK_DEST}"

echo "✅ LLM Wiki git post-commit hook successfully installed at .git/hooks/post-commit"
