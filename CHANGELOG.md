# Changelog

All notable changes to basement are recorded here. See the linked
release-notes files in `docs/release-notes/` for the full per-release
write-up; this file is the at-a-glance index.

## v1.5.0a — 2026-05-22

Backup story, cycle 1: scheduled, named bucket-to-bucket backup with
a cron engine layered over the existing v0.8.x sync engine. New
`internal/backup` package — `backup.Backups` store (atomic JSON
under `{dataDir}/backups.json`), `backup.Scheduler` wrapping
`github.com/robfig/cron/v3` with panic-recovery per job. Six new
user-tier endpoints under `/api/v1/user/backups` (CRUD + `/run`),
gated on the same USER auth as `/user/syncs` with 404-on-not-owner
to avoid existence leaks. New routes `/files/backups` (list),
`/files/backups/new` (4-step wizard: source / destination /
schedule / name+review), `/files/backups/$id` (detail with last-10
run history, edit-schedule inline, enable+disable, run-now button).
Schedule UI: manual / daily HH:MM / weekly day+HH:MM / monthly
day-of-month+HH:MM / custom cron. Backup runs go through the
existing sync engine via a runner closure (no duplication of pull
semantics). Tag + main land at the same commit; smoke extended
with `/files/backups` + wizard step-1 checks.

## v1.4.0 — 2026-05-22

Scale + perf milestone. Three cycles (v1.4.0a → v1.4.0c) sharpen
basement for clusters with real volume — flat directories with
thousands of objects, key permissions across hundreds of buckets,
storage growth that needs surfacing before it surprises the operator,
and Garage block-scrub maintenance with a UI instead of a CLI:
virtualized bucket browser (`@tanstack/react-virtual`), per-file
batch select with sticky action bar, paginated + filterable key
permissions editor with a sticky Save bar, paginated audit log
with Export CSV, growth-rate column + top-growing-buckets panel +
anomaly banner on `/admin/usage` with a 7d / 30d / 90d range
selector, and `/admin/clusters/{cid}/scrub` for Garage block-scrub
state + kickoff. Driver interface gains `PerBucketStatsAvailable()`,
`ScrubSupport()`, `ScrubState()`, `StartScrub()` — all four in-tree
drivers implement, Garage gets the real work, AWS / MinIO advertise
unsupported with operator-facing reason text. No data migrations;
`/api/v1/user/regions/{rid}/buckets` envelope tweak is back-compat
read on the FE. Smoke 42/42 pass against live; 23 routes screenshot-
verified with zero console errors. Block-scrub status against
legacy Garage builds without `/v1/worker` renders the error banner
gracefully — v1.5 will add a feature-detection fallback so the Run
scrub button hides instead.

Full notes: [`docs/release-notes/v1.4.0.md`](docs/release-notes/v1.4.0.md)

### Cycles

- **v1.4.0a** — Virtualized bucket browser
  (`@tanstack/react-virtual`, fixed 48px rows, infinite scroll on
  the continuation token), `Driver.PerBucketStatsAvailable()`
  capability gate (Garage v1 hides Size/Objects columns at the
  user-region tier instead of rendering em-dashes), `/admin/audit`
  pagination (50/page Prev/Next + "Page N of M" + Export CSV).
- **v1.4.0b** — Paginated key permissions editor at
  `/admin/clusters/{cid}/keys/{kid}` Edit mode — hydrates the FULL
  cluster bucket list, filter input, 50-per-page pagination, "Show
  only granted" toggle, sticky Save bar. Batch object operations in
  the bucket browser — per-file checkboxes, select-all-visible
  header checkbox, sticky bottom action bar, delete fans out via
  `Promise.allSettled` with per-row error indicators on partial
  failure.
- **v1.4.0c** — Garage block-scrub UI at
  `/admin/clusters/{cid}/scrub` (Running/Idle badge, progress %,
  blocks scanned/corrupt, last-completed timestamp, free-form
  driver message, Run scrub button + 5s/30s polling); `ScrubSupport`
  / `ScrubState` / `StartScrub` on the driver interface — Garage v1
  + v2 implement against the admin worker endpoints, AWS S3 + MinIO
  advertise unsupported. Storage analytics on `/admin/usage` —
  Growth (Nd) column on the per-cluster table, "Buckets growing
  fastest" panel, amber anomaly banner for >100% growth, 7d / 30d /
  90d range selector feeding the inline trend charts.

## v1.4.0b — 2026-05-22

Scale + perf cycle 2 of v1.4. Two surfaces that broke down at
thousand-bucket / thousand-object scale get pagination + selection:

* **Paginated key permissions screen.** `/admin/clusters/{cid}/keys/{kid}`
  Edit mode now hydrates the FULL cluster bucket list (granted +
  ungranted) instead of just `key.buckets`, so the operator can grant
  access to new buckets without bouncing through the "+ Grant access"
  dialog. Filter input (`Filter buckets...`) narrows by alias substring
  client-side. Pagination at 50 buckets per page with Previous/Next +
  "Showing X-Y of Z (page N of M)" indicator. "Show only granted"
  toggle (default off — shows ALL, on — hides ungranted rows). Sticky
  Save bar pins to the bottom of the card so the operator never has to
  scroll a long list to commit. Checkbox state survives pagination —
  the edit array is mutated in-place, not rebuilt per page.

* **Batch object operations in the bucket browser.** Per-file checkbox
  column added at the left edge of every file row (folder rows are
  excluded — recursive deletes need explicit confirmation per folder).
  "Select all visible" checkbox in the table header with an
  indeterminate state when some-but-not-all visible files are
  selected. When ≥1 object is selected a sticky bottom action bar
  appears with "N selected | Delete N objects | Cancel". Delete fires
  parallel DELETE requests via `Promise.allSettled` — partial failure
  surfaces a per-row error indicator (destructive icon + "delete
  failed" label + title attribute carrying the backend's error
  message) and leaves the survivors selected for retry. Move/copy
  punted to v1.5 (needs server-side copy + delete pattern). Row
  height held at 48px so virtualization perf from v1.4.0a stays
  intact.

Tests: 4 new for key perms editor (filter narrows, pagination,
only-granted, state survives pagination), 4 new for batch ops
(select-all, delete fans out N requests, partial failure surfaces
per-row errors, cancel clears). 218/218 green. Smoke gains two
checks: key edit-mode mounts the new filter + sticky-save, bucket
browser mounts the select-all-visible checkbox.

## v1.4.0a — 2026-05-22

Scale + perf cycle 1 of v1.4. Bucket browser virtualized via
`@tanstack/react-virtual` — a flat directory with 10K+ rows scrolls
smoothly at fixed-row-height (48px); folders sort to the top, files
in S3 order, sticky header, scroll resets on prefix change. Infinite
scroll auto-fetches the next continuation token when the user nears
the bottom of a truncated page. New `Driver.PerBucketStatsAvailable()`
capability flag gates the Size + Objects columns on the per-region
bucket list — Garage v1 returns false (no public stats at the
user-region tier) and the columns hide rather than render rows of
em-dashes; AWS S3 / MinIO / Garage v2 return true. `/admin/audit`
gains pagination (50/page default, Prev/Next with "showing X-Y of Z"
footer, "Page N of M"), an `offset` query param backed by
`Audit.QueryWithTotal` on the file logger, and a client-side
"Export CSV" button that dumps the currently filtered page.

## v1.3.0 — 2026-05-22

Multi-user onboarding + key-first model refinement + sudo elevation
polish. Six cycles (v1.3.0a → v1.3.0e plus hotfixes) tighten the
v1.2 architecture into a comfortable-with-multiple-humans deploy:
OIDC group-claim auto-mapping to basement roles, driver-aware
endpoint hints in cluster + key forms, per-region S3 addressing
toggle (path-style / virtual-host) with an in-place rotate-key flow,
delimiter-based folder navigation in the bucket browser, persistent
invite tokens with a Pending Invites section on `/admin/users`,
bulk-import of access keys (CSV / TSV / aws-cli credentials profiles)
at `/files/keys/new`, and a per-cluster `cluster_admin` assignment
UI right on the cluster detail page. ADR-0003 simplified to two
modes (USER / ADMIN dropping ELEVATED — TTL is the safety, not a
sub-mode); admin TTL is now operator-configurable at `/admin/system`;
expiry banner replaces the page in-place rather than yanking the
operator out of an in-progress form. Hotfix stack: graceful
backend-revoked-key handling (401 USER_KEY_REJECTED instead of bare
500), region label honored in S3 signing, login lands everyone on
`/files` instead of admin-only auto-redirect to `/admin`,
folder-navigation re-render bug, presign URL double-encoding fix.
No data migrations; `mode="elevated"` cookies silently migrate to
admin. Smoke 36/36 pass against live; 29 routes screenshot-verified.

Full notes: [`docs/release-notes/v1.3.0.md`](docs/release-notes/v1.3.0.md)

### Cycles

- **v1.3.0a** — OIDC group-claim → role auto-mapping. Operator
  configures the mapping at `/admin/system`; matching groups
  auto-assign on every IdP login, stale ones revoke, manual
  assignments never touched.
- **v1.3.0a.1** — Graceful handling of backend-revoked user keys.
  Region endpoints translate S3 auth-rejection codes into 401
  `USER_KEY_REJECTED` with the offending region + alias + endpoint
  + accessKeyId so the FE can render an actionable error.
- **v1.3.0a.2** — Force path-style S3 addressing across every driver
  via shared `driver.NewS3PathStyleClient` helper. Fixes Garage
  ListObjects 404 (request was routing to
  `http://bucket.host:port/` instead of `http://host:port/bucket/`).
- **v1.3.0a.3** — Elevation UX hotfix: wrap destructive admin
  handlers with `useElevationGuard()` + auto-elevate on persona
  switch in the UserMenu.
- **v1.3.0a.4** — ADR-0003 amendment: drop ELEVATED, two-mode auth
  (USER / ADMIN), operator-configurable TTL (60s–24h),
  drop-in-place expiry banner. `ModeElevated` survives as a string
  alias for one cycle; v1.2-era `mode="elevated"` cookies silently
  migrate to ADMIN.
- **v1.3.0b** — Driver-aware endpoint hints in cluster + key forms.
  Public `GET /api/v1/system/driver-defaults` returns the curated
  `EndpointDefaults` catalogue; FE caches forever; "Common
  endpoints" expandable with one-click "Use this" + region
  auto-suggest.
- **v1.3.0c** — Per-region S3 addressing toggle
  (`UserRegion.AddressingStyle`) + `POST
  /api/v1/user/regions/{regionId}/rotate` for in-place key rotation.
  IP-host smart default forces path-style regardless of toggle.
- **v1.3.0c.1** — Folder navigation in the bucket browser via S3
  `delimiter="/"`. `ObjectPage.commonPrefixes` cascades through all
  four drivers; FE renders folder rows first, breadcrumb + parent
  affordance.
- **v1.3.0d** — Multi-user onboarding: persistent invites at
  `{dataDir}/invites.json` (bcrypt-hashed, 30-day default TTL,
  per-invite label) with full create / revoke / rotate / copy-URL
  UX at `/admin/users`. Bulk-import keys at `/files/keys/new`
  (CSV / TSV / aws-cli) with per-row preview + non-aborting
  per-row error reporting via `POST /api/v1/user/regions/bulk`.
- **v1.3.0e** — Per-cluster `cluster_admin` assignment UI. New
  "Cluster admins" section above Buckets on the cluster detail
  page; `GET /api/v1/admin/clusters/{cid}/admins` filters global
  assignments to scopes matching `cluster:{cid}` (exact, wildcard,
  superuser). Inherited rows render with an amber badge + disabled
  Remove (managed from `/admin/policies`).

## v1.3.0e — 2026-05-22

Per-cluster cluster_admin assignment UI. Before this cycle, granting
someone admin authority over one specific cluster (without giving them
authority over every cluster) meant hand-editing assignment JSON or
typing `cluster:{cid}` into the global `/admin/policies` scope field
from memory. Cluster detail pages now surface a dedicated "Cluster
admins" section above Buckets — the operator's first question when
they land on a cluster page ("who runs this?") gets a direct answer.
Table renders user (display name + username), role, source (manual /
OIDC / inherited from global), and a Remove button. Wildcard
inheritance (`cluster:*` or the `*` superuser scope) shows up with an
amber "inherited from global" badge and a tooltip-explained disabled
Remove — those have to be managed from `/admin/policies` because they
affect more than this one cluster. New `+ Add cluster admin` button
opens a two-field modal (user picker eagerly fetched from
`/admin/users`, role picker defaulted to `cluster_admin` with other
non-deprecated roles available) that POSTs to the existing
`/admin/policies/assignments` endpoint with scope `cluster:{cid}`.
Backend gets one new convenience endpoint
`GET /api/v1/admin/clusters/{cid}/admins` gated on `policy:view_matrix`
@ `host:*`: filters the global assignment list to scopes matching
`cluster:{cid}` (covers exact, wildcard, and superuser via
`ScopeMatches`), joins user `Name` from the store server-side, and
marks inherited rows with a boolean so the FE doesn't have to re-do
the matching. Returns 404 for stale cluster links so the FE doesn't
render a misleading empty table. Test surface: four backend tests
(scoped + wildcard filtering, display-name join, capability gate
denial, unknown-cluster 404) and four FE tests (inherited row
disables Remove with tooltip, manual row enables Remove, OIDC row
renders the OIDC badge, hook URL points at the right endpoint).
`pnpm build` + `go test -race ./...` both green.

## v1.3.0d — 2026-05-22

Multi-user onboarding cycle — two related features both about adding
more humans to a basement install. Invite-token polish: persistent
invites now live in `{dataDir}/invites.json` (bcrypt-hashed, atomic
write, RWMutex), tokens default to 30-day expiry (operator-configurable
per invite), and `/admin/users` gains a Pending Invites section with
create + label + revoke + rotate + copy-full-URL affordances. The
public redemption endpoint (`POST /invites/{token}/redeem`) now
verifies against the persistent store instead of accepting any
well-formed input; expired tokens are cleaned up on the rejection
path. The optional label feeds the auto-generated username (sanitized
to lowercase + alnum + dash) so an invite labelled "wife" provisions
the user as `wife` instead of `user-abcd1234`. Bulk-import keys at
`/files/keys/new`: new "Bulk import" toggle swaps the single-key form
for a paste-area that auto-detects three formats — CSV (with snake_case
or camelCase header variants), TSV, and aws-cli credentials-file
profile blocks — renders a per-row preview table with client-side
validation errors, and submits via the new `POST
/api/v1/user/regions/bulk` endpoint. The bulk endpoint creates rows
independently: a per-row failure (`DUPLICATE_REGION`, `INVALID_ENDPOINT`,
`INVALID_REQUEST`) lands in the response's `errors` array with the
original index but doesn't abort the rest of the batch. The
`addressingStyle` column rides along when present (default `path`).
Gated only on USER-tier auth — anyone authenticated can bulk-add their
own keys. Invite endpoints stay gated on `host:manage_users` (admin
only). Test surface: store-level invite tests (create + redeem +
revoke + rotate + expiry rejection + persistence across reopen), API
tests for invite endpoints + bulk regions (happy path + per-row error
non-abort + duplicate detection + addressing-style honoring), FE
parser tests covering all three formats + header variants, FE UI
tests covering bulk-mode toggle + preview + submit. `pnpm build` +
`go test -race ./...` both green.

## v1.3.0c.1 — 2026-05-22

Folder navigation in the bucket browser via S3 `delimiter="/"`. Before
this cycle, the `/files/{regionId}/b/{bid}` route dumped every key
under the bucket flat — a hundred-deep `raw/broadcom-docid/...pdf`
tree looked like a hundred files at the root. Now the user-region
list-objects handler defaults `delimiter="/"` and returns
`commonPrefixes` (sub-folder rows) alongside `objects` (files at this
level). Folder rows render first, alphabetical; file rows after. The
breadcrumb across the top is the bucket alias + each prefix segment,
all clickable; an "Up to parent folder" affordance sits beneath. The
driver interface change (`ListObjects` gains a trailing `delimiter
string` param) cascades through all four drivers — garage_v1, garage,
aws_s3, minio — each setting `s3.ListObjectsV2Input.Delimiter` only
when non-empty. The sync engine + bucket-access probe continue to
pass `""` for flat recursive listing. Wire-shape rename: the JSON
field on `ObjectPage` is `commonPrefixes` (matches the S3
nomenclature), replacing the prior `prefixes`. Driver-level test
asserts the delimiter rides the wire only when non-empty (both
aws_s3 and minio); handler-level test asserts the default and the
explicit-empty paths; FE test asserts folder rows render before file
rows, clicking a folder navigates to the right prefix, and the empty
state copy changes inside a sub-folder. Build + race tests green;
both `prefixes` -> `commonPrefixes` references in the share viewer
and bucket browser updated together so nothing's left looking at
the old field.

## v1.3.0c — 2026-05-22

Per-region S3 addressing toggle + in-place key rotation. Two compact
operator-quality-of-life features delivered in one cycle. New
`UserRegion.AddressingStyle` field ("path" default | "virtual_host")
threads from the keychain through `Registry.ForUserRegion` and the
shared `driver.BuildS3Client` helper which picks the right
client constructor. New `driver.NewS3VirtualHostClient` mirrors the
existing `NewS3PathStyleClient`; both are picked through
`BuildS3Client` which enforces the IP-host smart default — an IP
literal forces path-style regardless of the requested toggle because
virtual-host addressing requires wildcard DNS for the bucket
subdomain. Backwards-compat: every UserRegion persisted before this
cycle reads back as path-style via the store's `applyReadDefaults`
helper; no migration needed. New `POST
/api/v1/user/regions/{regionId}/rotate` updates accessKey + secret in
place (preserving alias / endpoint / region / addressingStyle /
LastUsedAt history), audits as `region:rotate`, and invalidates the
cached S3 client for the old (endpoint, accessKey) tuple so the next
ListBuckets uses the fresh secret. Wrong-owner attempts collapse to
404 (region API security model). Frontend: `/files/keys/new` gains
an Advanced expandable with a "Use virtual-host addressing" toggle,
auto-disabled when the endpoint host is an IP literal; the AWS row
in "Common endpoints" auto-checks the toggle (AWS prefers
virtual-host for S3-tool compatibility). `/files/keys` cards show a
"via path-style" / "via virtual-host" subtitle and gain a "Rotate
key" button next to Delete, opening a 2-field dialog
(accessKeyId + secret) per the popups-max-2-fields rule. Card
shows "Last rotated …" for 24h after a rotation.

## v1.3.0a.4 — 2026-05-21

Two-mode auth model + operator-configurable admin TTL + drop-in-place
expiry banner (ADR-0003 amendment). The v1.2 USER/ADMIN/ELEVATED
state machine collapses to USER/ADMIN — the ELEVATED sub-mode was
cognitive overhead without real protection; the per-elevation TTL
remains the safety. `MinModeFor()` now returns only `ModeUser` or
`ModeAdmin`; every previously-ELEVATED capability (cluster:delete,
bucket:delete, key:delete, host:manage_users, host:manage_policies,
policy:edit_matrix, policy:assign_role, cluster:edit_layout)
collapses into the same ADMIN tier as cluster:edit + friends.
`ModeElevated` survives as a string alias for `ModeAdmin` for one
release cycle so v1.2 call sites compile unchanged; v1.2-era cookies
with `mode="elevated"` silently migrate to ADMIN on read in
`currentMode()` — no logout required across the upgrade. Likewise
`POST /auth/elevate` and `/auth/elevate/oidc/start` accept
`target_mode="elevated"` as a synonym for `"admin"`. New
`OrgCapabilities.AdminSessionTTLSec` (default 900, range 60–86400)
is the operator-configured per-elevation TTL, set from
`/admin/system` &rarr; "Admin session timeout" card (dropdown of
5m/15m/30m/1h/2h/8h presets + Custom seconds input). The
`/auth/elevate` handler reads this on each call instead of the
hardcoded 15-min default; clamping happens on write so an
out-of-band on-disk JSON value still produces a sane live TTL.
Frontend: `PersonaPill` loses the ELEVATED variant — just one
ADMIN variant with a wider warning ramp (amber at &lt;2 min, red +
flashing + "Stay admin" extend toast at &lt;30s) so the operator
gets real lead time and a one-click extend. New
`ElevationExpiredBanner` renders at the top of `/admin/*` after an
ADMIN&rarr;USER expiry transition; the banner offers a Re-elevate
button that pops the standard elevation modal, and dismisses on
successful re-elevation or on navigation away from `/admin/*` — the
page itself stays mounted so any in-progress form keeps its
state. Tests: drop the ELEVATED-specific assertions, add two-mode
tests + TTL config + clamp tests + banner tests + warning-ramp
tests.

## v1.3.0b — 2026-05-21

Driver-aware endpoint hints in cluster + key forms. New public
`GET /api/v1/system/driver-defaults` returns the curated
`EndpointDefaults` catalogue (admin URL, S3 endpoint, region label,
one-sentence hints, optional docs link) for every registered driver;
FE caches forever. Add Cluster + Edit Cluster forms now render
driver-specific placeholders (`http://garage-host:3903` for Garage,
`https://s3.us-east-1.amazonaws.com` for AWS S3, `http://minio-host:9000`
for MinIO) and inline hints under each input. Add Key form gains a
"Common endpoints" expandable with one-click "Use this" buttons for
each driver, plus an auto-suggest that fills the region label when
the operator pastes an endpoint matching a known pattern
(`amazonaws.com` → `us-east-1`, `garage` → `garage`, `minio` →
`us-east-1`) — never overwrites a region the operator has already
typed. Pure UX surface, no schema change.

## v1.3.0a.2 — 2026-05-21

Force path-style S3 addressing across every driver via a shared
`driver.NewS3PathStyleClient` helper (`internal/driver/s3client.go`).
Garage requires path-style; IP-addressed MinIO requires it (no DNS
wildcard for `bucket.10.x.y.z`); AWS S3 accepts it on every region.
Inline copies in `internal/drivers/{aws_s3,garage,garage_v1,minio}`
collapse into one call site so future driver work cannot drift back
to virtual-host. Fixes the `404 NotFound` on user-region ListObjects
against Garage (request was routing to `http://lsi.10.1.7.10:3902/`
instead of `http://10.1.7.10:3902/lsi/`). Adds
`TestNewS3PathStyleClient_ForcesPathStyle` as the regression guard;
no behavioural change for the AWS-S3 / MinIO admin paths that
already had the flag set inline. Builds on the cycle v1.3.0b
follow-ups (`81b4928`, `f1f0bc3`) that updated garage_v1 to call
the helper — this commit lands the helper itself.

## v1.3.0a.1 — 2026-05-21

Graceful handling of backend-revoked user keys. Region endpoints
(`/api/v1/user/regions/{id}/buckets`, `/objects`, presign-get/put,
multipart init/part/complete/abort, delete-object) now detect the
underlying S3 auth-rejection codes (`InvalidAccessKeyId`,
`SignatureDoesNotMatch`, `AccessDenied`, `Forbidden`,
`InvalidSignature`) and surface them as 401 `USER_KEY_REJECTED` with
the offending region + alias + endpoint + accessKeyId in the error
payload — replacing the bare 500 `INTERNAL` the un-fixed path produced
for matthew's `lsi` region (key `GK6f4403ea8f6168544d035f4d` was
deleted on Garage but still cached in the keychain). FE renders an
inline alert with "Delete this region" + "Add a fresh key" actions
instead of a generic "internal error" toast.

## v1.2.0 — 2026-05-21

Sudo-style admin elevation (ADR-0003): USER → ADMIN → ELEVATED
session state machine; destructive ops require fresh re-auth with
short TTL (15 min ADMIN, 5 min ELEVATED, both env-tunable);
local-password or OIDC step-up via `prompt=login` + 60s `auth_time`
freshness check; persona pill carries live mode + countdown +
"drop privileges" button. Key-first user model: each access key is
a card on `/files`, multiple keys per endpoint allowed
(`unique(userId, endpoint)` → `unique(userId, endpoint, alias)`).
New env vars `BASEMENT_ADMIN_TTL_SEC`,
`BASEMENT_ELEVATED_TTL_SEC`, `BASEMENT_OIDC_ELEVATION_PROMPT`. No
breaking changes; existing matthew session grandfathered to ADMIN
for 7 days post-deploy.

Full notes: [`docs/release-notes/v1.2.0.md`](docs/release-notes/v1.2.0.md)

### Cycles

- **v1.2.0a** — Backend mode state machine: JWT carries `mode` +
  `mode_expires_at` claims; `POST /api/v1/auth/elevate` mints
  ADMIN (15min TTL) or ELEVATED (5min TTL) cookies after
  password re-auth; gate enforces `MinModeFor(capability)` so
  destructive ops (cluster:delete, bucket:delete, key:delete,
  policy:edit_matrix, host:manage_*, cluster:edit_layout) get
  403 ELEVATION_REQUIRED until the session steps up. Pre-v1.2
  cookies (no mode claim) treated as ADMIN for a 7-day back-compat
  grace window so the existing matthew session keeps working
  across the deploy.
- **v1.2.0b** — Frontend elevation modal + persona pill countdown.
  `/auth/me` now echoes the live mode + expiry; new
  `POST /auth/logout-elevation` lets the operator drop privileges
  back to USER without re-logging-in. `<AuthModeProvider>` mirrors
  the cookie state in React with a 1Hz auto-downgrade tick;
  `<ElevationProvider>` exposes `useElevationGuard()` so destructive
  click handlers re-prompt for the password and retry on success;
  the openapi-fetch middleware opens the modal eagerly on any
  unhandled 403 ELEVATION_REQUIRED. `<PersonaPill>` lives in
  AppShell + UserShell: USER neutral pill, ADMIN amber pill +
  mm:ss countdown, ELEVATED orange pill + SVG lightning bolt,
  flash + toast at <30s, "drop privileges" button next to the
  countdown chip.
- **v1.2.0c** — OIDC step-up elevation via `prompt=login`. New
  `POST /auth/elevate/oidc/start` mints a state token + returns
  the IdP authorize URL with `prompt=<BASEMENT_OIDC_ELEVATION_PROMPT,
  default login>` + `max_age=0`; `GET /auth/elevate/oidc/callback`
  validates state (5min TTL, same-session bound), checks the new
  ID token's `auth_time` is within 60s (rejects cached IdP sessions
  that ignore `prompt`), mints the elevated cookie, and 302s the
  browser to `/?elevated=<mode>`. The v1.2.0a `/auth/elevate`
  dispatcher now pivots OIDC-only users with `{requires_oidc:true,
  start_url}` instead of 403; `/auth/me` advertises a new `oidcUser`
  boolean so the FE renders an "Elevate via SSO" button (no
  password field) for OIDC accounts. `AuthModeHydrator` picks up
  the callback's `?elevated=<mode>` query param, fires a success
  toast, invalidates `/auth/me`, and strips the param from the
  URL. In-memory state map cleans expired entries on each insert.
- **v1.2.0d** — Key-first user model + drop unique-endpoint constraint.
  Backend uniqueness on `UserRegion` moves from `(userId, endpoint)`
  to `(userId, endpoint, alias)` so a user can register multiple
  access keys against the same S3 endpoint ("Work S3" +
  "Personal S3"); same alias still 409s. The sync resolver picks
  the first match per endpoint (all keys at one endpoint bridge to
  the same admin Connection) and emits a debug log when multiple
  keys exist. Frontend: new canonical `/files/keys/new` "Add a
  key" form, `/files/regions/new` redirects to it; `/files/keys`
  becomes "My Keys"; `/files` keeps "My Regions" but reframes the
  subtitle as "Each card is one of your access keys" and renders
  `UserKeyCard` (alias prominent, endpoint hostname as small mono
  subtitle, access-key-ID truncated below). `useCreateUserRegion`
  hook renamed to `useCreateUserKey` (`useUserRegions` kept — it
  matches the storage type).

## v1.1.0 — 2026-05-21

Region tier replaces phantom Connections at the user persona
(ADR-0002). Per-user keychain at `/files/keys`; one credential per
endpoint; backend authoritative for bucket visibility. `bucket_user`
role deprecated. Sync + share become region-aware. Legacy
`/api/v1/user/clusters/*` + `/user/buckets/connect` + `/user/keys`
endpoints removed.

Full notes: [`docs/release-notes/v1.1.0.md`](docs/release-notes/v1.1.0.md)

### Cycles

- **v1.1.0a** — UserRegion store + AES-GCM secret encryption + uniqueness on (userId, endpoint)
- **v1.1.0b** — Region API endpoints `/api/v1/user/regions/*` (CRUD + bucket + object ops)
- **v1.1.0c** — Frontend rewrite of `/files` persona: `/files/regions/new`, `/files/{regionId}`, `/files/{regionId}/b/{bid}`
- **v1.1.0c.1–c.4** — Garage ListBuckets S3-fallback fixes + smoke-test alignment
- **v1.1.0d** — Garage admin bridge + `bucket_grants.json → user_regions.json` migration + region-keychain UI
- **v1.1.0e** — Retire legacy cluster-tier user endpoints + bucket-grants cleanup
- **v1.1.0f** — Deprecate `bucket_user` role in policy matrix
- **v1.1.0g** — Region-to-connection resolver for sync + share engines + `region:list_buckets` audit hook
- **v1.1.0h** — Object-tier audit hooks (`region:list_objects`, `region:presign_get/put`, `region:multipart_*`, `region:delete_object`) + release notes + README + CHANGELOG

## v1.0.0 — 2026-05-21

The production-ready milestone. Three-tier role model (Host Admin /
Cluster Admin / User), flexible policy matrix at `/admin/policies`,
per-user encrypted S3 credentials, audit log subsystem, metrics
persistence + time-series chart on `/admin/usage`.

Full notes: [`docs/release-notes/v1.0.0.md`](docs/release-notes/v1.0.0.md)

### Cycles

- **v1.0.0a** — encrypt admin_token + secret_key in `connections.json` at rest
- **v1.0.0b** — retire legacy `internal/store/grants.go` in favour of BucketGrants
- **v1.0.0c** — audit log subsystem + `/admin/audit` viewer
- **v1.0.0d** — metrics persistence + time-series chart on `/admin/usage`

## Pre-v1.0

See git tag history (`v0.4.0` through `v0.9.0m.1`) for the lead-up
to the v1.0 milestone — multi-cluster admin (v0.5.0), four drivers
+ OIDC (v0.5.x), end-user shell + sharing (v0.6.x–v0.7.x),
cross-backend sync (v0.8.x), operator polish + ADR-0001 RBAC
(v0.9.x).
