# Changelog

All notable changes to basement are recorded here. See the linked
release-notes files in `docs/release-notes/` for the full per-release
write-up; this file is the at-a-glance index.

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
