#!/usr/bin/env bash
# postdeploy-ui-smoke.sh — thin wrapper around postdeploy-ui-smoke.ts.
#
# The TS file imports `playwright`, which lives in frontend/node_modules.
# Node's module resolution walks up from the script file, so running
# from /repo/scripts/ would fail. This wrapper resolves the script
# path absolutely and runs node with `frontend/` as the CWD so the
# import resolves correctly. The script itself is filesystem-only —
# its CWD doesn't matter beyond `node_modules` lookup.
#
# Usage / env vars: see postdeploy-ui-smoke.ts header comment.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TS_FILE="$REPO_ROOT/scripts/postdeploy-ui-smoke.ts"
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

# Node 24+ ships native TS stripping via amaro. Check the version
# so the failure mode on older Node is a clear message, not a
# cryptic syntax error.
node_major="$(node -p 'process.versions.node.split(".")[0]')"
if [[ "$node_major" -lt 24 ]]; then
    printf 'requires Node 24+ (have %s) — native .ts execution lands at 24\n' "$node_major" >&2
    exit 2
fi

cd "$FRONTEND_DIR"
exec node "$TS_FILE" "$@"
