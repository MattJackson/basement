# scripts/

Operational scripts for basement. None of these are required for
day-to-day development — they're for deployment validation, debugging,
and CI.

## `postdeploy-smoke.sh`

A black-box smoke test that exercises a **running basement server**
through its public HTTP API. Run it immediately after a deploy to verify
the build is healthy and none of the historically-fragile timing budgets
have regressed.

### What it checks

In order, stopping on the first failure:

1. **`GET /api/v1/version`** — server is reachable, returns
   `{version, commit, builtAt}`. Budget: 2s.
2. **Auth round-trip** — `POST /api/v1/auth/login` sets the
   `__Host-basement_session` cookie, `GET /api/v1/auth/me` echoes back
   the admin user. Budget: 3s combined.
3. **`GET /api/v1/admin/clusters`** — flat read of `connections.json`,
   must be near-instant. Budget: **<1s** (a regression here means
   something is blocking the handler).
4. **`POST /api/v1/admin/clusters/{cid}/_test`** — health-check on the
   first configured cluster. Budget: **≤10s** (matches the Garage v1
   client timeout). `ok:false` is acceptable; what we're testing is
   that the timeout fires within budget.
5. **`GET /api/v1/admin/buckets`** and **`GET /api/v1/admin/keys`** —
   cross-cluster aggregated reads. Budget: **≤4s each** (3s per-cluster
   deadline + overhead, even with one stalled cluster).
6. **Bucket lifecycle** on a healthy cluster — create → get → arm-delete
   → delete (with `X-Confirm-Delete` token) → verify 404. Uses
   `smoke-life-<timestamp>-<pid>` as the alias so leftover resources are
   obvious if cleanup fails.
7. **Validation gates** — empty alias returns 400 `ALIAS_REQUIRED`,
   duplicate alias returns 409 `DUPLICATE_ALIAS`, DELETE without
   `X-Confirm-Delete` returns 400 `CONFIRMATION_REQUIRED`.
8. **Static-asset cache headers** (regression guard for the 2026-05-19
   favicon-cache incident) — `/favicon.svg` is
   `public, max-age=3600, must-revalidate`; a hashed `/assets/*` bundle
   pulled from the index HTML is `public, max-age=31536000, immutable`.

Every check has a timing budget. Exceeding the budget is a failure with
a message like:

```
✗ GET /api/v1/admin/clusters exceeded timing budget
  expected ≤1.0s, took 3.847s — should be a flat connections.json read
  — something is blocking the handler
```

### Usage

Defaults to `https://basement.pq.io` with `matthew/password`:

```bash
./scripts/postdeploy-smoke.sh
```

Override the target or credentials with env vars:

```bash
BASEMENT_URL=https://basement.example.com \
BASEMENT_USER=alice BASEMENT_PASS=hunter2 \
  ./scripts/postdeploy-smoke.sh
```

Flags:

- `-v` / `--verbose` — verbose curl output (every request/response)
- `--no-color` — disable ANSI color (use this in CI logs)

Exit codes: `0` all checks passed, `1` a check failed, `2` bad
invocation (missing dependency, bad flag).

### Requirements

- `bash` 4+ (uses arrays, `[[ ]]`, `local`)
- `curl`
- `jq` (parsing JSON responses)
- `awk` (float comparison for timing assertions)

### Cleanup behavior

The script creates test resources with the alias prefix `smoke-` so
leftovers are easy to spot. An `EXIT` trap attempts to delete any test
buckets that weren't already cleaned up, even on failure or
`Ctrl-C`. If cleanup itself fails (network blip, expired session),
you'll see a `smoke-<...>` bucket in the cluster — safe to delete by
hand; nothing depends on it.

### Note on naming

The brief originally specified `_smoke_` as the resource prefix, but
the bucket-alias validator (`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
rejects underscores. The script uses `smoke-` instead. Test resource
names still embed `$(date +%s)-$$` so collisions across parallel runs
or stale leftovers are immediately visible.
