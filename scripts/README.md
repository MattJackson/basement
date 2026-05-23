# scripts/

Operational scripts for basement. None of these are required for
day-to-day development — they're for deployment validation, debugging,
and CI.

| Script | What it covers |
|---|---|
| [`postdeploy-smoke.sh`](#postdeploy-smokesh) | API-level smoke: timing budgets, auth, bucket lifecycle, validation gates, cache headers |
| [`postdeploy-ui-smoke.sh`](#postdeploy-ui-smokesh) | UI-level smoke: route/navigation, render assertions, console errors via headless Chromium |
| [`comprehensive-smoke.sh`](#comprehensive-smokesh) | Full-coverage UI smoke: every route, desktop+mobile, form validation, modal walks, ephemeral-only destructive ops |

Run both after every deploy. The API smoke is faster (~10s); the UI
smoke takes ~10-20s on a healthy deploy and exercises the bug class
the API smoke can't see (broken routes, missing renders, JS errors).

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

---

## `postdeploy-ui-smoke.sh`

A black-box **UI** smoke test that drives a real headless Chromium
through the SPA. Sibling of `postdeploy-smoke.sh` (API-level) — this
one catches the bug class that the API smoke can't see:

- route configuration / parent-layout bugs (the v0.3.1 regression
  where `/admin/clusters/$cid` rendered as a layout without `<Outlet />`,
  so clicking a bucket/key Link redirected back to cluster detail
  instead of opening the detail page)
- missing-on-render bugs (counts, badges, section headers)
- silent JS errors / unhandled promise rejections

The shell file is a thin wrapper around
[`postdeploy-ui-smoke.ts`](postdeploy-ui-smoke.ts); see the TS file
header for implementation notes.

### What it checks

In order, with screenshots of each major screen saved under
`/tmp/basement-smoke/<timestamp>/`:

1. **Login flow** — `GET /` redirects to `/admin/login`, submitting
   credentials lands on `/admin`.
2. **Clusters list** — `/admin/clusters` renders at least one row
   with a non-empty label, a driver badge, and a Resources cell
   that surfaces buckets + keys counts (the v0.3.1 `ClusterCounts`
   component).
3. **Cluster detail navigation** (v0.3.1 regression test) — clicking
   the first cluster row lands on `/admin/clusters/{cid}` and shows
   Buckets + Keys section headers.
4. **Bucket detail navigation** (v0.3.1 regression test) — clicking
   the first bucket `Link` in the cluster-detail Buckets section
   lands on `/admin/clusters/{cid}/buckets/{id}` and renders bucket
   detail content (not the cluster-detail page).
5. **Key detail navigation** (v0.3.1 regression test) — same shape
   for `/admin/clusters/{cid}/keys/{id}`.
6. **Layout editor** — `/admin/clusters/{cid}/layout` renders the
   `Layout · {label}` header (Garage) or the "Layout not supported"
   card (aws-s3).
7. **Aggregated buckets** — `/admin/buckets` redirects to `/admin`
   and renders the "My Buckets" page with rows (or the empty state).
8. **Aggregated keys** — `/admin/keys` renders the "Access keys" page
   with rows (or the empty state).
9. **Console + pageerror gate** — fails if any `console.error` or
   `pageerror` fired across the whole run. Warnings are surfaced
   inline but don't fail the run.

Each check prints a `[ok]` / `[FAIL]` / `[skip]` / `[warn]` line in
the same tone as the bash smoke. Exit `0` on all green, `1` on any
failure, `2` on bad setup (missing dep, missing browser binary).

### Usage

Defaults to `https://basement.pq.io` with `matthew/password`:

```bash
./scripts/postdeploy-ui-smoke.sh
```

Override the target or credentials with env vars:

```bash
BASE_URL=https://basement.example.com \
USERNAME=alice PASSWORD=hunter2 \
  ./scripts/postdeploy-ui-smoke.sh
```

### Requirements

- Node **24+** (the TS file is executed natively via amaro's TS
  stripper — no transpile step)
- `playwright` installed under `frontend/node_modules` (a devDep);
  the wrapper checks for this and prints the install command if
  missing
- Chromium browser binary, installed once via
  `pnpm -C frontend exec playwright install chromium`

### Decisions

- **Library, not test framework.** Uses `playwright` directly rather
  than `@playwright/test`. The latter would bring a config file and
  HTML reporter; for a smoke that lives next to a bash sibling, the
  procedural drive-the-browser style matches the existing tone
  better and avoids the test-runner overhead.
- **Dep location.** Playwright is installed in `frontend/package.json`
  rather than a separate `scripts/package.json`. There's already one
  manifest in the workspace; adding a second just for this script
  would be friction without payoff. The wrapper script handles the
  fact that the TS file lives in `scripts/` but its dep lives one
  level up.
- **Screenshots, not video.** Each major screen takes a full-page
  screenshot before the next assertion. Cheap, useful for debugging
  a failure, and doesn't require a video codec.

### Screenshots

Each major screen is captured to
`/tmp/basement-smoke/<ISO-timestamp>/NN-name.png`. Useful for
diagnosing a failure visually after the fact, or for grabbing a
"what the deploy looked like" snapshot. The directory is left in
place after the run so you can scroll through it; clean it up with
`rm -rf /tmp/basement-smoke/` periodically.

---

## `comprehensive-smoke.sh`

Full systematic walk of every route in `frontend/src/routes/`, at
both desktop (1280x900) and mobile (375x667) viewports, with form
validation walks and modal coverage. Destructive coverage uses
**ephemeral-only** resources tagged `smoke-{timestamp}-{nonce}-*` —
real data (matthew's `lsi`/`cheshire`, real federations, real OIDC
identities) is never touched.

Where the curated `postdeploy-ui-smoke.sh` is a regression-focused
spot-check (~70 checks, one viewport, no mutations), this is the
exhaustive complement (~200+ checks, both viewports, ephemeral CRUD).

### Safety guarantees

- **No real-data mutation.** Every server-side mutation uses a name
  starting with `smoke-{Date.now()}-{nonce}` so even a cleanup
  failure leaves obvious leftover that's easy to reap.
- **Baseline count check.** Real-resource counts (regions, SAs,
  webhooks, backups, federations) are captured before any mutation
  and compared at end-of-run. A mismatch is a loud failure.
- **Cleanup runs in `finally`.** Even if a check throws, the
  ephemeral reaper runs and tries to delete every tracked resource.
  Failures are logged with IDs so an operator can scrub manually.

### Coverage

- Public routes (`/`, `/login`, `/admin/login`, `/share/$token` with
  a bogus token)
- All `/files/*` user routes (home, keys, shares, syncs, backups,
  federated-buckets, webhooks, region/bucket/object pages)
- All `/admin/*` routes (system, users, clusters, policies, keys,
  audit, usage, service-accounts, migrate)
- Form validation paths (blank submit, invalid data, valid data)
- Modal walks (create-key, create-SA, federation wizard, backup
  wizard, webhook create, elevation password modal, delete confirms)
- Auth-state coverage (USER mode, ADMIN mode after elevation,
  drop-to-user navigation)
- WebDAV probe (OPTIONS for DAV headers)
- PWA probe (manifest + service worker)
- Mobile viewport re-run of read-only routes (touch targets,
  horizontal nav scroll, card layouts)

### Usage

```bash
./scripts/comprehensive-smoke.sh
# or
pnpm -C frontend run smoke:full
```

Output:

- `/tmp/basement-smoke-<timestamp>/desktop/` — desktop screenshots
- `/tmp/basement-smoke-<timestamp>/mobile/` — mobile screenshots
- stdout: per-check pass/fail + final summary + bug report

Exit code: `0` all checks pass, `1` any failure (cleanup still ran),
`2` setup error.
