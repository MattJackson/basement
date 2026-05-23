# Changelog

All notable changes to basement are recorded here. See the linked
release-notes files in `docs/release-notes/` for the full per-release
write-up; this file is the at-a-glance index.

## v1.13.0a — 2026-05-23

Pluggable-skins foundation (ADR-0008). First of four sub-cycles for
v1.13.0; lays the registry + the basement-default reference skin +
the `--basement-*` CSS variable layer so v1.13.0b can ship the
upload UI and v1.13.0c can ship the additional built-in skins +
typography / density / borderRadius / footer / loginHero wiring.

- **New package `internal/skin`** — `Skin` (8-element struct: name,
  display name, version, product name, assets, palette light+dark,
  typography, border radius, density, footer, login hero), `Registry`
  (`Register / Get / All` mirroring `internal/gateway.Registry`),
  `BuiltInDefault()` reference skin whose palette mirrors
  `frontend/src/index.css` exactly so visual diff against v1.12.x is
  zero.
- **New endpoint `GET /api/v1/skins`** — authenticated, not admin-only
  (the user-side skin picker landing in v1.13.0c needs the same
  payload). Returns the registry roster sorted alphabetically. 503
  `SKINS_NOT_WIRED` when the registry hasn't been wired (tests).
- **`OrgCapabilities` extension** — `ActiveSkin string` (default
  `"basement-default"`) + `SkinPolicy string` (one of `"default" |
  "locked" | "user-choice"`, default `"default"`). Legacy files
  without these fields read as the defaults; unknown policy literals
  clamp to `"default"` on both the read and write paths.
- **CSS variable wrapper layer** — `--basement-primary`, `--basement-bg`,
  `--basement-fg`, `--basement-muted`, `--basement-accent`,
  `--basement-destructive`, `--basement-warning`, `--basement-success`,
  `--basement-info` × light/dark. Additive (the shadcn `--color-*`
  layer is untouched) so a downgrade rolls back cleanly. Values
  mirror the current shadcn tokens 1:1 for warning/success/info
  defaults fill from sensible HSL tones.
- **UserMenu Theme submenu** — `Theme ▸ System / Light / Dark` radio.
  Replaces the standalone `ThemeToggle` button that used to sit in
  `AppShell` / `UserShell` headers. Per-user always, regardless of
  `skinPolicy` — brand identity doesn't dictate light/dark for an
  individual user.
- **Page-chrome ThemeToggle removed from shells** — both `AppShell`
  and `UserShell` headers are now brand-clean (PersonaPill +
  UserMenu only). `LoginForm` continues to mount the standalone
  toggle since it's the pre-auth surface with no UserMenu to host
  the submenu.
- **Hard constraints honoured** — basement-default's palette mirrors
  current Tailwind tokens exactly; UserMenu test extension verifies
  the three radio options + cookie persistence; the upload UI and
  additional skins are deliberately deferred to v1.13.0b.

Touched: `docs/adr/0008-pluggable-skins.md` (new), `internal/skin/`
(new package: `skin.go`, `registry.go`, `default.go`,
`registry_test.go`), `internal/store/org_capabilities.go`,
`internal/store/org_capabilities_test.go`, `internal/api/skins.go`
(new), `internal/api/skins_test.go` (new), `internal/api/server.go`,
`cmd/basement-server/main.go`, `frontend/src/index.css`,
`frontend/src/shared/ui/UserMenu.tsx`,
`frontend/src/shared/ui/__tests__/UserMenu.test.tsx`,
`frontend/src/shared/layout/AppShell.tsx`,
`frontend/src/shared/layout/UserShell.tsx`,
`frontend/src/shared/layout/__tests__/UserShell.test.tsx`,
`CHANGELOG.md`.

## v1.12.0b — 2026-05-23

Store-layer swap-and-save plumbing + OpenAPI regen for the v1.12.0a
deferred work. The CSK HTTP surface now actually migrates legacy
JWT-encrypted ConfigEnc blobs into CSK-encrypted ConfigEncCSK blobs
on first unlock, and the FE's type-generated client carries the five
new endpoints + three schemas.

- **`store.Connections.SwapClusterSecret(ctx, cid, oldEnc, newEnc)`** —
  atomic + idempotent per-record swap of the CSK ciphertext field.
  Bytes-equal guard rejects stale writes from racing concurrent
  unlocks (two admins migrating at once: the second is a silent
  no-op). Persists through the same `tmp+fsync+rename` pipeline as
  every other store mutation. Never touches the legacy `ConfigEnc`
  field — the JWT bridge stays recoverable until a future cycle
  retires it.
- **New on-disk schema field `configEncCSK`** alongside the existing
  `configEnc`. `load()` round-trips both; `saveLocked()` back-fills
  the in-memory cache with both freshly-computed values so the
  v1.12.0b migration helper sees the legacy ciphertext through a
  bare `Get`.
- **`maybeMigrateLegacyClusterSecret` wired** — on every successful
  unlock the helper reads `conn.ConfigEnc`, runs
  `clustersecret.MigrateFromJWTMap` (decrypt under JWT → validate
  JSON shape → re-seal under in-memory CSK), and calls
  `SwapClusterSecret(cid, conn.ConfigEncCSK, newBlob)`. First-time
  migrators see `migrated=true` and fire `cluster:csk_migrated`;
  raced second-callers see `migrated=false` (no double-audit). All
  errors log + leave on-disk state untouched so the next unlock
  retries cleanly — the bridge is never burned before the new path
  is verified.
- **`clusterRequiresLegacyMigration` wired** — `/lock-status` now
  reports `requiresMigration=true` exactly when the connection has
  a populated `ConfigEnc` AND an empty `ConfigEncCSK`. The FE's
  "first unlock will migrate" banner stops being a stub.
- **OpenAPI `openapi/basement.yaml`** — five new endpoints under
  `/admin/clusters/{cid}/`: `POST unlock`, `POST lock`,
  `GET lock-status`, `POST admins`, `DELETE admins/{adminUserId}`,
  plus three new schemas (`ClusterUnlockRequest`,
  `ClusterAddAdminRequest`, `ClusterLockStatus`). All carry
  `x-basement-since: '1.12'` so the FE's per-version gates work.
  `pnpm build` regenerates `frontend/src/shared/api/types.gen.ts`
  cleanly.
- **Tests** — `internal/store/connections_csk_swap_test.go` covers
  happy-path swap + on-disk shape + idempotent race + legacy
  preservation + not-found error. `internal/api/admin_cluster_
  secrets_migration_test.go` covers the unlock-driven round trip
  (legacy → CSK), the no-op-on-second-unlock case, and the
  `requiresMigration` flag flip.

Touched: `internal/store/connections.go`,
`internal/store/connections_csk_swap_test.go` (new),
`internal/api/admin_cluster_secrets.go`,
`internal/api/admin_cluster_secrets_migration_test.go` (new),
`internal/api/admin_clusters_test.go` (mock + SwapClusterSecret),
`internal/driver/registry_test.go` (mock + SwapClusterSecret),
`openapi/basement.yaml`, `frontend/src/shared/api/types.gen.ts`
(regen), `CHANGELOG.md`.

## v1.12.0a — 2026-05-23

Per-cluster envelope encryption (ADR-0007). Cluster admins set a
password that derives an Argon2id wrapping key (OWASP 2026: time=3,
memory=64MiB, threads=4); the wrapping key opens a random 256-bit
Cluster Secret Key (CSK) that protects the cluster's stored secrets.
Plaintext CSK lives only in process memory between unlock and lock or
restart.

- **New package `internal/clustersecret`** — `ClusterSecretManager`
  (Unlock/Lock/IsUnlocked/Encrypt/Decrypt/AddAdmin/RemoveAdmin/
  BootstrapFirstAdmin/MigrateFromJWT), `FileStore` (atomic JSON
  persistence under `{dataDir}/cluster_secrets.json`), `MemoryStore`
  for tests. Multi-admin: N wrappedCSK records per cluster (one per
  admin user ID), each independently rotatable. Restart = locked.
- **Five new HTTP endpoints** under `/api/v1/admin/clusters/{cid}/`:
  `POST unlock {password}`, `POST lock`, `GET lock-status`,
  `POST admins {adminUserId,password}`, `DELETE admins/{adminUserId}`.
  Gated on `cluster:edit` (status read uses `cluster:test`).
  First-admin POST bootstraps the CSK; subsequent admin adds require
  the cluster to be already unlocked.
- **423 LOCKED response code** — handlers that need a CSK-decrypted
  secret can call `requireUnlocked(w, cid)` to return 423 with
  `{cluster_id, hint}`. The FE intercepts this at the wire layer and
  pops the unlock modal, then retries the original call on success.
- **Frontend**: `LockBadge` in the cluster header, `ClusterEncryption`
  section on `/admin/clusters/{cid}` (enable / lock / unlock / add
  admin / remove admin), `UnlockClusterModal` mounted at the root via
  `ClusterUnlockProvider`. Password fields use `type=password` +
  `autocomplete="off"`.
- **Backup runner respects locked clusters** — a scheduled run whose
  source or destination connection lives on a locked CSK cluster
  skips and logs; the next scheduled run auto-resumes once an admin
  unlocks. No-op in practice for v1.12.0a (no Connection has migrated
  to CSK yet — the store-layer per-record swap lands in a follow-up
  cycle), wired now so the gate is correct the moment the migration
  completes.
- **Federation engine intentionally not gated** — federation resolves
  via per-user UserRegion secrets which stay on the JWT-derived path
  per ADR-0007 (different trust model, per-user not per-cluster).
- **Hard constraints honoured** — CSK plaintext never written to disk,
  never in audit metadata, never in logs. Audit events:
  `cluster:csk_first_admin_bootstrapped`, `cluster:csk_unlocked`,
  `cluster:csk_locked`, `cluster:csk_admin_added`,
  `cluster:csk_admin_removed`, `cluster:csk_migrated`. `defer
  mgr.LockAll()` in main.go zeros cached CSKs on graceful shutdown.
- **Migration safety** — legacy v1.0.0a JWT-encrypted ConfigEnc keeps
  working until first-unlock retries the swap. The bridge is never
  burned before the new path is verified; partial-failure leaves the
  legacy ciphertext in place for the next attempt.

Touched: `docs/adr/0007-per-cluster-envelope-encryption.md` (new),
`internal/clustersecret/` (new package: `clustersecret.go`, `store.go`,
`clustersecret_test.go`), `internal/api/server.go`,
`internal/api/admin_cluster_secrets.go`,
`internal/api/admin_cluster_secrets_test.go`,
`internal/api/backup_runner.go`, `cmd/basement-server/main.go`,
`frontend/src/main.tsx`, `frontend/src/shared/api/client.ts`,
`frontend/src/shared/api/queries.ts`,
`frontend/src/shared/auth/clusterUnlock.tsx` (new),
`frontend/src/shared/ui/UnlockClusterModal.tsx` (new),
`frontend/src/shared/ui/LockBadge.tsx` (new),
`frontend/src/routes/admin/clusters/$cid/index.tsx`, `CHANGELOG.md`.

## v1.11.0.21 — 2026-05-23

Gateways card general UX polish on `/admin/system`. The card had been
accumulating cruft across three prior cycles (sort order in `.19`,
stub-row copy in `.20`, intro rewrite in `.22`); this pass tunes the
whole card for visual coherence and lets the live row carry the
operator's attention.

- **Hero/stub visual split** — implemented gateways render inside a
  bordered, slightly-tinted panel (`border-input bg-muted/20 p-4`)
  with a heading + status pill + capability chips + a prominent mount
  URL with copy. Stubs render in a dashed-border, lower-opacity row
  with the protocol name + coming-soon badge + a single-line
  dot-separated capability list. The contrast carries the grouping
  on its own — the inter-row `<hr/>` separators are gone.
- **Status pill replaces StatusBlock prose** — the old block read
  *"Status: running, 0 active connections, last activity: —, total
  requests: 42, listen: …"* in one prose run-on. Replaced with a
  pill: green dot + **Active** + last-activity (or red dot +
  **Stopped** when not running). Power-user details (connection
  count, total requests) move into the pill's `title` attribute.
- **Capability chips reworked for stubs** — implemented row keeps
  the bordered monospace chip; stubs render the same list as a
  dot-separated inline run that takes less visual weight (`text-[11px]
  text-muted-foreground`). The redundant **"Capabilities:"** prefix
  is dropped on both — the row context already speaks for itself.
- **Connect hints tightened** — each platform clause is now a single
  imperative ("macOS: Finder ⌘K, paste mount URL.") instead of a
  full sentence. The `<details>` chrome is unstyled (no panel) so it
  sits flush with the rest of the row.
- **Base URL override demoted** — moved from a top-level labelled
  Input + paragraph hint into a collapsed `<details>` labelled
  "Advanced". Reverse-proxy setups are a power-user concern; default
  flow doesn't need it taking permanent vertical space.
- **Save button text shortened** — "Save WebDAV settings" → "Save".
  The row's heading already names the protocol.
- **Docs link shortened** — "WEBDAV integration guide →" → "Docs →".
  Same reasoning.
- **Mobile-first header** — title cluster + Enable toggle stack
  vertically on `<sm` viewports, side-by-side on `≥sm`. Mount-URL +
  Copy stack the same way. Save row stays footer-bottom on mobile.
- **Mount-URL input** — `onFocus={e => e.currentTarget.select()}` so
  clicking the read-only field hands the operator a ready-to-copy
  selection even when the clipboard API isn't available.
- **Test extension** — `gateways-card.test.tsx` gains four
  `v1.11.0.21 polish` assertions pinning the status pill copy
  (Active / Stopped), the simplified Save label, and the stub
  "not implemented yet" placeholder so a future cycle can't regress
  the polish silently.

Files touched: `frontend/src/routes/admin/system.tsx`,
`frontend/src/__tests__/gateways-card.test.tsx`, `CHANGELOG.md`.

## v1.11.0.17 — 2026-05-23

Mobile UI audit + inline fixes for the obvious phone-viewport bugs.

- **New `scripts/mobile-audit.ts`** — Playwright-driven mobile-quality
  audit. Walks every route (35 today) across 4 viewports (iPhone SE
  375×667, iPhone 14 390×844, iPad Mini 768×1024, Android narrow
  360×640) and runs per-route checks: horizontal scroll, tap-target
  size (44×44px floor on phones), sticky-header overlap, form labels,
  modal dismissibility, table-overflow, primary-nav reachability, and
  console errors. READ-ONLY — never POSTs/PUTs/DELETEs anything on the
  target deploy. Output: per-viewport screenshots in
  `/tmp/basement-mobile-{ts}/{viewport}/{route}.png` plus a markdown
  summary at `docs/mobile-audit-{YYYY-MM-DD}.md`. Wrapper:
  `scripts/mobile-audit.sh`.
- **First audit, baseline findings** — `docs/mobile-audit-2026-05-23.md`
  captures the v1.11.0.15 baseline against `basement.pq.io`. iPad Mini
  passes every route. Phone viewports surfaced 51 MAJOR horizontal-
  scroll findings (one per admin route, all the same root cause: the
  AppShell header) plus 100 MINOR tap-target findings for default
  shadcn Button + Input sizes (32px, below the 44×44 HIG floor).
- **AppShell horizontal-scroll fix** — admin header's left cluster
  (Logo + Buckets/Usage/Clusters nav) was pushing the right cluster
  (PersonaPill + ThemeToggle + UserMenu) ~230px off the canvas on
  every phone viewport. Mirrored the v1.8.0e UserShell pattern: nav is
  now horizontally scrollable (`overflow-x-auto whitespace-nowrap`,
  scrollbar hidden), left cluster gets `min-w-0 flex-1`, right cluster
  gets `flex-shrink-0`. Desktop visuals unchanged. Same fix in
  AppShell as `v1.10.0.1` already shipped in UserShell.
- **AppShell admin nav tap targets** — `Buckets / Usage / Clusters`
  links rendered at the bare text line-height (~20px), below the
  WCAG/iOS HIG 44×44 floor. Added `inline-flex items-center
  min-h-[44px]` to `NAV_LINK` (same fix shape as UserShell v1.10.0.1
  — desktop visual identical because the text is what the eye reads).
- **Button + Input default tap-target floor** — both base shadcn
  components rendered at h-8 (32px), generating the bulk of the MINOR
  tap-target findings. Added `min-h-[44px] sm:min-h-0` to the default
  + lg button sizes and to the icon variants, and to the Input
  component. Dense desktop UIs (table rows, toolbars) opt into the
  smaller xs/sm button sizes explicitly so they're untouched.
- **Service-account form labels** — `/admin/service-accounts/new` had
  two unlabeled inputs (Name + capability search). Added `htmlFor`/
  matching `id` on the Name label; added `aria-label="Search
  capabilities"` to the search input. Fixes mobile autofill + screen
  reader association.

Touched: `scripts/mobile-audit.ts` (new), `scripts/mobile-audit.sh`
(new), `docs/mobile-audit-2026-05-23.md` (new),
`frontend/src/shared/layout/AppShell.tsx`,
`frontend/src/components/ui/button.tsx`,
`frontend/src/components/ui/input.tsx`,
`frontend/src/routes/admin/service-accounts/new.tsx`, `CHANGELOG.md`.

Follow-up cycles: re-audit against basement.pq.io after this tag rolls
to confirm the AppShell + Button + Input fixes drop the MAJOR count
to ~0 on phone viewports; table-as-cards layout below 640px for
`/admin/audit`, `/admin/service-accounts`, `/admin/clusters/{cid}/layout`,
`/admin/usage` (currently horizontally scrollable inside the table
wrapper, but a card layout reads better on a phone); LoginForm-level
tap-target audit (now inherits from Button/Input fixes but its own
spacing could use a once-over).

## v1.11.0.15 — 2026-05-23

Delete-key post-nav fix + orphan `/admin/keys` route removed.

- **Bugfix: post-delete navigation lands on a real route** — deleting
  a key from `/admin/clusters/{cid}/keys/{id}` was redirecting to the
  global `/admin/keys` page, which no longer fits the per-cluster
  route model (and is now removed entirely — see below). The
  `useDeleteKey` mutation's `onSuccess` now navigates to the
  cluster detail page (`/admin/clusters/{cid}`), where the keys
  section shows the live post-delete list. Also invalidates the
  per-cluster keys query cache so the deleted row is gone on arrival.
- **Breaking: `/admin/keys` route removed** — both the backend
  endpoint (`GET /api/v1/admin/keys` cross-cluster aggregate) and the
  FE page (`/admin/keys` "Access keys" screen) are deleted. Keys are
  inherently per-cluster (Garage admin model); a flat global list
  strips that context and the route had no canonical home after the
  v1.11.0.3 per-cluster route split. Operators who bookmarked it now
  hit 404; the per-cluster keys list lives on the cluster detail
  page at `/admin/clusters/{cid}`. Scrubbed: AppShell nav item,
  UserMenu dropdown item, cluster-detail "View all →" links,
  smoke probes, OpenAPI paths, `AggregatedKeysResponse` +
  `KeyWithConnectionId` schemas, `useKeys`/`useKeysFlat` hooks,
  `listAllKeysHandler` + dead `listKeysHandler` Go handlers, fanout
  tests for the aggregate.
- **Per-cluster key detail "← Keys" back-link** now points at the
  owning cluster's detail page (`/admin/clusters/{cid}`) rather than
  the deleted global page; label reads `← Cluster`.

Touched: `internal/api/server.go`, `internal/api/admin_clusters.go`,
`internal/api/admin_keys.go`, `internal/api/admin_clusters_fanout_test.go`,
`openapi/basement.yaml`, `frontend/src/shared/api/mutations.ts`,
`frontend/src/shared/api/queries.ts`, `frontend/src/shared/layout/AppShell.tsx`,
`frontend/src/shared/ui/UserMenu.tsx`, `frontend/src/shared/ui/SecretShownOnceDialog.tsx`,
`frontend/src/routes/admin/clusters/$cid/index.tsx`,
`frontend/src/routes/admin/clusters/$cid/keys/$id.tsx`,
`frontend/src/routes/admin/keys/` (deleted),
`frontend/src/shared/layout/__tests__/AppShell-admin-user-redirect.test.tsx`,
`scripts/postdeploy-ui-smoke.ts`, `scripts/comprehensive-smoke.ts`,
`scripts/README.md`, `CHANGELOG.md`.

## v1.11.0.14 — 2026-05-23

Mutation cache-invalidation audit. Operator-reported bug: deleting a
UserRegion key on `/files/keys` left the row visible until a manual
page refresh. Root cause: `useDeleteUserRegion` succeeded server-side
but never called `queryClient.invalidateQueries({queryKey:["user","regions"]})`,
so the React Query cache that `useUserRegions` reads stayed warm with
the deleted row until `staleTime` (30s) expired or a window-focus
refetch fired. The `router.invalidate()` call on the caller side only
refreshes route loaders, not React Query state.

Same shape bug class existed across 33 other mutation hooks in
`frontend/src/shared/api/queries.ts` — every freshman who added a
mutation in the v1.x cycle forgot the `onSuccess: () => invalidate`
pattern that `mutations.ts` (cluster/bucket/key admin) follows.

- **Fix the operator's repro**: `useDeleteUserRegion` now invalidates
  `["user","regions"]` and removes the per-detail cache entry. After
  this tag deploys, delete an ephemeral `smoke-*` key on /files/keys —
  the row vanishes immediately, no manual refresh needed.
- **Audit + fix 33 additional mutations**: create/update/delete/state-
  change hooks for UserRegions (3), backups (4), syncs (4), shares (2),
  policies (4), OIDC mappings (1), invites (3), scrub (1), federation
  (4), service accounts (4), webhooks (6), bucket versioning (1), object
  versions (1), object lock (3), bucket encryption (2), lifecycle (1).
  Each now invalidates the matching list cache and any per-item detail
  cache the operator may be looking at.
- **Parameterised test pattern**: new
  `frontend/src/shared/api/__tests__/invalidation.test.ts` exercises
  the bug class — render hook, mutate, assert
  `queryClient.invalidateQueries` was called with the expected key.
  23 mutation-invalidation cases covering the highest-risk hooks
  (delete + state-change paths) so the bug class can't silently come
  back when a freshman adds a new mutation.
- **Test harness fix**: the v1.4.0b batch-delete fan-out test
  (`/files/$regionId/b/$bid` batch ops) asserted total `fetch` count;
  now that `useDeleteUserRegionObject` invalidates the bucket subtree
  on success, the listing refetch counts as extra fetches. The
  assertion now filters by `method === "DELETE"` to keep the focus on
  the fan-out itself, not the inevitable refetch.

Touched: `frontend/src/shared/api/queries.ts`,
`frontend/src/shared/api/__tests__/invalidation.test.ts` (new),
`frontend/src/routes/files/$regionId/b/__tests__/$bid.test.tsx`,
`CHANGELOG.md`.

## v1.11.0.12 — 2026-05-23

Tier 1 CI quality gates — every release ships through 10 automated
checks before tag push.

- **New `.github/workflows/quality.yml`** — runs on every push to
  main + every PR. Independent parallel jobs: golangci-lint, go vet,
  gosec, govulncheck, gitleaks, spectral (OpenAPI), eslint, tsc
  --noEmit. Each gate is warn-mode or block-mode per its baseline
  state; `docs/security-audit-baseline.md` tracks tightening per
  follow-up cycle.
- **`release.yml` extended with Trivy** — image vulnerability scan
  on Critical/High after the GHCR push. Warn-mode for the first
  cycle while the baseline is characterised; flips to block-mode
  next release.
- **`.github/dependabot.yml`** — weekly grouped updates for gomod,
  npm (frontend), docker, and github-actions ecosystems. One PR per
  ecosystem per week keeps the noise low.
- **`.github/workflows/lighthouse.yml`** + `.lighthouserc.json` —
  per-PR perf + a11y check against the live deploy on any
  `frontend/**` touch. Thresholds: performance ≥ 0.7, accessibility
  ≥ 0.9, best-practices ≥ 0.8.
- **`Makefile`** — `make quality` orchestrates the same gates
  locally. Also: `make lint`, `make vet`, `make test`, `make sec`,
  `make vulns`, `make frontend`, `make smoke`, `make smoke-full`,
  `make fuzz-audit`.
- **`.pre-commit-config.yaml`** — pre-commit hooks: whitespace,
  YAML/JSON validation, gitleaks, gofmt + go vet on commit, full
  `go test -race` on push. Install with `pre-commit install`.
- **`.gosec.yml`** — gosec configuration. Rule-level global
  suppressions (G104 unhandled errors, G304 file inclusion via
  variable, G404 weak random) are documented inline with rationale
  per-rule. Severity floor is `high`.
- **Axe-core a11y integration in `scripts/comprehensive-smoke.ts`**
  — every desktop screenshot pass now runs an axe-core audit and
  rolls violations into the final summary. Non-blocking (a11y is
  tracked over time, not gated). Loads `@axe-core/playwright` via
  dynamic import; the script still runs without it (graceful skip
  with install instructions). Install: `pnpm -C frontend add -D
  @axe-core/playwright`.
- **First fuzz test as starter pattern** — `internal/audit/
  filter_fuzz_test.go` exercises `matchFilter` + the Event-JSON
  parse path with adversarial inputs. Verified locally at 90k+
  execs/sec for 5+ seconds with zero panics. Pattern + add-a-target
  recipe documented in `CONTRIBUTING.md`.
- **`CONTRIBUTING.md` — test pyramid section** — documents the
  unit → integration smoke → UI smoke → fuzz → quality gates →
  pre-commit ladder, with the exact command per layer.
- **`docs/security-audit-baseline.md`** — operator-facing log of
  what each gate surfaces and the triage decision per finding.
  Locks down the contract that flipping any gate from warn to block
  requires updating this file.

Hard constraint honoured: pure CI/test-infrastructure cycle, zero
product-code changes. `go test -race ./...` green, `pnpm build`
green. See `docs/release-notes/v1.11.0.md` for the broader v1.11.x
narrative.

## v1.11.0.11 — 2026-05-23

README rewrite — value-first, badges, no comparison framing. See
git log for the full diff; CHANGELOG entry added retroactively in
v1.11.0.10 docs sweep.

## v1.11.0.10 — 2026-05-23

Documentation 100% up-to-date sweep. Docs-only cycle; no product
code touched.

- **Three new top-level docs**:
  - `docs/architecture.md` — component walkthrough (process model,
    package layout, request lifecycle, auth tiers, persistence,
    driver registry, federation engine, metrics + logging, gateway
    architecture, v2.0 outlook).
  - `docs/testing.md` — five-layer pyramid (Go + Vitest unit,
    docslint, feature-coverage smoke, comprehensive UI smoke,
    postdeploy UI smoke) + CI gates + per-change-type test guidance.
  - `docs/feature-matrix.md` — honest capability × driver table
    sourced from each driver's `Capabilities()` impl; calls out the
    Garage versioning / object-lock / SSE Unsupported branch and
    the open BUG01/03/04/05/06 driver-parity gaps.
- **`docs/configuration.md` rewrite** — removed the stale "Only
  `garage` supported in v1.0" claim; added the AWS S3 driver block;
  documented `BASEMENT_LOG_FORMAT`, `BASEMENT_METRICS_TOKEN`,
  `BASEMENT_ADMIN_PASSWORD`, `BASEMENT_OIDC_ELEVATION_PROMPT`;
  reframed the driver story around the v1.x-correct pattern (no
  `BASEMENT_DRIVER` required; add clusters via `/admin/clusters`);
  added the auto-bootstrap (v1.11.0c) walkthrough.
- **Integration stub honesty pass** — SMB / NFS / FTP stubs claimed
  v1.10 / v1.11 implementation slots that never landed; rewritten
  to reflect the v2.x line (SMB + NFS in v2.3, FTP in the v2.x
  long-tail) and to cross-link ADR-0006 for the v2 sketch. S3
  stub now cross-links ADR-0006 explicitly.
- **`SECURITY.md` env-var fix** — referenced
  `BASEMENT_ADMIN_INITIAL_PASSWORD` (does not exist); replaced with
  the actual surface (`BASEMENT_ADMIN_PASSWORD_HASH` /
  `BASEMENT_ADMIN_PASSWORD` + the v1.11.0c auto-bootstrap path) and
  cross-linked `deployment/docker.md`.
- **`docs/feature-smoke-bugs.md`** — added "Status as of v1.11.0.10"
  table; BUG02 marked FIXED in v1.11.0.5, BUG01/03/04/05/06 remain
  OPEN (v1.11.0.6 through v1.11.0.9 cycles did not land).
- **Forward-link chain across release notes** — added "Next release"
  pointers to v1.4.0 → v1.5.0 → v1.6.0 → v1.7.0 → v1.8.0
  (v1.0/v1.1/v1.2/v1.3/v1.9/v1.10 already had them). v1.11.0
  marked as current `v1.x` tip with a forward-link to ADR-0006
  (the v2.0 design proposal).
- **`docs/screenshots/SHOTLIST.md`** — added historical-document
  banner pointing at the current Playwright capture flow.
- **URL consistency** — normalized `MattJackson` →
  `mattjackson` in README + `docs/deployment/docker.md` so the
  install.sh URL matches the lowercase Go module path
  (`github.com/mattjackson/basement`).

Touched: `CHANGELOG.md`, `SECURITY.md`,
`docs/architecture.md` (new), `docs/testing.md` (new),
`docs/feature-matrix.md` (new), `docs/configuration.md`,
`docs/feature-smoke-bugs.md`, `docs/screenshots/SHOTLIST.md`,
`docs/integrations/{smb,nfs,ftp,s3}.md`,
`docs/release-notes/{v1.4,v1.5,v1.6,v1.7,v1.8,v1.11}.0.md`,
`docs/deployment/docker.md`.

## v1.11.0.8 — 2026-05-23

Real Garage v1 + v2 in CI — integration test infrastructure.

Pure test addition; no product code changes. The driver bug class
that surfaced repeatedly across v1.11.0.1 / v1.11.0.2 / v1.11.0.5
BUG02 and the federation no-op replication bug v1.11.0.4 all share
the same root cause: the unit suite uses fakes that honour the
documented interface but don't reproduce real-Garage wire semantics
(grant-permission wire shape, second-precision object mtimes, the
admin-only ADR-0001 connection shape). Every one of those bugs
would have been caught BEFORE deploy by an integration suite that
talks to actual Garage containers.

- **New `internal/drivers/garagetest` package** — testcontainers-go
  bootstrap helper. `Bootstrap(t, V1|V2)` spins up a single-node
  Garage cluster, mounts an in-memory `garage.toml`, waits for the
  admin API, stages + applies a single-node layout, returns a
  `*Cluster` with `AdminURL`/`AdminToken`/`S3Endpoint` ready to wire
  into a driver. Talks to the cluster's admin API directly (raw HTTP)
  for bootstrap — the driver under test never participates in its
  own setup. Auto-skips on hosts without Docker so a local
  `go test -tags=integration ./...` on a no-Docker laptop produces
  a clean "skipped" rather than a noisy error.
- **`internal/drivers/garage/driver_integration_test.go`** —
  replaces the env-var skeleton with four real tests covering the
  v1.11.0.x bug classes against `dxflrs/garage:v2.0.0`:
  admin-only driver builds + ListBuckets (v1.11.0.1), bucket ID
  round-trip vs cluster's view (v1.11.0.2),
  AllowBucketKey+GetKey grant readback (v1.11.0.5 BUG02), and a
  Health+ListNodes smoke.
- **`internal/drivers/garage_v1/integration_test.go`** — parallel
  suite against `dxflrs/garage:v1.0.1` honouring the driver-parity
  doctrine. Every regression test the v2 driver carries also pins
  the v1 driver's behaviour, so a future refactor can't quietly
  recreate the v2 BUG02 mistake in the v1 code path.
- **`internal/federation/integration_test.go`** — end-to-end engine
  test against TWO Garage v2 clusters. Boots the engine, sleeps
  past the boot tick so LastSync gets set, uploads an object to
  the primary, then asserts the replica receives it within 15s.
  Direct repro for v1.11.0.4 — the bug only surfaced because real
  Garage returns whole-second mtimes while the fakeDriver records
  nanoseconds.
- **`.github/workflows/integration.yml`** — three CI jobs (Garage
  v2 driver, Garage v1 driver, federation engine) gated to paths
  that could plausibly break a real-Garage interaction. Concurrency
  group cancels stale runs per branch.
- **`Makefile`** — new `make integration` runs all three suites
  locally. Docker required.
- **`CONTRIBUTING.md`** — "Integration tests" section documents
  the build tag, the local-run command, and the CI workflow.

Tests stay behind `//go:build integration`, so `go test -race ./...`
(the CONTRIBUTING.md "ready to merge" gate) is unchanged and still
Docker-free. The new `testcontainers-go` dependency is only pulled
into the build under `-tags=integration`. `pnpm build` clean;
`go test -race ./...` green across all packages.

Touched: `go.mod`, `go.sum`, `Makefile` (new), `CONTRIBUTING.md`,
`CHANGELOG.md`, `.github/workflows/integration.yml` (new),
`internal/drivers/garagetest/garagetest.go` (new),
`internal/drivers/garage/driver_integration_test.go` (rewritten),
`internal/drivers/garage_v1/integration_test.go` (new),
`internal/federation/integration_test.go` (new).

## v1.11.0.9 — 2026-05-23

Federation engine replicate-timeout fix. v1.11.0.4 fixed the LastSync
diff-filter bug but uncovered a second issue: the per-federation
goroutine had NO timeout on `replicateBatch` / `PutObjectStream`. When
the primary held substantial objects (multi-MB files, many objects),
the boot tick blocked indefinitely inside the per-replica worker
pool's `wg.Wait`. Every subsequent ticker.C send was lost because
the `for-select` in `runFederation` never returned to consume them —
the federation appeared dead from the operator's side: `updatedAt`
frozen at boot-tick time, manual `/resync` calls a no-op, the engine
appeared alive (other federations ticked) but THIS federation stuck.

Landed after v1.11.0.10 / .11 / .12 in wall-clock order; the
v1.11.0.10 docs sweep noted "v1.11.0.6 through v1.11.0.9 cycles did
not land" — this entry corrects that for v1.11.0.9.

Fix:

- **Per-object timeout (`ObjectReplicateTimeout = 60s`)**. Each
  per-object replicate gets `context.WithTimeout(ctx,
  ObjectReplicateTimeout)`. Long enough for ~100 MB at a modest WAN
  link, short enough that a pathological single-object hang can't
  permanently strand a federation. Cancelled objects record a failure
  and the next tick re-attempts.
- **Per-tick deadline (`TickDeadline = MaxBatchPerTick * 10s = 1000s`)**.
  The whole tick (compute diff + replicate batch + record health)
  is wrapped in a deadline-bounded ctx. When the deadline fires the
  in-flight per-object ctxs see Done(), workers exit, replicateBatch
  returns the partial counters, the for-select unblocks, and the
  next scheduled tick fires on schedule.
- **Engine-level cancellation context**. `Start` creates an engine
  ctx that `Stop` cancels FIRST (before closing quit channels) so
  in-flight PutObjectStream / DeleteObject calls on well-behaved
  drivers exit promptly during shutdown rather than blocking on
  their per-object 60s timeout.
- **Stop grace period (`StopGracePeriod = 10s`)**. `Stop` no longer
  hangs indefinitely waiting for `loops.Wait` + `inflight.Wait`.
  Caps the drain at 10 seconds per phase; logs at Warn with the
  count of abandoned work when a wedged driver ignores ctx and the
  grace expires. The leaked goroutines exit naturally when their
  upstream connection times out — or when the process exits.
- **`healthRank` now includes `HealthPending`**. Pending sorts
  between in-sync and lagging in the federation summary so a fresh-
  but-unverified replica surfaces as yellow ("pending") in the
  computedHealth column rather than silently inheriting "in-sync".
  Per the operator's v1.11.0.4 cycle note.

Tests pinned:

- `TestFederationEngine_ReplicateTimesOut` — wedged PutObjectStream
  cancellation by per-object timeout; replica health flips off pending
  to lagging.
- `TestFederationEngine_TickContinuesAfterStuckBatch` — boot-tick
  wedge resolves; second scheduled tick fires (proves the for-select
  unblocks after replicateBatch returns).
- `TestFederationEngine_StopDoesNotHangOnBlockedReplicate` — ctx-
  ignoring driver leaks but `Stop()` returns within the grace bound.
- `TestEngine_StopCancelsInflight` — well-behaved driver exits on
  ctx-cancellation propagated from `Stop`; replaces the old
  `TestEngine_StopWaitsForInflight` which pinned the now-incorrect
  "Stop blocks until in-flight finishes" contract.
- `TestHealthRank_PendingBetweenInSyncAndLagging` +
  `TestToFederatedBucketResponse_PendingSurfacedInSummary` — pin the
  pending-yellow-signal rank ordering at the API layer.

Touched: `internal/federation/engine.go`,
`internal/federation/engine_test.go`,
`internal/api/user_federated_buckets.go`,
`internal/api/user_federated_buckets_test.go`, `CHANGELOG.md`.

## v1.11.0.5 — 2026-05-23

Feature-coverage smoke + Garage v2 driver bugfix.

- **New `scripts/feature-smoke.ts`** — per-feature functional smoke
  against test backends only (10x Garage v2 on 10.1.7.11:38xx).
  Exercises every product feature A-O (cluster + key + bucket basics,
  UserRegions, presign, multipart, backups, webhooks, service
  accounts, lifecycle, versioning/object-lock/SSE stub paths, WebDAV,
  audit, onboarding). Hard safety gate: every destructive op asserts
  the target cluster label starts with `garage-v2-test-`; operator's
  `classe` cluster and `lsi`/`cheshire` regions are deny-listed and
  baseline-snapshotted to catch any drift. Ephemeral resources use
  the `feat-smoke-{ts}-{rand}-` prefix; cleanup runs in finally with
  a broad-sweep pre-flight reaper for leftovers from prior runs.
  Run via `bash scripts/feature-smoke.sh`. 46 checks, 0 failures
  against the live deploy.
- **Bugfix: Garage v2 driver dropped per-bucket key permissions on
  every GetKey** — `getKeyInfoResponse.BucketsPermissions` was typed
  as `bucketPermissionResp` (flat `read/write/owner`), but the
  Garage v2 `GetKeyInfo` wire shape nests them under
  `permissions: {read, write, owner}` (KeyInfoBucketResponse,
  garage-admin-v2.json:3490-3527). `keyFromInfo` therefore returned
  all-false flags on every grant readback after `AllowBucketKey`
  succeeded — which silently routed every downstream call signed
  with the affected key into 401 `USER_KEY_REJECTED` against the
  backend. Caught by the new feature smoke. Fix: switch the field
  to `[]keyInfoBucketResponse` and read `b.Permissions.Read/...`.
- **Bug report at `docs/feature-smoke-bugs.md`** — every failure /
  warning from the smoke run, tagged with a `BUG##` ID, per-feature
  pass/fail counts, and a follow-up table for the bugs that aren't
  trivial enough to fix inline this cycle (bucket-rename via PATCH,
  `/admin/clusters/{cid}/driver-info` endpoint missing, multipart
  abort handler not passing the object key, snapshots list 500 on
  fresh snapshot-mode backup, WebDAV PROPFIND blocked by edge).

Touched: `scripts/feature-smoke.ts` (new), `scripts/feature-smoke.sh`
(new), `internal/drivers/garage/keys.go`,
`internal/drivers/garage/keys_test.go`,
`docs/feature-smoke-bugs.md` (new), `CHANGELOG.md`.

## v1.11.0.4 — 2026-05-23

Federation engine no-op replication fix. Caught by hands-on Garage v2
testing against basement.pq.io: the polling tick ran (lag gauges
advanced, tick logs fired) but `federation:replicate_object` audit
events never emitted and `basement_federation_replicate_total` stayed
absent. Replicas stayed empty while the health endpoint falsely
reported "in-sync".

Root cause: the LastSync filter in `computeDiff` compared an S3-style
object's whole-second `LastModified` against a nanosecond-precision
`replica.LastSync`. After the engine's boot tick set LastSync to a
post-second-boundary nanosecond, any object uploaded the same second
landed with `LastModified == LastSync.Truncate(time.Second)` —
`LastModified.After(LastSync)` returned false and the engine
permanently skipped the object.

Fix:

- `LastSyncSlack = 2 * time.Second` subtracted from `replica.LastSync`
  before the `obj.LastModified.After(...)` comparison. Survives both
  whole-second mtimes and modest clock skew while preserving the
  steady-state HEAD-skipping optimisation for genuinely older objects.
- `HealthPending` state added: `ComputeHealth(zero, ...)` now returns
  pending instead of in-sync so the FE doesn't lie before the engine
  has actually verified the replica.
- `recordSuccess` no longer unconditionally flips to in-sync on an
  empty-diff tick: a truncated scan now returns lagging so the
  operator sees the replica isn't fully verified.
- Structured slog (`Info`) at tick start, diff computation and batch
  replicate so live-deploy debugging can correlate engine behaviour
  with the audit log without log-format guesswork.

Tests:

- `TestEngine_PollingReplicatesObjectAfterEmptyBootTick` — pins the
  exact bug repro (boot tick → upload at whole-second mtime → assert
  replica receives object + `federation:replicate_object` audit fires).
- `TestEngine_FreshReplicaReportsPending` — covers the new
  HealthPending semantic.
- `TestEngine_HealthCalculation` updated for zero-LastSync → pending.

## v1.11.0 — 2026-05-23

Milestone release closing the v1.11 launch-readiness arc. See
[`docs/release-notes/v1.11.0.md`](docs/release-notes/v1.11.0.md) for
the full write-up.

Summary of what shipped across the v1.11.0a -> v1.11.0f + v1.11.0.1
sub-cycles:

- **First-run onboarding wizard** (`v1.11.0a`) — five-step
  Welcome -> Cluster -> OIDC -> Team -> Done wizard at
  `/admin/first-run`, Skip on every step, dismiss latch on the
  welcome card; AppShell auto-routes admin entries when
  `needsOnboarding && !completed`; upgrade-safe (existing deploys
  auto-promote to `completed=true`).
- **Production deployment guide** (`v1.11.0b`) — new
  `docs/deployment/` directory with README + docker + reverse-proxy
  + tls + hardening + backup-basement + upgrade docs covering the
  full path from `docker run` to production posture.
- **5-minute install** (`v1.11.0c`) — auto-bootstrap of
  JWT secret + admin password on first boot;
  `INITIAL ADMIN PASSWORD` printed to stdout; persisted to
  `{DATA_DIR}/.jwt-secret` + `{DATA_DIR}/.initial-admin-password`
  (0600); new `scripts/install.sh` curl-pipe-bash one-liner.
  Zero behavioural change for operators with explicit env.
- **Trust + credibility docs** (`v1.11.0d`) — `SECURITY.md`,
  `CONTRIBUTING.md`, `DCO.md`, `.github/ISSUE_TEMPLATE/`,
  `PULL_REQUEST_TEMPLATE.md`, plus a CycloneDX SBOM GitHub Actions
  workflow published per release tag.
- **Screenshots gallery + README polish** (`v1.11.0e`) — 15-shot
  Playwright-driven capture under `docs/screenshots/v1.10/` plus
  a 2x4 README embed; README compressed from a 60-bullet feature
  wall into a 13-row table; new Quickstart code-block matching
  v1.11.0c auto-bootstrap.
- **Observability — Prometheus exporter + Grafana + alerts +
  slog** (`v1.11.0f`) — `/metrics` endpoint with 14 metric
  families; `docs/observability/` with Grafana dashboard +
  alert rules; `BASEMENT_LOG_FORMAT=json|text` slog handler.
- **Garage v2 admin-tier connection hotfix** (`v1.11.0.1`) —
  caught by real-Garage-v2 testing during the v1.11 cycle.

No breaking changes; no driver interface changes; no data
migrations. New env vars: `BASEMENT_METRICS_TOKEN`,
`BASEMENT_LOG_FORMAT`, `BASEMENT_ADMIN_PASSWORD` (all optional).

## v1.11.0e — 2026-05-23

Screenshots gallery + README polish. Closes the v1.11
launch-readiness gap on the README front so visitors landing on
the repo see what they're getting before they install.

- **Capture script** (`scripts/capture-v1.10-screenshots.ts`) —
  Playwright-driven walk against a live deploy using the same
  fetch-based auth pattern as `scripts/comprehensive-smoke.ts`.
  Discovers a real region + bucket + cluster + federation +
  backup for the route walks; mints one clean ephemeral
  service-account for the SA list + MCP section shot. Shots
  7 / 8 / 9 / 10 fall back to a Playwright-rendered static-HTML
  mock when the live target (Garage-only) doesn't support the
  underlying primitive; mocked shots end in `-mocked.png` and
  embed an explicit disclaimer naming the production component.
- **Output** (`docs/screenshots/v1.10/`, 15 PNGs) — clusters
  list, bucket browser (desktop + mobile), three compliance
  sections (Versioning / Object Lock / Encryption), object
  versions panel, federation detail + wizard, backup detail,
  service accounts list, MCP config dialog, Gateways card,
  policy matrix, audit log.
- **Gallery index** (`docs/screenshots/README.md`) — per-shot
  table with file name + description + source (live / mock);
  documents the `-mocked.png` convention; re-capture command +
  env var overrides.
- **README polish** — "Why basement" trimmed to a 5-sentence
  pitch; new Quickstart code-block matching v1.11.0c auto-
  bootstrap; new Screenshots section embedding 8 of the 15 PNGs
  with one-line context; Features compressed from a 60-bullet
  wall into a 13-row table with per-release-notes links; new
  "What's next" section naming v1.11 launch-readiness +
  v2.0 S3 gateway + v2.x sketch; CONTRIBUTING / SECURITY links.
- **Tests** (`internal/docslint/screenshots_test.go`) —
  TestScreenshotsIndex (README headings + capture script +
  v1.10/ refs + `-mocked.png` explanation), TestV110GalleryFiles
  (each of the 15 expected shots exists, accepts live or mocked
  variant), TestReadmeReferencesGallery (README image embeds +
  index link can't silently rot).

Touched: `scripts/capture-v1.10-screenshots.ts` (new),
`docs/screenshots/v1.10/` (15 PNGs new), `docs/screenshots/README.md`
(new), `README.md`, `internal/docslint/screenshots_test.go` (new),
`CHANGELOG.md`.

## v1.11.0f — 2026-05-23

Observability cycle: basement now exposes a fixed-set Prometheus
exporter, a drop-in Grafana dashboard, a starter alert ruleset, and
runs all server logging through `log/slog` with configurable JSON or
text output.

- **`/metrics` endpoint** publishes 14 metric families covering HTTP
  request rate + latency histogram, auth attempts, audit volume,
  federation replicate + per-replica lag, backup runs + last-success
  timestamp, webhook delivery outcomes, active sessions, service
  accounts, buckets, objects, and build info. Defaults to
  unauthenticated (standard Prometheus convention); a bearer-token
  gate activates when `BASEMENT_METRICS_TOKEN` is set.
- **Audit -> Prometheus bridge** wraps the existing `audit.Logger` so
  every event flowing through the audit pipeline (`auth:login`,
  `federation:replicate_*`, `backup:run_*`, `webhook:fired_*`) drives
  the corresponding Prometheus counter automatically — no call-site
  instrumentation required. The on-disk audit log is unchanged.
- **`docs/observability/`** ships `grafana-dashboard.json` (ten-panel
  drop-in dashboard), `prometheus-alerts.yml` (six rules:
  BasementDown, BasementHighAuthFailureRate, BasementFederationLagHigh,
  BasementBackupFailed, BasementBackupOverdue, BasementWebhookFailureRate),
  and a README with scrape config + import steps + metric reference.
- **`BASEMENT_LOG_FORMAT`** switches the slog handler between `json`
  (default; one parseable object per line, ready for log aggregators)
  and `text` (key=value for local dev). Invalid values rejected at
  boot.
- **`basement_build_info`** stamped from `version.Version` +
  `version.Commit` at boot, giving operators a stable "is basement
  alive" signal and the basis for the `BasementDown` alert.

Tests: new unit tests cover (a) every metric family surfaces on
`/metrics`, (b) bearer-token gate accepts/rejects correctly, (c) the
HTTP middleware records counter + histogram samples, (d) backup
success runs update the last-success gauge, (e) the audit collector
fans out auth/webhook/federation/backup events to their specialty
counters, (f) `/metrics` returns 503 when the collector isn't wired,
(g) bad-cred login bumps `auth_attempts_total{result="failure"}`
through the wired audit collector, (h) `BASEMENT_LOG_FORMAT` parses
and validates, (i) JSON slog output is parseable and text slog
output isn't accidentally JSON-shaped.

Touched: `internal/metrics/prometheus.go` (new),
`internal/metrics/audit_collector.go` (new),
`internal/metrics/prometheus_test.go` (new),
`internal/api/server.go`, `internal/api/prometheus_endpoint_test.go`
(new), `internal/config/config.go`, `internal/config/config_test.go`,
`internal/serviceaccount/store.go`, `cmd/basement-server/main.go`,
`cmd/basement-server/main_test.go` (new),
`docs/observability/README.md` (new),
`docs/observability/grafana-dashboard.json` (new),
`docs/observability/prometheus-alerts.yml` (new), `CHANGELOG.md`.

## v1.11.0d — 2026-05-22

Trust + credibility docs cycle. Self-hosters evaluating basement
care about disclosure policy, contribution terms, and supply-chain
transparency before they will commit to a control plane that holds
their KMS key IDs and S3 secrets. This cycle ships the
GitHub-canonical files so a first-time visitor can answer "how do
I report a vuln, how do I contribute, what is the supply-chain
story" without reading source:

- **`SECURITY.md`** at the repo root. GitHub auto-detects and
  surfaces this in the Security tab. Documents the security contact
  (`matthew@pq.io`), best-effort 48-hour initial-response SLA,
  supported-versions policy (current minor + previous minor),
  90-day responsible-disclosure window, and an accurate threat
  model that names what basement actually trusts (`DATA_DIR`,
  `JWT_SECRET`, admin password, OIDC discovery) versus what it
  does not (backend HTTP responses, backend audit truth, backend
  permissions as policy). Crypto claims are concrete and tied to
  the implementing files: per-user S3 secrets + cluster admin
  tokens are AES-256-GCM with key derived as `sha256(JWT_SECRET)`
  (`internal/store/crypto.go`), local passwords are bcrypt cost-12
  (`internal/auth/bcrypt.go`), service-account secrets are bcrypt
  of the secret half (`internal/serviceaccount/store.go`). Also
  notes what we explicitly do NOT obscure (KMS key IDs are
  plaintext — they're public identifiers, not secrets) and what we
  do NOT log (object contents, plaintext passwords, post-mint
  bearer secrets).
- **`CONTRIBUTING.md`** at the repo root. AGPL-3.0 contribution
  license terms; commercial dual-licensing path retained by the
  maintainer with DCO sign-off (rather than copyright assignment)
  as the lightweight enabler; local dev loop
  (`git clone` → `pnpm install` → `pnpm build` → `go test -race
  ./...`); code style (`gofmt`, `golangci-lint`, `prettier`,
  `eslint`); PR process (branch → tests + sign-off → review →
  merge). Driver-contribution section is explicit about the
  parity doctrine (advertise unsupported honestly, do not fake
  capability flags).
- **`.github/DCO.md`**. Standard Developer Certificate of Origin
  v1.1 text with a project-specific addendum tying the sign-off
  to the maintainer's commercial relicensing rights, so a
  contributor knows up front what they're agreeing to. `git commit
  -s` how-to + `git rebase --signoff main` for back-fill.
- **`.github/ISSUE_TEMPLATE/`** with four GitHub Issue Forms
  (`bug_report.yml`, `feature_request.yml`, `question.yml`,
  `security.yml` — the last is a tiny redirect form whose only
  purpose is to push reporters back to SECURITY.md before they
  publish a vuln in the open) plus `config.yml` setting
  `blank_issues_enabled: false` so every report lands in a
  template. Bug + feature forms list all four drivers as
  multi-select dropdowns so triage gets routed correctly.
- **`.github/PULL_REQUEST_TEMPLATE.md`** with four required
  sections (Summary, Test plan, Linked issues, DCO sign-off) and
  reviewer-notes scratch space. Test-plan checklist nudges
  contributors to actually run `go test -race ./...` + the FE
  build before opening the PR.
- **`.github/workflows/sbom.yml`**. On every tag push, generates
  both CycloneDX-JSON and SPDX-JSON Software Bill of Materials
  for the source tree using
  [syft](https://github.com/anchore/syft), attaches them as
  release artifacts via `softprops/action-gh-release`. Best-effort
  scan of the published Docker image is included as a
  `continue-on-error` step (the parallel `release.yml` may not
  have published the image yet when the SBOM job runs). Independent
  workflow + independent failure surface — a syft regression does
  not block the Docker image publish.

Tests: new `internal/docslint/` package with table-driven Go tests
asserting every new file exists, contains the substantive content
(SLA, threat model, AGPL terms, DCO clauses (a)-(d), all four
drivers in the bug-report dropdown, CycloneDX + SPDX in the SBOM
workflow), and cross-links the other files consistently. The test
walks up from `os.Getwd()` to find `go.mod`, so it works whether
you `go test ./internal/docslint/...` from the repo root or from
elsewhere. No FE / Go source code changed; `go test -race ./...`
+ `pnpm build` green.

## v1.11.0c — 2026-05-22

**5-minute install.** Bootstrap path so `docker run` against the
default image with no env vars at all comes up working, prints the
auto-generated admin password to stdout, and lands the operator on
the dashboard. Three deliverables in one cycle:

- **Auto-bootstrap on first boot.** Three env vars become optional:
  `BASEMENT_JWT_SECRET` auto-generates 32 random bytes on first boot
  and persists hex-encoded to `{DATA_DIR}/.jwt-secret` (0600) so the
  secret survives container restarts. `BASEMENT_ADMIN_PASSWORD_HASH`
  auto-generates a 24-char random password (no ambiguous chars),
  bcrypt-hashes it, prints `INITIAL ADMIN PASSWORD: <pw>` to stdout
  for `docker logs basement | grep` retrieval, and persists the
  plaintext to `{DATA_DIR}/.initial-admin-password` (0600) so the
  operator can recover it after the log line scrolls off. New
  `BASEMENT_ADMIN_PASSWORD` (plaintext) convenience env var:
  bcrypt-hashes at boot, never persists plaintext. `BASEMENT_DRIVER`
  is now optional too — when unset, basement boots without a default
  cluster wired and the operator adds one via `/admin/clusters`.
  Bootstrap is fully idempotent: explicit env vars short-circuit
  every auto-generation path with zero behaviour change, and an
  existing `.jwt-secret` / `.initial-admin-password` file is reused
  rather than rotated.

- **`scripts/install.sh` — one-liner installer.** `curl -sSL .../install.sh | bash`
  detects Docker, picks a writable install directory (`./basement/`
  or `/opt/basement/`), generates a minimal `docker-compose.yml`,
  pulls `ghcr.io/mattjackson/basement:latest`, starts the container,
  tails logs until the listening line, then prints the
  auto-generated admin password in a banner. Idempotent — re-running
  pulls + restarts without touching the data volume so the password
  the operator saw on first install still works. POSIX shell + bash;
  no jq/yq/python dependency.

- **Docs: 5-minute README + auto-bootstrap section in
  `docs/deployment/docker.md`.** README quickstart shrinks to three
  commands (`docker run`, `docker logs | grep`, open browser). The
  v1.11.0b `docs/deployment/docker.md` gains an Auto-bootstrap
  section at the top covering the evaluation posture; the existing
  production-deploy walk-through stays as-is.
  `docs/configuration.md` updates the admin-auth + JWT secret rows
  to "No (auto-bootstrap)" and adds a 5-minute-evaluation block.

Tests: new `internal/config/bootstrap_test.go` covers the seven paths
(no env vars → full mint + persist, plaintext env var → hash + no
persist, existing `.jwt-secret` → reuse, existing
`.initial-admin-password` → reuse, explicit env vars → strict no-op
+ no disk writes, corrupt `.jwt-secret` → clean error,
randomPassword alphabet contract). Existing config tests updated:
`TestLoad_MissingJWTSecret` → `..._BootstrapsInstead`,
`TestLoad_MissingAdminPasswordHash` → `..._BootstrapsInstead`,
`TestLoad_MissingDriver` → `..._NowOptional`, `TestLoad_AggregatedErrors`
expects only the driver-specific vars. Touched:
`internal/config/{config.go,bootstrap.go,config_test.go,bootstrap_test.go}`,
`cmd/basement-server/main.go`, `scripts/install.sh`,
`docs/deployment/docker.md`, `docs/configuration.md`, `README.md`.
`go test -race ./...` + `pnpm build` + 359/359 frontend tests green.
End-to-end smoke (no env vars → server boots, prints password, files
created at 0600; reboot → reuses both files; plaintext env → no
persistence) verified locally.

## v1.11.0.1 — 2026-05-22

Garage v2 driver: admin-tier (admin-only) connections fixed.

- **Bug.** Adding a Garage v2 cluster with only `admin_token` (no
  `access_key_id` / `secret_key`) — the ADR-0001 operator-level "admin
  tier" connection — created the cluster but `GET
  /api/v1/admin/clusters/$CID/buckets` returned
  `DRIVER_BUILD_FAILED: ... missing required config key:
  access_key_id`. The v1 driver
  (`internal/drivers/garage_v1/garage.go`) already gates S3 client
  construction on all three of `s3_endpoint` + `access_key_id` +
  `secret_key` (v0.9.0d); the v2 driver only checked `s3_endpoint`
  and propagated the missing-creds error up through `newDriver`.
- **Fix.** Mirror the v1 gate in `internal/drivers/garage/garage.go`:
  build the S3 client only when all three keys are present. Admin
  ops (`ListBuckets`, `CreateBucket`, key/cluster CRUD) flow through
  the admin API + `admin_token` and need no S3 client; the v1.1.0d
  region bridge takes over for S3 data-plane work via user-region
  keys. S3 ops on an admin-only driver already returned
  `ErrUnsupported` via the existing `if d.s3Client == nil` guards
  in `s3.go`, so the only fix needed was the constructor gate.
- **Tests.** Added five unit tests to
  `internal/drivers/garage/s3_test.go`:
  (a) `newDriver` succeeds with admin-only config + leaves
  `s3Client` nil; (b) `newDriver` succeeds when `s3_endpoint` is set
  but creds absent (region-bridge lookup mode) + leaves `s3Client`
  nil while preserving `s3Endpoint`; (c) `ListBuckets` works on an
  admin-only driver against a mock admin server (the regression
  test for the deploy failure); (d) S3 ops return `ErrUnsupported`
  cleanly on admin-only drivers; (e) the full-creds path remains
  unchanged + builds the S3 client. `go test -race ./...` green;
  `pnpm build` clean.

## v1.11.0b — 2026-05-22

Production deployment guide cycle (docs-only).

- **New `docs/deployment/` directory** with an index README + six
  topic sub-docs: `docker.md` (annotated Compose + bcrypt + JWT
  rotation), `reverse-proxy.md` (Caddy / Nginx / Traefik examples
  including the Caddy `method`-allowlist workaround for WebDAV
  PROPFIND / MKCOL / MOVE / COPY passthrough flagged in v1.9.0e),
  `tls.md` (auto-ACME / behind Cloudflare / proxy-terminated
  topologies + the Cloudflare WebDAV-verb-stripping note +
  `cloudflared` tunnel workaround), `hardening.md` (network bind,
  cookie posture, secrets management, data-dir perms, audit-log
  retention, container-user / capability / read-only-rootfs
  posture + production checklist), `backup-basement.md` (data-dir
  vs object-data distinction, ZFS-snapshot / stop-and-copy /
  live-rsync trade-off, JWT-secret-must-be-backed-up-with-data
  warning, restore drill), `upgrade.md` (tag-pull + restart,
  Watchtower wiring + opt-out reasoning, v1.x forward+backward
  compat contract, v2.0 major-version-upgrade caveat).
- **README Quickstart rewritten** as a five-line `docker run`
  one-shot with `htpasswd -bnBC 12` for the admin bcrypt hash and
  `openssl rand -base64 32` for the JWT secret; pointer to the new
  deployment dir for production posture.

No code changes; `pnpm build` + `go test -race ./...` green.

## v1.11.0a — 2026-05-22

First-run onboarding wizard. Fresh operator installs basement, logs in
as the host admin, and now gets walked through cluster + (optional)
OIDC + (optional) team invite + done instead of an empty UI with no
guidance. Five-step linear wizard at `/admin/first-run` with a stepper,
Skip on every step, and the same dismiss latch reachable from "I'll set
up later" on the welcome card.

- **Detection.** New `GET /api/v1/admin/onboarding/state` reports
  `{needsOnboarding, completed}`. `needsOnboarding` is `true` only when
  the deploy has zero clusters AND zero non-admin users; `completed` is
  the dismiss latch stored on `OrgCapabilities.OnboardingCompleted`.
  The AppShell auto-routes admin entries to the wizard when
  `needsOnboarding && !completed`.
- **Upgrade-safety.** An existing `org_capabilities.json` that predates
  the `onboardingCompleted` field auto-promotes to `completed=true` on
  load — so operators upgrading from v1.10.x with a configured deploy
  are never bounced into the wizard. Fresh installs (no file on disk)
  read `completed=false` and see the wizard correctly.
- **Dismiss latch.** `POST /api/v1/admin/onboarding/dismiss` (uiAdmin-
  gated, audit-logged as `host:onboarding_dismissed`) flips the latch
  to `true` and the FE never auto-shows the wizard again. Manual
  navigation to `/admin/first-run` always works regardless of state.
- **Wizard UX.** Linear stepper at top showing 1/5 → 5/5; each step has
  Back + Skip + Next, all 44px tap targets. Step 2 (cluster) carries a
  Test-connection button that creates + tests the connection via the
  existing `/admin/clusters/{cid}/_test` endpoint and surfaces a green
  ✓ / red inline error. Step 5 (Done) renders four nav cards to
  Clusters / My Regions / System / docs.

Tests: new Go tests cover the state endpoint (fresh-deploy / cluster-
exists / dismiss-flips / 401 / 403), the store migration (legacy file
auto-completes on upgrade / fresh install does not / latch persists
across re-open), and a frontend unit test exercises the wizard step-
through + dismiss paths. AppShell admin-redirect test updated to mock
the new `useOnboardingState` hook. 363/363 frontend tests + `go test
-race ./...` green.

Touched: `internal/store/org_capabilities.go` (new field +
`MarkOnboardingCompleted` + legacy-file migration), `internal/api/
admin_onboarding.go` (state + dismiss handlers), `internal/api/
server.go` (route wiring), `frontend/src/routes/admin/first-run.tsx`
(new wizard route), `frontend/src/shared/layout/AppShell.tsx` (redirect
effect), `frontend/src/shared/api/queries.ts` (`useOnboardingState`).

## v1.10.0.2 — 2026-05-22

Continuation of the v1.10.0.1 "bug tested to death" pass — same shapes
the smoke flagged on adjacent surfaces, all four mopped up in one
hotfix tag:

- **`/admin/users/new` blank submit silently did nothing.** Same shape
  as the keys/new + federated-buckets/new fixes in v1.10.0.1 — submit
  was `disabled` while the username was blank, so an operator clicking
  a pristine form got no feedback. Now submit stays enabled, the click
  fires per-field validation (username + password-length when
  invite-only is off), inline `role="alert"` messages render next to
  each offending input, `aria-invalid` flips on the inputs, and the
  alerts auto-clear once the operator starts typing.
- **`ThemeToggle` 32px tall on mobile.** The `Button size="icon"`
  default (`size-8`) sat below the WCAG/iOS HIG 44×44 mobile tap
  threshold flagged in smoke section [E]. Added
  `min-h-[44px] min-w-[44px] sm:min-h-0 sm:min-w-0` so the tap area
  is >=44px on touch viewports and snaps back to the 32px visual on
  desktop (where the smoke audit did not flag this control).
- **`UserMenu` trigger ~40px on mobile.** Same fix shape — added the
  44px floor on touch viewports, cleared on `sm:`.
- **`/files` "+ Add a key" CTA 36px on mobile.** The header action +
  empty-state CTA shared a `px-4 py-2 text-sm` recipe that rendered
  ~36px tall. Same `min-h-[44px] sm:min-h-0` floor on the link.

Tests: new unit tests cover (a) `/admin/users/new` blank-submit
inline alerts (each input flips `aria-invalid` + a `role=alert`
renders; alert clears when the operator types), (b) the password-too-
short alert, (c) `ThemeToggle` carries the tap-target utilities, (d)
`UserMenu` trigger carries them, (e) the `/files` Add-a-key CTAs
carry the tap-target utility. Touched:
`frontend/src/routes/admin/users/new.tsx`,
`frontend/src/routes/files/index.tsx`,
`frontend/src/shared/theme/ThemeToggle.tsx`,
`frontend/src/shared/ui/UserMenu.tsx`, plus their `__tests__/`.
359/359 frontend tests + `go test -race ./...` green; comprehensive
smoke re-run against the deploy promotes the four affected checks
from `bug` to `no-bug`.

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
