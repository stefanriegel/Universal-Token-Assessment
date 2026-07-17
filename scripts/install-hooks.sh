#!/usr/bin/env bash
# Wire the project's shared git hooks (in .githooks/) into the local clone.
# Run once after cloning. Re-run is safe (idempotent).

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if [ ! -d .githooks ]; then
  echo "No .githooks/ directory at repo root; nothing to install." >&2
  exit 1
fi

git config core.hooksPath .githooks

# Make sure scripts in .githooks/ are executable.
find .githooks -type f -not -name '*.txt' -not -name '*.md' -exec chmod +x {} +

echo "Hooks installed (core.hooksPath = .githooks)."
echo "Active hooks:"
ls .githooks | grep -v -E '\.(txt|md)$' | sed 's/^/  /'
