# Changelog

All notable changes to basement are recorded here. See the linked
release-notes files in `docs/release-notes/` for the full per-release
write-up; this file is the at-a-glance index.

## v1.10.0.1 — 2026-05-22

Smoke-caught bug-fix cycle on top of v1.10.0e. Three findings from
the comprehensive smoke audit (section [C] form validation + section
[E] mobile touch targets) addressed in one tag:

- **`/files/keys/new` blank submit silently did nothing.** The submit
  button was `disabled` on blank required fields, so a click produced
  no feedback. Now the button stays enabled; clicking with blanks
  surfaces inline `role="alert"` messages next to each empty field
  and flips `aria-invalid` on each offending input. Mutation only
  fires once everything is filled in.
- **`/files/federated-buckets/new` Next button silently did nothing.**
  Same shape: the wizard's Next was `disabled` until the current
  step validated. Now Next stays enabled, clicking with an incomplete
  step surfaces an inline `role="alert"` describing exactly what's
  missing (per step), the wizard refuses to advance. Step 5 Create
  carries the same treatment.
- **UserShell mobile nav tap targets <44px.** Top-nav links rendered
  at the text line-height (~20px) and the Logo lockup at 40px — both
  below the WCAG/iOS HIG 44×44 threshold flagged in smoke section
  [E]. Added `min-h-[44px]` to `NAV_LINK` and `min-h-[44px]
  min-w-[44px]` to the Logo anchor; desktop visuals unchanged (the
  utilities only expand the hit-area box, not the rendered text /
  SVG).

Tests: new unit tests cover (a) blank-submit inline alerts on the
Add-a-key form, (b) blank Next on each federation wizard step
surfacing the right inline error, (c) tap-target utilities present on
UserShell nav + Logo. Touched: `frontend/src/routes/files/keys/new.tsx`,
`frontend/src/routes/files/federated-buckets/new.tsx`,
`frontend/src/shared/layout/UserShell.tsx`,
`frontend/src/shared/ui/Logo.tsx`, plus their `__tests__/`. 353/353
frontend tests + `go test -race ./...` green; comprehensive smoke
re-run against the deploy promotes from `bug` to `no-bug` for the
three affected checks.

## v1.10.0 — 2026-05-22

Compliance + integrity milestone — versioning + Object Lock + SSE.
v1.10 closes the v1.x arc with the ransomware shield + compliance
posture that complements v1.6 federation: federation replicates data
across backends; versioning + object-lock + server-side encryption
make those replicas resilient, recoverable, and private. Five primary
cycles (`v1.10.0a` → `v1.10.0e`) plus a smoke-gate-caught
`v1.10.0d.1` AppShell hydration-race hotfix (same shape as
v1.7.0a.3/a.4). Bucket versioning landed first (driver interface +
AWS S3 + MinIO full + Garage stubs + UI card +
`ObjectVersionsPanel`); Object Lock layered on versioning per the S3
spec (Governance + Compliance modes + per-version legal hold);
default server-side encryption shipped last (SSE-S3 + SSE-KMS with
per-axis capability bits gating the algorithm radio). AWS S3 + MinIO
full across all three; Garage v1 / v2 advertise unsupported (upstream
content-addressed block store conflicts with versioned overwrites)
and the UI renders graceful "Not supported by this backend driver"
notices instead of fighting the gap. 72/72 smoke checks pass against
the live deploy that this milestone promotes. **v1.x is feature-
complete with this release; v2.0 = basement IS a backend (S3
gateway) is the next major.** ADR-0006 (v2.0 gateway design) is the
next senior artifact. Full write-up in
[docs/release-notes/v1.10.0.md](docs/release-notes/v1.10.0.md).

**v1.x roadmap complete; v2.0 chain begins next.** The v1.x line ran
v1.0 → v1.10 in one extended operator session-and-change. Recap of
every minor tag in the v1.x line:

- **v1.0** — Multi-backend admin + RBAC + scoped creds (ADR-0001)
- **v1.1** — Region tier replaces phantom Connections at user persona (ADR-0002)
- **v1.2** — Sudo-style admin elevation (ADR-0003)
- **v1.3** — Multi-user polish (OIDC group mapping + invites + per-cluster admin UI)
- **v1.4** — Scale + perf (virtualized browser + paginated key perms + growth analytics + scrub UI)
- **v1.5** — Backup story (scheduled backups + GFS retention + restore wizard)
- **v1.6** — Federation + multi-backend replication (ADR-0005)
- **v1.7** — Service accounts + webhooks + event-driven federation
- **v1.8** — MCP server + Mobile PWA + service-account config UX + rebrand / relicense
- **v1.9** — WebDAV gateway + pluggable gateway architecture
- **v1.10** — Compliance + integrity (versioning + Object Lock + SSE)

## v1.10.0e — 2026-05-22

Milestone gate cycle for v1.10.0. Smoke 72/72 green against the live
v1.10.0d.1 deploy that v1.10.0 promotes; full v1.10 smoke check
suite (`[v1.10a]` bucket detail compliance sections + `[v1.10b]`
versioning API shape + `[v1.10c]` object-lock API shape + `[v1.10d]`
encryption API shape + `[v1.10e]` driver-unsupported branch
rendering); screenshot pass to `/tmp/v1.10.0-screenshots/` capturing
the three sections in their unsupported branch (live deploy has
Garage-only user regions). Release notes
([`docs/release-notes/v1.10.0.md`](docs/release-notes/v1.10.0.md))
with the end-of-v1.x summary table; README updated with v1.10
feature bullets + competitive matrix gains (parity with MinIO
Console on the security / integrity axis); CHANGELOG v1.10.0 entry
with v1.x line recap.

## v1.10.0d.1 — 2026-05-22

Smoke gate v1.10 + AppShell admin-redirect hydration hotfix. Smoke
surface adds five new v1.10 checks. AppShell hotfix: the v1.9.0e.2
tight-coupling redirect fired on the first render of `/admin/*`
before AuthModeHydrator could push the cookie-derived mode into the
provider — every full-page navigation to `/admin/*` bounced to
`/files` even on freshly-elevated sessions. Same shape as the
v1.7.0a.3/a.4 AdminEntryElevationGuard hotfix: (1) defer redirect
while `useUser()` is loading; (2) belt-and-braces — read `user.mode`
directly off `/auth/me` payload and short-circuit when admin /
elevated. Two new tests pin both branches.

## v1.10.0d — 2026-05-22

Bucket default server-side encryption — SSE-S3 + SSE-KMS. Driver
gains four new methods (`BucketEncryptionSupport` returning
per-axis SSE-S3 + SSE-KMS bits, `GetBucketEncryption` +
`PutBucketEncryption` + `DeleteBucketEncryption`); AWS S3 full (both
axes), MinIO full (SSE-S3 + SSE-KMS where the MinIO server is
wired with a KMS backend — the driver surfaces the live state
honestly), Garage stubs returning `ErrUnsupported`. User-tier API
under `/api/v1/user/regions/{rid}/buckets/{bid}/encryption` (GET
folds the per-axis capability bits + algorithm + key ID into one
response so the FE branches off one fetch). New
`EncryptionSection` card on the bucket detail page handles four
branches internally (unsupported / SSE-S3-only / both-axes / enabled
with separate Disable button); capability-honest gating —
no driver-name checks anywhere in the FE.

## v1.10.0c — 2026-05-22

Object Lock — bucket configuration + per-version retention + per-
version legal hold. Driver gains seven new methods
(`ObjectLockSupport` + `GetObjectLockConfig` + `PutObjectLockConfig`
+ `GetObjectRetention` + `PutObjectRetention` + `GetObjectLegalHold`
+ `PutObjectLegalHold`); AWS S3 + MinIO full, Garage stubs. User-
tier API under `/object-lock` + per-version `/retention` +
`/legal-hold`. New `ObjectLockSection` card layered on versioning
per the S3 spec — three branches surfaced internally (unsupported /
needs-versioning / full editor). Once Object Lock is enabled, the
disable affordance disappears (S3 one-way contract). Per-version
lock UX in `ObjectVersionsPanel`: lock status pills (Compliance /
Governance until YYYY-MM-DD or Legal hold), Set retention modal
with reduce-detection (compliance-reduce blocked; governance-reduce
surfaces bypass-governance toggle), Set / Release hold toggle,
Delete affordance gated on lock state.

## v1.10.0b — 2026-05-22

Bucket versioning UI — `VersioningSection` card on the bucket detail
page + `ObjectVersionsPanel` per-object version history + "Show all
versions" toggle on the bucket browser. Three branches mirror
`LifecycleSection`'s posture: supported=false → "Not supported by
this backend driver" notice; supported + disabled → select offers
Enabled only (S3 contract: once enabled, can't flip back to "never
enabled"); supported + enabled-or-suspended → Enabled + Suspended
options. ObjectVersionsPanel mounts inline below the file list when
the operator clicks "Versions" on any file row — lists every version
with version-ID, size, modified timestamp, current badge,
delete-marker chip, Download + Delete affordances.

## v1.10.0a — 2026-05-22

Bucket versioning — driver interface + AWS S3 + MinIO impl +
user-tier API. First cycle of the v1.10 compliance + integrity
track. Driver gains seven members (`VersioningSupport` capability
gate, `GetVersioningStatus` + `EnableVersioning` +
`SuspendVersioning` bucket-level toggle, `ListObjectVersions` +
`GetObjectVersion` + `DeleteObjectVersion` per-object history).
AWS S3 + MinIO ship full via aws-sdk-go-v2; Garage v1 + v2 stubs
return `ErrUnsupported` from every method
(`VersioningSupport()=false`) — upstream Garage doesn't implement
bucket versioning in its S3 surface, content-addressed block store
semantics conflict with versioned overwrites. User-tier endpoints
under `/api/v1/user/regions/{rid}/buckets/{bid}/versioning` + per-
version `/versions[/{vid}]`. Wire envelope folds the capability flag
into the GET payload so the FE branches off one fetch.

## v1.9.0 — 2026-05-22

WebDAV gateway + pluggable gateway architecture milestone. v1.9
lights up basement's third client surface (native filesystem mount,
on top of v1.8's mobile PWA and MCP) and lays the architecture for
the rest. New `internal/gateway/` package with `Gateway` + `Backend`
+ `Registry` interfaces makes the gateway tier as pluggable as the
driver tier. WebDAV ships as the first real implementation
(`/webdav/` tree, Basic auth via password or `BMNT...:secret`,
Finder / Explorer / Nautilus / iOS Files / rclone). Four stub
gateways (SMB, NFS, FTP, S3) register at boot so the
`/api/v1/admin/gateways` roster + the `/admin/system` Gateways card
surface the full protocol matrix from day one, marked "coming soon"
until their implementations land. The Gateways card is registry-
driven — adding a new gateway in v1.10+ takes one file in
`internal/gateway/{name}/` plus a `Register()` call in `main.go`,
no UI changes. Time Machine integration docs are honest about
basement not shipping native SMB (no production-grade pure-Go SMB
server) and document the recommended NAS + BACKUP-wizard pattern
plus the Samba + s3fs-fuse community sidecar workaround. Full
write-up in [docs/release-notes/v1.9.0.md](docs/release-notes/v1.9.0.md).

## v1.9.0e.2 — 2026-05-22

Tight mode/view coupling. Operator UX reset that kills the "ADMIN
mode on user pages or vice versa" confusion the v1.7.0a-era guards
papered over. Mode now drives navigation: dropping privileges always
sends the operator to `/files`, elevating always sends them to
`/admin/clusters`, and URL-bar navigation to `/admin/*` in USER mode
silently redirects to `/files` instead of firing an elevation prompt.
Deletes `AdminEntryElevationGuard` (the side-effect-only
elevation-on-entry hook that conflicted with the × drop button) and
`AdminUserModeBanner` (impossible state under the new coupling).
Reverts the v1.9.0e.1 PersonaPill mode-vs-view distinction — the
pill is now MODE-only again, because under tight coupling USER can't
reach `/admin/*` at all and admin on `/files/*` is the only mismatch
state, expressed by routing rather than by the pill's visual variant.
Keeps the v1.9.0e.1 elevation success cache-invalidate fix so the
hydrator picks up the rotated cookie immediately. PersonaPill's ×
drop button + UserMenu's "Switch to user view" both call
`/auth/logout-elevation` then navigate to `/files`; UserMenu's
"Switch to admin view" still elevates then navigates to
`/admin/clusters`. Logo href tracks mode in both shells so the
header brand mark always points at the operator's "home" for the
mode they're in.

## v1.9.0e.1 — 2026-05-22

Elevation success cache-invalidate + PersonaPill mode-vs-view. Two
operator-reported bugs in the elevation flow. **Bug 1:** clicking
"Switch to admin" in UserMenu → entering password → success → the
guard at `/admin/clusters` immediately re-prompted because the cached
`/auth/me` payload still said `mode: "user"` and
`AdminEntryElevationGuard` reads `user.mode` directly off the cached
payload to decide whether to fire. Fix in
`shared/auth/elevation.tsx` — `handleSuccess` now invalidates the
`["auth", "me"]` (and `["user"]`) queries so `AuthModeHydrator` picks
up the freshly-rotated cookie mode immediately. Same shape as the
v1.7.0a.2 drop-privileges cache-staleness bug, just on the rising
edge. **Bug 2:** `PersonaPill` showed an identical "ADMIN" pill
regardless of which URL the operator was on, so admin-mode-on-`/files`
looked the same as admin-mode-on-`/admin/*` — confusing because the
URL didn't match the chrome. Fix in `components/layout/PersonaPill.tsx`
— pill now consults `useLocation()` and renders a solid amber pill
on `/admin/*` ("ADMIN") versus an outlined / muted amber pill on
`/files/*` ("ADMIN · user view") with a tooltip explaining the state.
USER mode pill is unchanged. Three new test files pin both fixes:
`elevation-invalidate.test.tsx`, `PersonaPill-view.test.tsx`, and
`AdminEntryElevationGuard-no-double-prompt.test.tsx`.

## v1.9.0d — 2026-05-22

Generalised Gateways UI + plugin doc. The WebDAV-hardcoded card on
`/admin/system` shipped in v1.9.0b is replaced by a registry-driven
card that renders one row per Gateway returned from
`GET /api/v1/admin/gateways`: capability chips (read / write / delete
/ move / lock / basic-auth / bearer-auth / sigv4-auth), live status
(running, active connections, last activity, total requests), mount
URL with Copy button + per-platform connect hints for implemented
gateways, "Coming soon" badge in place of an enable toggle for stubs.
Auto-refresh on a 30s tick keeps the status counters honest. New
`useGatewaysRegistry()` hook wraps the endpoint; the card writes
both the legacy `webdav` field AND the new generic
`protocols["webdav"]` entry so v1.9.0b operators who flipped the
WebDAV kill switch keep their state through the upgrade. The
`OrgCapabilities.Gateways` shape gains a generic
`Protocols map[string]GatewayConfig` nest (Enabled + BaseURL +
Options) so v1.10+ gateways can ship per-protocol settings without a
new Go field per gateway; a legacy v1.9.0b file with only
`gateways.webdav.{enabled,baseUrl}` auto-migrates into
`Protocols["webdav"]` on read. New plugin doc
`docs/integrations/adding-a-gateway.md` walks through the Gateway +
Backend interfaces, the boot-time wiring in `main.go`, the per-
protocol Enable toggle pattern, the testing recipe, and points at
`internal/gateway/webdav/` as the reference implementation. Stub
docs (`smb.md`, `nfs.md`, `ftp.md`, `s3.md`) ship as implementation-
tracking placeholders the card's per-row docs links point to.

## v1.9.0c — 2026-05-22

Gateway architecture cycle. The WebDAV gateway shipped in v1.9.0a/b
generalised into a pluggable `gateway.Gateway` interface so v1.10+ can
drop in SMB, NFS, FTP, and S3 by implementing the same contract. New
package `internal/gateway/` holds the `Gateway` + `Backend` +
`Registry` interfaces and the production `Backend` that composes
existing primitives (`config.Admin`, `store.UserRegions`,
`serviceaccount.ServiceAccounts`, `driver.Registry`, `store.Connections`)
into a single data-plane surface. WebDAV moved from `internal/webdav`
into `internal/gateway/webdav` and now drives every storage call
through `Backend.{ListRegions, ListBuckets, ListObjects, HeadObject,
GetObject, PutObject, DeleteObject, CopyObject, CreateBucket,
DeleteBucket}` — the protocol code no longer reaches into the driver
registry or the user-region store directly. Four stub registrations
(SMB, NFS, FTP, S3) are wired into the registry at boot so
`/admin/gateways` lists the full protocol roster with "coming soon"
badges driven by `Implemented() = false`. New endpoint
`GET /api/v1/admin/gateways` returns the registry roster (name,
displayName, description, capabilities, status, implemented, enabled)
for the v1.9.0d generalized UI cycle. Behaviour-preserving: every
existing WebDAV verb test migrated unchanged; the `/webdav/` mount
path + auth shapes + kill-switch contract carry through the refactor
unmodified.

## v1.9.0b — 2026-05-22

Gateways settings + status UI + Time Machine docs. The WebDAV gateway
shipped in v1.9.0a is now operator-facing in `/admin/system` via a new
**Gateways** card: an Enabled toggle (default on, default-on migration
for legacy `org_capabilities.json` files), the auto-derived mount URL
with a Copy button, an optional Base URL override for reverse-proxy
deployments, and per-platform connect hints for macOS Finder, Windows
Explorer, Linux Nautilus, and iOS Files. Flipping Enabled off makes the
backend `/webdav/*` tree return `403 GATEWAY_DISABLED` from the next
request without a re-deploy. The Gateways card also has an explicit
**SMB — not supported natively** section that links to the new
`docs/integrations/time-machine.md` explainer (why we don't ship SMB,
the Samba-sidecar pattern as a community-supported workaround, and the
recommended NAS + basement BACKUP wizard pattern for Mac data). Full
WebDAV walkthrough, auth, limitations, and troubleshooting moved into
`docs/integrations/webdav.md`.

## v1.9.0a — 2026-05-22

WebDAV gateway. New `/webdav/` tree on the same chi router as
`/api/v1` exposes a user's regions, buckets, and objects to any
native WebDAV client (macOS Finder, Windows Explorer, Linux
Nautilus, iOS Files, Android, rclone). Auth is HTTP Basic — either
`username:password` against the env-admin / store users or a
service-account `BMNT...:secret` pair in the user/pass slots, so a
key minted at `/admin/service-accounts` can drive a Finder mount
without re-authenticating against the JSON login. Implementation
sits in `internal/webdav` over `golang.org/x/net/webdav`; a custom
`FileSystem` maps WebDAV paths (`/{alias}/{bucket}/{key}`) onto the
existing per-user `driver.ForUserRegion` surface so the Garage admin
bridge that powers `/api/v1/user/regions/{id}/buckets` is honoured
verbatim on PROPFIND. LOCK/UNLOCK return 501 (most clients are
read+write without locks); MOVE/COPY use ServerSideCopy; MKCOL on
a region root creates a bucket. The `AllowContentType` JSON
middleware moved off the root router into the `/api/v1` sub-router
so WebDAV PUT/PROPFIND aren't rejected with 415.

## v1.8.0 — 2026-05-22

MCP server + Mobile PWA + service-account config UX milestone. Six
primary cycles (v1.8.0a → v1.8.0e) plus this milestone tag give
basement its AI-agent-driveable control plane + phone-installable
shell + rebrand-and-relicense cutover end-to-end. Headline surfaces:
`basement-mcp` stdio server with ten tools (seven read + two write +
one placeholder) authenticated via v1.7 service accounts;
`<McpConfigSection>` shared component that emits ready-to-paste
`config.yaml` + Claude/Cursor JSON from any service account;
`/admin/service-accounts/$id` detail page surfaces the MCP config
after the shown-once dialog has closed; vite-plugin-pwa + iOS
standalone hooks + theme color make the web app installable on
Safari iOS + Chrome Android; mobile bucket browser flips to a
stacked card layout below 640px with 56px tap-target rows;
`<InstallToHomeScreenHint>` banner on `/files` walks non-technical
household users through Share → Add to Home Screen. Project
rebranded `basement-ui` → `basement` (Go module
`github.com/mattjackson/basement`, Docker image
`ghcr.io/mattjackson/basement`, OpenAPI spec `basement.yaml`) and
relicensed MIT → AGPLv3 with commercial-license escape hatch
(contact matthew@pq.io). The v1.8.0a `basement` CLI binary was
deleted in v1.8.0d before it could ship — aws-cli + web UI + MCP
cover the matrix.

Full notes: [`docs/release-notes/v1.8.0.md`](docs/release-notes/v1.8.0.md)

### Cycles

- **v1.8.0a** — `basement` CLI binary (later deleted in v1.8.0d).
  Introduced `internal/clilib` shared config + HTTP client package
  that survives the CLI deletion (basement-mcp re-uses it).
- **v1.8.0b** — Rebrand cutover: project renamed `basement-ui` →
  `basement`; Go module path moved to `github.com/mattjackson/basement`;
  OpenAPI spec file renamed; LICENSE switched from MIT to AGPLv3;
  Docker image moved to `ghcr.io/mattjackson/basement`; commercial-
  license escape hatch documented.
- **v1.8.0b.1** — Follow-on fix: OpenAPI filename `basement-ui.yaml`
  renamed to `basement.yaml` (the cutover commit missed it; CI was
  loading the old path).
- **v1.8.0c** — `basement-mcp` Model Context Protocol stdio server.
  Ten tools at launch; bearer-only auth via v1.7 service accounts
  + `~/.config/basement/config.yaml`; JSON-RPC 2.0 on newline-
  delimited stdin/stdout; install paths documented for Claude
  Desktop, Claude Code, Cursor.
- **v1.8.0d** — Drop `cmd/basement-cli/` (auditing found
  duplication with aws-cli + web UI); add "Use with MCP" affordance
  to service-account UI via new `<McpConfigSection>` shared
  component reused by `<SecretShownOnceDialog>` (mint flow) and
  the new `/admin/service-accounts/$id` detail route. Scrubs stale
  CLI references from README + v1.7 notes.
- **v1.8.0e** — Mobile PWA. vite-plugin-pwa generates
  `manifest.webmanifest` + `sw.js`; `/api/*` denylisted from
  navigation fallback; iOS standalone meta tags + apple-touch-icon
  + theme color; bucket browser card layout below 640px with 56px
  rows (iOS HIG tap-target compliance); `<InstallToHomeScreenHint>`
  banner on `/files` with one-time-dismissible localStorage gate.
- **v1.8.0f** — This milestone tag. Smoke + screenshot pass +
  release notes + CHANGELOG + README forward-link.

## v1.8.0e — 2026-05-22

Mobile PWA — installable on phone home screen + mobile-friendly
bucket browser. vite-plugin-pwa generates `dist/sw.js` +
`dist/manifest.webmanifest` on every build; the service worker
precaches the static app shell (HTML, JS, CSS, images, fonts) for
offline shell loads. `/api/*` is denylisted from the navigation-
fallback so auth-scoped responses always hit the network instead of
being served stale from the cache. `index.html` declares the iOS-
specific `apple-mobile-web-app-capable` + `apple-mobile-web-app-
status-bar-style` meta tags Safari needs to render standalone (the
web manifest alone isn't enough for Safari iOS); `apple-touch-icon`
at 180×180 supplies the home-screen icon; `theme-color: #C9874B`
tints the address bar on Chrome / Edge Android. The existing
`/site.webmanifest` is kept alongside the new `/manifest.webmanifest`
for back-compat with v1.7-or-earlier installed shortcuts. Bucket
browser virtualized row renderer flips to a stacked card layout
below 640px (`useIsMobile()` via `(max-width: 639px)` matchMedia);
row height bumps to 56px so checkbox + filename tap targets meet
iOS HIG's 44px minimum; size + last-modified columns hide so the
file name is the primary visual element; `data-layout="card"` on
the scroll container is the smoke / E2E observability seam. New
`<InstallToHomeScreenHint>` banner on `/files` renders when (1)
localStorage flag not set, (2) display-mode=browser, (3) mobile
viewport — explains Share → Add to Home Screen for Safari iOS
(which doesn't auto-prompt); same banner shows on Android even
though Chrome auto-prompts via `beforeinstallprompt`, for
consistency. Once dismissed, never re-shows for that device.

## v1.8.0d — 2026-05-22

CLI removed; aws-cli + web UI cover the use cases. The
`cmd/basement-cli/` binary that was on the original v1.8 plan never
landed on main — object-store CRUD is better served by aws-cli
against the SigV4 endpoint, and the basement-specific control plane
(clusters, keys, service accounts, webhooks, federation, backups)
lives in the web UI where the role matrix + elevation flow already
gate the dangerous bits. Stale references in README,
`internal/clilib/config.go`, and the v1.7.0 release notes are
scrubbed. `internal/clilib` keeps its generic shape so any future
out-of-process client (a `basement-mcp init` subcommand, a homebrew
script, etc.) can adopt the same YAML + `$BASEMENT_CONFIG`
precedence without a migration step.

Service-account UI gains a "Use with MCP" affordance for the
shipped client. New shared component `<McpConfigSection>` renders
two snippets per service account:

  - `config.yaml` — the same shape `cmd/basement-mcp` reads via
    `internal/clilib`. Endpoint defaults to `window.location.origin`
    so an operator standing in front of their own basement gets the
    right URL without typing. Copy + Download buttons. On the mint-
    success dialog (`SecretShownOnceDialog`) the YAML inlines the
    plaintext secret — same shown-once contract the rest of the
    dialog enforces. On the detail page (no plaintext available) the
    YAML renders `<SECRET_FROM_ROTATE>` as a placeholder + a hint to
    rotate the secret to refill it.
  - Claude Desktop / Claude Code / Cursor JSON config — `command:
    basement-mcp` with `BASEMENT_CONFIG` pointing at the YAML above.
    Copy button.

New route `/admin/service-accounts/$id` shows identity, capabilities,
and the "Use with MCP" card. The list page's name column now links
to the detail page so operators can grab the snippet after the
shown-once dialog has closed.

## v1.8.0c — 2026-05-22

MCP server binary (`basement-mcp`). New stdio-based Model Context
Protocol server exposes a curated subset of basement-server's API
as MCP tools so AI agents (Claude Code, Claude Desktop, Cursor)
can drive storage workflows via natural language. JSON-RPC 2.0 on
newline-delimited stdin/stdout (no HTTP variant); protocol version
2024-11-05; advertises `tools` capability only. Ten tools ship —
seven read (`basement_list_regions`, `basement_list_buckets`,
`basement_list_objects`, `basement_get_object_metadata`,
`basement_list_backups`, `basement_list_federations`,
`basement_list_audit`), two write (`basement_create_share`,
`basement_create_backup_run`), and one forward-compatible
placeholder (`basement_search` returns `NOT_IMPLEMENTED` until the
v1.9 search-index cycle). Auth is bearer-only via v1.7.0b service-
account credentials read from `~/.config/basement/config.yaml`;
the MCP server inherits whatever capabilities the SA was granted
and does not define its own role model. New `internal/clilib`
shared package factors out the YAML config loader + HTTP client
so a future `basement` CLI can reuse the same plumbing without a
fork. Install paths documented for Claude Desktop, Claude Code,
and Cursor in `cmd/basement-mcp/README.md`. No driver changes; no
new env vars except the existing `$BASEMENT_PROFILE` /
`$BASEMENT_SECRET_KEY` from the CLI substrate. Tool calls log to
stderr (JSON) so the stdout transport stays clean.

## v1.8.0b.1 — 2026-05-22

OpenAPI filename rename follow-on: `openapi/basement-ui.yaml` →
`openapi/basement.yaml`. The v1.8.0b rebrand cutover swept LICENSE,
package.json, README.md, the v1.7 release notes, and the Go module
path in one pass but missed the spec file name itself — CI was
loading `openapi/basement-ui.yaml` by path and failed on v1.8.0b.
Single-file rename + CI loader path fix; no behavior change.

## v1.8.0b — 2026-05-22

Rebrand cutover: project renamed `basement-ui` → `basement`. The
original name outgrew itself — basement is no longer just a UI,
it's a control-plane substrate (federation engine, backup engine,
webhook delivery worker, bearer-auth middleware, MCP server starting
in v1.8.0c). Go module path moved to `github.com/mattjackson/basement`;
all internal import paths updated in one sweep; OpenAPI spec file
renamed; Docker image moved from `ghcr.io/mattjackson/basement-ui`
to `ghcr.io/mattjackson/basement`; `cmd/basement-server` keeps its
name (already correct since v0.x). License simultaneously switched
from MIT to AGPLv3 with a commercial-license escape hatch (contact
matthew@pq.io) for proprietary embedding / hosted SaaS scenarios.
Operators using Watchtower against the old image repo will stop
receiving updates until they update the image string; compose / k8s
manifests need the rename. Data dir (`BASEMENT_DATA_DIR`) and env
var names are unchanged. CI failed (`openapi/basement-ui.yaml`
still referenced by path); fixed in v1.8.0b.1 follow-on.

## v1.8.0a — 2026-05-22

`basement` CLI binary (later deleted in v1.8.0d). Originally planned
as a third surface alongside the web UI + (forthcoming) MCP server
for operator scripting against basement's control plane. Shipped
with subcommands for clusters / keys / regions / shares / syncs /
backups + a YAML config loader at `~/.config/basement/config.yaml`
+ `$BASEMENT_PROFILE` selection. The `internal/clilib` shared
package — config loader + HTTP client — was the durable artifact;
the CLI binary itself was deleted in v1.8.0d after audit found
substantial overlap with aws-cli (object CRUD) and the web UI
(control plane), but `internal/clilib` stayed and now backs the
v1.8.0c MCP server.

## v1.7.0 — 2026-05-22

Service accounts + webhooks milestone. Six primary cycles (v1.7.0a
→ v1.7.0f) plus four hotfix cycles (v1.7.0a.1 / a.2 / a.3 / a.4)
plus this milestone tag give basement its M2M auth substrate +
event-driven workload primitives end-to-end: long-lived `BMNT`-
prefixed bearer credentials scoped per-capability (the substrate
for v1.8's CLI + MCP server + Mobile PWA); HMAC-SHA256-signed
bucket-event webhooks with retry + auto-disable + Python
verification snippet; an in-process pub/sub that flips v1.6's
federation engine from 10s polling to sub-second convergence
(polling stays as fallback). No driver changes; no new env vars;
bearer auth runs parallel to the existing JWT session cookie.
Audit attribution distinguishes machine-bearer activity from human-
cookie activity at a glance (`actor=sa:{ID}` vs `actor=username`).
Two hydration-race hotfixes (v1.7.0a.3 + v1.7.0a.4) landed because
the milestone smoke gate caught a regression in v1.7.0a.1's
auto-elevation guard — same pattern as the v1.5.0c.1 routing hotfix
that the v1.5 smoke caught. Smoke 56/56 pass against live; 44
routes screenshot-verified including the new SA + webhook surfaces.

Full notes: [`docs/release-notes/v1.7.0.md`](docs/release-notes/v1.7.0.md)

### Cycles

- **v1.7.0a** — ServiceAccount data layer + admin API:
  `internal/serviceaccount` package, atomic JSON store, bcrypt-hashed
  secrets, `BMNT`-prefixed access keys, six admin endpoints under
  `/api/v1/admin/service-accounts` gated on `host:manage_users` with
  404-on-not-owner; soft delete preserves audit-greppability.
- **v1.7.0a.1** — UX hotfix: AdminEntryElevationGuard auto-elevates
  on `/admin/*` deep-link entry in USER mode + AdminUserModeBanner
  belt-and-braces persistent fallback. Closes URL-bar bypass.
- **v1.7.0a.2** — Drop-privileges UI cache invalidation fix:
  `handleDrop` now invalidates `["auth","me"]` after `setAuthMode`
  so the hydrator agrees with local state instead of fighting it.
- **v1.7.0b** — Bearer-auth middleware: `Authorization: Bearer
  AKID:SECRET` parallel to JWT cookie; cookie wins when both
  present; `policy.ServiceAccountAllows` AND's SA capabilities
  against scope envelope (SA bundle is floor + ceiling); audit
  attribution rewrites actor to `sa:{ID}`.
- **v1.7.0c** — Service-account admin UI: list page + mint route
  (page not modal) + shared `<SecretShownOnceDialog>` reused by
  Create + Rotate; capability picker domain-grouped + searchable;
  per-cap scope editor pre-fills sensible defaults.
- **v1.7.0d** — Webhook subscription type + delivery engine:
  `internal/webhook` package, atomic JSON store, HMAC-SHA256-signed
  delivery, 3-attempt 1s/5s/15s retry, 10-failure auto-disable,
  per-delivery + mutation audit, `POST /test` synthetic envelope.
- **v1.7.0e** — Webhook subscription UI: list + form (single route,
  not wizard) + detail with Python verification snippet (copy-
  pasteable into Flask / FastAPI receivers) + recent delivery
  history + Enable / Disable / Test / Delete actions.
- **v1.7.0f** — Federation event-driven via internal pub/sub:
  `webhook.Engine.Subscribe(name, cb)` exposes in-process pub/sub;
  `federation.Engine.SubscribeToEvents` bridges it;
  `ObjectCreated/Modified/Deleted` envelopes drive per-replica
  streamPut / DeleteObject in seconds; polling stays as fallback
  for backends without webhook source coverage.
- **v1.7.0a.3** — Smoke-caught regression: AdminEntryElevationGuard
  deferred until `useUser()` resolves (was firing during the post-
  nav hydration gap).
- **v1.7.0a.4** — Follow-on hotfix: guard also short-circuits when
  `user.mode === "admin" || "elevated"` directly off /auth/me — the
  AuthModeHydrator's setMode runs in a SUBSEQUENT render so the
  provider's mode is still the conservative default in the first
  render where user data arrives.

## v1.7.0a.4 — 2026-05-22

AdminEntryElevationGuard hydration-race hotfix follow-on. v1.7.0a.3
deferred the prompt until `useUser()` resolved — but the
AuthModeHydrator's `setMode` runs in a SUBSEQUENT render, not the
same render where user data first arrives. Within that first render
the provider's mode is still the conservative USER default, so the
guard fired anyway. Smoke against v1.7.0a.3 still saw the elevation
modal pop on every `/admin/*` goto, intercepting subsequent clicks.
Fix: also short-circuit when `user.mode === "admin" || "elevated"`
read directly off the `/auth/me` payload, side-stepping the
one-render gap between user-data-arrives and hydrator-runs-setMode.
One new test pins the new branch; 292/292 frontend tests pass.

## v1.7.0a.3 — 2026-05-22

AdminEntryElevationGuard hydration-race hotfix. The v1.7.0a.1 guard
read mode from AuthModeProvider, which defaults to "user" until
AuthModeHydrator syncs the actual mode from `/auth/me`. Every full-
page navigation to an admin route raced: the guard's `useEffect`
fired one tick before the hydrator updated mode to admin, popping
the elevation modal even for sessions already in ADMIN. The
milestone smoke gate caught this against v1.7.0e/f as three admin-
side clicks intercepted by the modal overlay. Fix: guard now defers
while `useUser()` is loading and skips the prompt when user data is
present. One new test pins the loading-state branch; 291/291
frontend tests pass.

## v1.7.0f — 2026-05-22

Federation event-driven replication via internal pub/sub. The
`webhook.Engine` gains a `Subscribe(name, cb) -> unsubscribe` API that
fires synchronously inside the dispatcher BEFORE per-webhook delivery,
so internal subsystems can react to bucket events without configuring
external HTTP webhooks. `federation.Engine.SubscribeToEvents` wires
the federation engine into that bus: when an envelope's
(regionId, bucket) matches a federation's primary, the engine queues a
single-object replicate task onto a per-federation buffered channel
that a dedicated worker drains. `ObjectCreated` / `ObjectModified`
envelopes drive a streamPut to each replica; `ObjectDeleted` envelopes
trigger a `DeleteObject` instead (new method on the replication-client
interface + on the production federationwire adapter). The 10s polling
tick continues as a fallback for backends without webhook-source
coverage — both paths share the same recordSuccess / recordFailure
helpers so the broken-after-3 / auto-failover semantics stay identical
across them. Saturated event channels drop the oldest task with a log
warning rather than blocking the dispatcher; the dropped task's
convergence falls back to the next polling pass. Wired in main.go
after both engines start. 7 new tests (4 federation:
event-driven-matches-primary, event-driven-ignores-non-primary,
event-driven-delete-propagates, polling-still-runs-alongside; 3
webhook: subscribe callback fires, unsubscribe stops, multi-subscriber
independence; plus a panic-safety case). With this cycle live, an
operator deleting an object via /files browser sees the delete
propagate to every federation replica within seconds instead of
waiting up to the 10s polling lag.

## v1.7.0d — 2026-05-22

Webhook subscription type + delivery engine. New `internal/webhook`
package (types + atomic-JSON store + delivery dispatcher) and
`/api/v1/user/webhooks` CRUD wire up bucket-event webhooks: an operator
declares "POST to this URL when an object is created / modified /
deleted in bucket X", supplies a shared HMAC secret (or lets the
server mint one), and the engine signs every outbound body with
`X-Basement-Signature: sha256=<hex>`. Retry policy is 3 attempts with
1s/5s/15s exponential backoff; ten consecutive failures auto-disable
the webhook + emit `webhook:auto_disabled`. Per-delivery audit
(`webhook:fired_success` / `webhook:fired_failure`) plus mutation
audit (`webhook:create/update/delete/test/enable/disable`). Secret
handling matches the v1.7.0a service-account mint-only pattern — the
cleartext is returned on Create + on rotated Update, redacted from
every List/Get response. `POST /test` emits a synthetic envelope so
operators can validate target + secret without waiting for real
traffic; the user-region object DELETE handler now fires a real
`object.deleted` event after a successful server-side delete. Engine
is robust to per-delivery panics (recover-shielded), saturated queue
drops the oldest envelope rather than blocking emit, and Stop drains
in-flight deliveries cleanly. Real-world coverage of create / modify
events lands with the v2.0 gateway; v1.7.0e brings the FE,
v1.7.0f hooks webhooks into federation. 21 new tests across the
package + handlers (store CRUD + ownership, engine retry/auto-disable/
filter/signature roundtrip/panic-safety, API mint + redaction +
rotation + enable/disable).

## v1.7.0a.1 — 2026-05-22

UX hotfix: auto-elevate on `/admin/*` entry + persistent fallback
banner. Closes the URL-bar bypass where landing on `/admin/clusters`
directly rendered the page in USER mode (PersonaPill: USER, URL: admin,
every destructive click 403'd with `ELEVATION_REQUIRED`). New
`AdminEntryElevationGuard` sits inside AppShell and opens the
elevation modal whenever the operator hits `/admin/*` in USER mode —
mirrors the UserMenu "Switch to admin view" behaviour, but for deep
links / bookmarks / manual URL entry. Cancel routes to `/files` with
an info toast (`"Cancelled — staying in user view"`); success leaves
the page in place with mode = ADMIN. A `useRef` latches the last
prompted pathname so navigation within `/admin/*` doesn't fire N
modals. A second new component, `AdminUserModeBanner`, renders as a
sticky amber banner whenever the operator is on `/admin/*` in USER
mode — belt-and-braces with the auto-prompt, distinct from the
v1.3.0a.4 `ElevationExpiredBanner` (which only handles the
falling-edge admin→user case). Carries "Elevate to admin" and
"Drop to /files" buttons. 15 new tests cover the guard's debounce,
cancel-toast-navigate path, success path, ADMIN passthrough, and the
banner's render rules + button wiring. ADR-0003 amendment section
appended with the operator quote and behaviour spec.

## v1.7.0c — 2026-05-22

Service-account admin UI. New `/admin/service-accounts` list page and
`/admin/service-accounts/new` mint route close out the v1.7.0a backend
with an operator-facing surface. The list table shows name, `BMNT`
access key, capability count, last-used relative time, expiry, and an
Active/Expired/Revoked pill in the order the v1.7.0b bearer middleware
checks them (revoked beats expired beats active). Per-row Rotate +
Revoke actions are dropdown-gated and run through the existing
elevation guard so a 403 pops the ADMIN-tier modal once + retries the
mutation on success. The mint flow is a route page (not a modal) per
the popups-max-2-fields doctrine — the capability picker is a
domain-grouped, searchable checkbox grid backed by the policy
registry's `usePolicies()` payload, with a per-capability scope
editor that pre-fills `host:*` / `cluster:*` / `bucket:{cid}:*` /
`key:{cid}:*` defaults so operators don't have to memorize the
six-form scope grammar from `validateServiceAccountScope`. Expiry
picks from "Never / 1 month / 6 months / 1 year / Custom date".
The shown-once dialog is a new shared
`<SecretShownOnceDialog>` reused by both create and rotate: lives in
`shared/ui/`, refuses dismissal on Escape / outside-click, gates the
Done button behind an acknowledgement checkbox, and surfaces four
copy paths — access key alone, secret (show/hide-toggled) alone,
the `Authorization: Bearer AKID:SECRET` header, and a
ready-to-paste `~/.aws/credentials` profile snippet. Five new
`useServiceAccounts`/`useServiceAccount`/`useCreate`/`useUpdate`/
`useDelete`/`useRotate` hooks join `shared/api/queries.ts` keyed on
`["admin", "service-accounts"]` for round-trip cache invalidation.
Admin user-menu gains a "Service accounts" link between Policies and
Audit log. 17 new component tests cover list rendering, status
collapse, capability filter, submit gating, mutation body shape,
clipboard interception, and the show/hide reveal toggle.

## v1.7.0b — 2026-05-22

Bearer-token authentication middleware for service accounts. The
auth middleware now tries (in order) the existing JWT session cookie,
then `Authorization: Bearer {AccessKeyID}:{Secret}` against the
v1.7.0a SA store, then falls back to `SESSION_REQUIRED`. Cookie wins
when both are present (an attacker who can set the Authorization
header still can't override an `HttpOnly` `Secure` cookie). Bearer
matches resolve the SA via `GetByAccessKey`, screen for
`SERVICE_ACCOUNT_REVOKED` + `SERVICE_ACCOUNT_EXPIRED` before the
bcrypt compare, then debounce-touch `LastUsedAt` and inject a
`Claims` whose `UserID` is the SA owner and whose new
`ServiceAccountID` field is the SA row ID. Policy gates branch on
`ServiceAccountID`: SA-authed requests route through a new
`policy.ServiceAccountAllows` pure-function that AND's the SA's
granted `Capabilities` list against its outer `Scopes` envelope —
the SA bundle is both floor and ceiling, the JWT user's role
assignments never apply. A missing capability returns 403
`ELEVATION_NOT_AVAILABLE` (distinct from `ELEVATION_REQUIRED` —
bearer tokens cannot elevate to ADMIN, so the FE must not render an
elevate CTA for M2M callers). Audit attribution rewrites the actor
field from the SA owner's username to `sa:{SA.ID}` so an operator
greppping audit can distinguish human cookie activity from machine
bearer activity at a glance. SigV4 (the v2.0 gateway path) is still
out of scope this cycle.

## v1.7.0a.2 — 2026-05-22

Drop-privileges UI sync fix. Clicking the × on the ADMIN persona
pill hit `/api/v1/auth/logout-elevation` (200 + new cookie with
mode=user) and called `setAuthMode({mode:"user", expiresAt:0})` —
but the countdown chip and ADMIN-amber styling stayed on screen.
Root cause: `AuthModeHydrator` syncs the server-reported mode off
the cached `["auth","me"]` React Query payload into the provider;
because the cache still held the pre-drop ADMIN response, the next
hydrator tick detected `current.mode !== user.mode` and snapped
the pill back. Fix: `handleDrop` now invalidates the
`["auth","me"]` query immediately after `setAuthMode`, so the
follow-up refetch reads the freshly-rotated cookie and the
hydrator agrees with local state instead of fighting it. Tests
pin (a) the invalidation call on success, (b) the pill
mode-flip + countdown removal in the rendered DOM, and (c) that
a backend rejection neither invalidates the cache nor flips the
pill.

## v1.7.0a — 2026-05-22

Service-account data layer + admin API. First cycle of the v1.7
service-accounts + webhooks milestone — substrate for v1.8 CLI/MCP
auth and v2.0 gateway SigV4 routing. New `internal/serviceaccount`
package with a bcrypt-hashed-secret atomic JSON store at
`{dataDir}/service_accounts.json`. Access keys carry a `BMNT` prefix
plus 16 random hex chars so they're greppable in audit logs.
Plaintext secret is generated server-side, returned exactly once on
Create + Rotate, never persisted or logged. Six admin endpoints
under `/api/v1/admin/service-accounts` (list / create / get / update
/ delete / rotate) gated on `host:manage_users` — cross-user GET /
PUT / DELETE collapse to 404 so the wire shape can't enumerate IDs
across owners. Delete is soft (RevokedAt) so audit greps can resolve
the access key back to a name + owner months after revocation. The
SigV4 verification middleware lands in v1.7.0b; the FE in v1.7.0c.

## v1.6.0 — 2026-05-22

Federation + multi-backend replication milestone. Six cycles
(v1.6.0a → v1.6.0f) plus this tag give basement its first
DR-grade story: a `FederatedBucket` is the same logical bucket
living on multiple backends, kept in lock-step continuously by a
polling-based replication engine, with manual + opt-in
auto-failover when the primary goes dark. Builds directly on
v1.5's sync engine — no driver changes; pure store + engine + API
+ UI additions. Six new user-tier endpoints under
`/api/v1/user/federated-buckets` (CRUD + `/failover` + `/resync`)
plus a `/by-target` reverse-lookup endpoint that powers the
bucket-browser federation badge. Atomic JSON store at
`{dataDir}/federated_buckets.json`. Per-federation goroutines tick
every 10s, diff primary against each replica, and queue missing
objects through a per-replica worker pool (default 4 workers) that
reuses the v1.5 sync engine as the copy primitive. Per-replica
health pills (`in-sync` / `lagging` / `stale` / `broken`) on the
detail page; `Promote to primary` is a confirmation Dialog
(two-fields rule). Opt-in auto-failover watchdog pings the
primary every 30s and promotes the healthiest replica after
`AutoFailoverSec` consecutive failures (audited as
`federation:failover` with `actor=system`). The bucket browser
surfaces a "Federated · N replicas, M in-sync" badge when a bucket
is part of a federation; clicks through to the federation detail.
No new env vars, no migrations, no breaking changes. Smoke 52/52
pass against live; 42 routes screenshot-verified including a
populated federation list + detail captured against an ephemeral
federation. This is the substrate for v2.0's S3 gateway: when the
gateway lands it routes inbound requests using the v1.6 federation
topology.

Full notes: [`docs/release-notes/v1.6.0.md`](docs/release-notes/v1.6.0.md)

### Cycles

- **v1.6.0a** — FederatedBucket data layer: `internal/federation`
  package with `FederatedBucket` + `ReplicaTarget` + `FederationPolicy`
  types, atomic JSON store, uniqueness on `(ownerUserId, name)`,
  `FindByTarget(regionId, bucket)` substrate for the v1.6.0e
  reverse-lookup endpoint.
- **v1.6.0b** — Replication engine: per-federation goroutines, 10s
  polling tick, per-replica worker pool (default 4 workers), health
  calc (`in-sync` / `lagging` / `stale` / `broken`),
  audit-per-object via `federation:replicate_object`.
- **v1.6.0c** — API endpoints: 6 user-tier endpoints under
  `/api/v1/user/federated-buckets` (CRUD + `/failover` + `/resync`),
  gated on USER auth, audited per ADR-0005. DELETE preserves replica
  data.
- **v1.6.0d** — Frontend: `/files/federated-buckets` list + 5-step
  wizard (Primary / Replicas / Policy / Initial-sync / Review) +
  detail page with per-replica health table + Resync now + Delete +
  Promote-to-primary confirmation Dialog.
- **v1.6.0e** — Bucket-browser federation badge + reverse-lookup
  endpoint `/by-target?regionId=X&bucket=Y`; `<FederationBadge>` on
  `/files/{regionId}/b/{bucketId}` clicks through to the federation
  detail page.
- **v1.6.0f** — Auto-failover watchdog (opt-in via
  `Policy.AutoFailover`); pings primary every 30s, promotes the
  healthiest replica after `AutoFailoverSec` consecutive failures;
  ranks replicas by `(health, lagBytes, lagObjects, lastSync)`;
  audited as `federation:failover` with `actor=system, reason=auto_watchdog`.

## v1.5.0 — 2026-05-22

Backup story milestone. Three cycles (v1.5.0a → v1.5.0c) plus the
v1.5.0c.1 routing hotfix give basement its own scheduled
bucket-to-bucket backup product end-to-end: named cron-scheduled
jobs, mirror + snapshot modes, GFS retention with auto-prune, and a
3-step restore wizard with snapshot-level deep-link. Backup runs
reuse the existing v0.8.x sync engine via a runner closure — no
duplication of pull semantics, no new copy code path. Six new
user-tier endpoints under `/api/v1/user/backups`, atomic JSON store
at `{dataDir}/backups.json`, panic-recovery in the scheduler so a
malformed cron expression can't down the goroutine. Mirror mode is
the default for back-compat with v1.5.0a records; snapshot mode
writes to `{dstBucket}/{slug(name)}/{YYYY-MM-DD_HH:MM:SS}/` and the
runner enumerates existing snapshots after every write to apply
GFS retention via the pure-function `PlanPrune` (17 table-driven
tests). The restore wizard short-circuits to a "Restore is only
available for snapshot-mode backups" notice for mirror records so
the operator never lands on a wizard with nothing to restore from.
No new env vars, no migrations, no breaking changes. Smoke 49/49
pass against live; 25 routes screenshot-verified.

Full notes: [`docs/release-notes/v1.5.0.md`](docs/release-notes/v1.5.0.md)

### Cycles

- **v1.5.0a** — Scheduled backup CRUD + cron engine
  (`internal/backup` package, `backup.Scheduler` wrapping robfig/cron
  with panic-recovery), 4-step wizard, detail page with run history.
- **v1.5.0b** — Snapshot mode + GFS retention (5-step wizard with
  Mode + retention step, `RetentionPolicy{KeepDaily, KeepWeekly,
  KeepMonthly}`, default `{7, 4, 12}`, auto-prune runner,
  `GET /user/backups/{id}/snapshots` for the detail page snapshot
  table).
- **v1.5.0c** — 3-step restore wizard with overwrite/skip semantics
  + per-snapshot `?ts=` deep-link from the detail page snapshot
  table. Mirror-mode short-circuit notice.
- **v1.5.0c.1** — Hotfix: `frontend/src/routes/files/backups/$id.tsx`
  renamed to `$id/index.tsx` so the restore route is actually
  reachable (v0.3.1-class parent-without-Outlet regression caught
  by the milestone smoke gate).

## v1.5.0c.1 — 2026-05-22

Routing hotfix for v1.5.0c. The backup detail page lived at
`frontend/src/routes/files/backups/$id.tsx` while the restore wizard
lived at `frontend/src/routes/files/backups/$id/restore.tsx`. TanStack
file-based routing treats this as a parent-with-children
configuration, but `$id.tsx` had no `<Outlet />`, so
`/files/backups/$id/restore` mounted under the detail content and
never displayed the wizard — same shape as the v0.3.1 cluster-detail
bug. Fixed by renaming `$id.tsx` → `$id/index.tsx` so both routes are
leaves under the `/files/backups` layout. routeTree.gen.ts
regenerated on build. The milestone smoke gate exercises both
surfaces (detail + restore) end-to-end and is now 49/49 green.

## v1.5.0c — 2026-05-22

Backup story, cycle 3: point-in-time restore. New endpoint
`POST /api/v1/user/backups/{id}/restore` (synchronous; request stays
open until the copy finishes; body
`{snapshotTimestamp, dstRegionId, dstBucket, dstPrefix?,
overwriteExisting}`; `snapshotTimestamp = "latest"` resolves at
request time). New route `/files/backups/$id/restore` — 3-step
wizard (pick snapshot / pick destination / confirm + run);
destination defaults to the backup's original source for one-click
in-place restore; `overwriteExisting` is off by default and the
confirm step surfaces a destructive-action warning when toggled on.
Result view renders in place of step 3 once the restore returns,
with per-object counts, bytes copied, started/completed timestamps,
and top-10 errors. Detail page's snapshot table gains per-row
"Restore →" deep-links that pre-fill the wizard via the `?ts=` search
param. Mirror-mode backups short-circuit to a "Restore is only
available for snapshot-mode backups" notice instead of mounting the
wizard.

## v1.5.0b — 2026-05-22

Backup story, cycle 2: snapshot mode + retention. v1.5.0a backups
shipped as a "continuous mirror" — each run overwrote the
destination. Real backups need point-in-time history. `Backup` gains
`Mode` (`mirror` | `snapshot`) and `Retention` (`KeepDaily` /
`KeepWeekly` / `KeepMonthly`); mirror is the default for back-compat
so existing records keep working untouched. Snapshot runs write to
`{dstBucket}/{slug(name)}/{YYYY-MM-DD_HH:MM:SS}/`; after the copy
lands the runner enumerates existing snapshots and applies
Grandfather-Father-Son retention via `PlanPrune` (pure function in
`internal/backup/retention.go`, 17 table-driven tests covering empty
input, multiple snapshots same day, gaps in days, ancient snapshots
beyond all buckets, GFS union, clock-skew defensive keep, dense
days, sort invariants). Default policy when none specified is
`{7,4,12}` — ~14 months of history with 23 stored snapshots.
`BackupResult` gains `SnapshotPrefix`, `SnapshotsPruned`,
`BytesReclaimed`. New endpoint
`GET /api/v1/user/backups/{id}/snapshots` returns the most recent 10
snapshots with object + byte counts. Wizard grows a 3rd step (Mode +
retention with the GFS defaults pre-populated); detail page shows
mode + retention summary, a snapshot table with "Browse →" links to
the destination bucket browser, and a "pruned" column on the run
history. Tag + main land at the same commit.

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
