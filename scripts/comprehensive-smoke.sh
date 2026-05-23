#!/usr/bin/env bash
# comprehensive-smoke.sh — wrapper around comprehensive-smoke.ts.
#
# Same shape as postdeploy-ui-smoke.sh — resolves the TS file and runs
# node from frontend/ so the `playwright` import resolves. See the TS
# header for env vars + safety guarantees.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TS_FILE="$REPO_ROOT/scripts/comprehensive-smoke.ts"
FRONTEND_DIR="$REPO_ROOT/frontend"

if [[ ! -f "$TS_FILE" ]]; then
    printf 'missing %s\n' "$TS_FILE" >&2
    exit 2
fi
if [[ ! -d "$FRONTEND_DIR/node_modules/playwright" ]]; then
    printf 'playwright not installed — run: pnpm -C frontend install\n' >&2
    printf 'and: pnpm -C frontend exec playwright install chromium\n' >&2
    exit 2
fi

node_major="$(node -p 'process.versions.node.split(".")[0]')"
if [[ "$node_major" -lt 24 ]]; then
    printf 'requires Node 24+ (have %s) — native .ts execution lands at 24\n' "$node_major" >&2
    exit 2
fi

cd "$FRONTEND_DIR"
exec node "$TS_FILE" "$@"
