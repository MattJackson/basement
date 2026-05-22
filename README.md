# basement

> One pane of glass for Garage, MinIO/OpenMaxIO, and AWS S3 —
> region-aware user persona, multi-backend admin underneath.

[![CI badge]] [![Release badge]] [![License: AGPL-3.0]]

basement is a polished, identity-aware admin UI for self-hosted
S3-compatible object storage. It manages multiple clusters across
multiple backends — Garage clusters at home, MinIO at work, an AWS
account for offsite backups — from a single web UI.

![Hero screenshot of clusters list with 3 different drivers](./docs/screenshots/clusters-list.png)

## Why basement

The post-MinIO Console world (Feb 2026 archival) left self-hosters
without a polished, multi-backend admin UI. The replacements either
fork the MinIO console (OpenMaxIO — MinIO-only), restore Garage
admin (khairul169/garage-webui, Noooste/garage-ui — Garage-only), or
ship as alpha-quality with security issues (RustFS).

basement is the gap-filler: clean, multi-backend, identity-aware.
Four drivers ship — **Garage v1**, **Garage v2** (first UI to
support the v2 admin API), **AWS S3**, and **MinIO/OpenMaxIO** —
with a driver interface that lets the project keep up with the
ecosystem. v1.0 added a flexible policy matrix + per-user encrypted
S3 credentials so backend audit logs attribute requests to the
actual user rather than a shared key. v1.1 sharpened the user
persona around a **region-tier keychain** — one credential per
endpoint, backend authoritative for bucket visibility — so users
stop seeing the cluster plumbing. v1.2 landed **sudo-style admin
elevation** plus a **key-first user keychain** that supports
multiple access keys against the same S3 endpoint. v1.3 was the
multi-user polish cycle: OIDC group → role auto-mapping,
driver-aware endpoint hints, per-region S3 addressing toggle,
folder navigation in the bucket browser, bulk-import of access
keys, per-cluster `cluster_admin` assignment UI, and a simplified
two-mode elevation model with operator-configurable TTL. v1.4 was
the **scale + perf** cycle: **virtualized object browser** for
10K+ row directories, **paginated key permissions** editor with
filter + sticky Save, **batch object operations** with sticky
action bar, **growth analytics** on `/admin/usage` (per-cluster
growth column + top-growing-buckets panel + anomaly banner + 7d /
30d / 90d range selector), and **Garage block-scrub UI** at
`/admin/clusters/{cid}/scrub` for live cluster-durability scans.
v1.5 is the **backup story** cycle: **scheduled S3 → S3 backups**
with cron-driven engine, **mirror + snapshot modes** (timestamped
history via `{slug}/{YYYY-MM-DD_HH:MM:SS}/`), **GFS retention**
(`KeepDaily / KeepWeekly / KeepMonthly`, default `{7, 4, 12}` ≈
14 months of history), an **auto-prune** runner after each
snapshot run, and a **3-step restore wizard** with per-snapshot
deep-link to walk an operator from "I lost the bucket" through
"land last Tuesday's copy in a target bucket" without a CLI.

## Features

- **Multi-cluster admin** — Add N clusters, manage them side by side
- **Four drivers** — Garage v1, Garage v2, AWS S3, MinIO / OpenMaxIO; driver-capability-honest UI (no driver-name checks)
- **Driver-aware form hints** (v1.3) — Cluster + key forms render driver-specific placeholders (`http://garage:3903`, `https://s3.us-east-1.amazonaws.com`, `http://minio:9000`) and auto-suggest the region label when the operator pastes a known endpoint
- **First UI to support Garage v2 admin API** — vendored spec, refreshed on upstream updates
- **OIDC + local password** — Sign in with Authentik / Keycloak / Pocket-ID; local password as break-glass
- **OIDC group → role auto-mapping** (v1.3) — Operator-configured at `/admin/system`; matching mappings auto-assign on every IdP login, stale ones revoke, manual assignments never touched
- **Three-tier role model** — Host Admin / Cluster Admin / User; orthogonal axes, any combo per account
- **Per-cluster cluster_admin assignment UI** (v1.3) — Cluster detail pages surface a "Cluster admins" section above Buckets; manual grants live alongside inherited rows from `cluster:*` / superuser scopes
- **Sudo-style admin elevation** — Two-mode state machine (USER / ADMIN) per [ADR-0003 amendment](docs/adr/0003-sudo-style-admin-elevation.md#amendment-v130a4--two-mode-simplification--operator-configurable-ttl); admin authority requires fresh re-auth (local-password or OIDC step-up); operator-configurable TTL at `/admin/system`; PersonaPill carries the live mode + countdown; drop-in-place expiry banner preserves form state across the USER&rarr;ADMIN downgrade
- **Flexible policy matrix** — 27 capabilities × roles × scopes editable at `/admin/policies`; two seeded roles (host_admin, cluster_admin) plus operator-defined custom roles. `bucket_user` is deprecated in v1.1 — bucket access is the S3 key's grant on the cluster, not a basement role
- **Key-first user keychain** — each access key is a card at `/files`; multiple keys per endpoint OK ("Work S3" + "Personal S3" against the same `s3.us-east-1.amazonaws.com`); AES-GCM keyed off JWT secret; backend authoritative for which buckets the key can reach (no per-bucket grant explosion in basement state)
- **Per-region S3 addressing toggle + in-place key rotation** (v1.3) — path-style (default, required for Garage) or virtual-host (preferred by AWS S3); rotate-key flow on `/files/keys` preserves alias / audit trail
- **Bulk-import access keys** (v1.3) — `/files/keys/new` "Bulk import" toggle accepts CSV / TSV / aws-cli credentials-file profile blocks with a per-row preview and validation
- **Folder navigation in the bucket browser** (v1.3) — Delimiter-based `commonPrefixes` collapse deep key trees into clickable folder rows with a breadcrumb back to the bucket root
- **Virtualized bucket browser** (v1.4) — 10K+ row directories scroll smoothly at fixed 48px rows via `@tanstack/react-virtual`; infinite scroll on the S3 continuation token. `Driver.PerBucketStatsAvailable()` capability flag hides Size + Objects columns on backends without per-bucket stats (Garage v1 at the user-region tier) instead of rendering rows of em-dashes
- **Batch object operations** (v1.4) — Per-file checkboxes + select-all-visible header checkbox + sticky bottom action bar (`N selected | Delete N objects | Cancel`); delete fans out via `Promise.allSettled` with per-row error indicators on partial failure
- **Paginated key permissions editor** (v1.4) — `/admin/clusters/{cid}/keys/{kid}` Edit mode hydrates the FULL cluster bucket list with a filter input, 50-per-page Prev/Next, "Show only granted" toggle, and a sticky Save bar pinned to the bottom of the card
- **Paginated audit log + CSV export** (v1.4) — `/admin/audit` switched from 200-row dumps to 50-per-page Prev/Next + "Showing X-Y of Z (Page N of M)" footer + a client-side "Export CSV" button that dumps the currently filtered page
- **Storage growth analytics** (v1.4) — `/admin/usage` adds a `Growth (Nd)` per-cluster column, a "Buckets growing fastest" panel, an amber anomaly banner for any bucket that more than doubled in the window, and a 7d / 30d / 90d range selector
- **Block scrub UI for Garage** (v1.4) — `/admin/clusters/{cid}/scrub` renders live scrub state (Running/Idle badge, blocks scanned/corrupt, progress %, last-completed timestamp, free-form driver message) and a Run scrub button. AWS S3 + MinIO advertise "Not supported" with the capability reason
- **Scheduled bucket-to-bucket backups** (v1.5) — `/files/backups` lists the caller's named, scheduled backups; `/files/backups/new` is a 5-step wizard (source / destination / mode + retention / schedule / name + review); detail page at `/files/backups/$id/` shows run history, snapshot table (snapshot-mode), enable / disable, edit-schedule-inline, run-now, delete
- **Mirror + snapshot backup modes** (v1.5) — `mirror` overwrites the destination on every run (continuous one-shot); `snapshot` writes to `{dst}/{slug(name)}/{YYYY-MM-DD_HH:MM:SS}/` for point-in-time history
- **Grandfather-Father-Son retention** (v1.5) — `RetentionPolicy{KeepDaily, KeepWeekly, KeepMonthly}` (default `{7, 4, 12}` ≈ 14 months of history with 23 stored snapshots); auto-prune runs after each snapshot write; pure-function `PlanPrune` with 17 table-driven tests
- **Restore wizard with snapshot deep-link** (v1.5) — `/files/backups/$id/restore` 3-step wizard: pick snapshot (latest or explicit timestamp), pick destination (defaulted to backup's original source for one-click in-place restore), confirm + run with `overwriteExisting` toggle; synchronous `POST /api/v1/user/backups/{id}/restore` returns per-object summary; per-snapshot "Restore →" deep-link pre-fills the wizard via `?ts=YYYY-MM-DD_HH:MM:SS`
- **Persistent invite tokens** (v1.3) — `/admin/users` "Pending invites" section: mint, label, revoke, rotate, copy-full-URL; 30-day default expiry; optional label feeds the auto-generated username
- **Two deployment postures** — Company mode (default, Host Admin curates clusters) vs Multi-tenant mode (users BYO buckets via own keys)
- **What-if policy simulator** — "Can user X do capability Y on scope Z?" with reasoning trace
- **Bucket lifecycle wizard** — "After 30 days, delete" without writing JSON; capability-gated per driver
- **Storage overview dashboard** — per-cluster totals + top buckets by size/objects
- **Cluster-to-cluster migrate wizard** — 3-step bulk copy across drivers (fans out to existing sync engine)
- **Cross-backend sync** — Pull/Push between any two clusters; resumable jobs persisted to disk
- **Bucket + Key admin** — CRUD, quotas, per-bucket permissions, delete protection via two-phase confirm tokens
- **Layout editor** (Garage) — Stage / apply / revert cluster topology changes
- **Audit log** — Every mutating action with actor / mode / capability / scope / result + filterable viewer at `/admin/audit`
- **All forms with >2 fields are pages, not dialogs** — operator-confirmed UX rule
- **Single static binary** — Go backend + embedded React frontend; distroless Docker image runs as UID 65532

## Screenshots

Empty-state onboarding · Clusters list · Bucket detail · Key
permission grid · OIDC login · Multi-cluster bucket list

(See `docs/screenshots/` for full size; see
`docs/screenshots/SHOTLIST.md` for descriptions.)

## Quickstart

```bash
git clone https://github.com/mattjackson/basement
cd basement/deploy
cp .env.example .env  # edit values
docker compose -f docker-compose.example.yml up -d
# basement on https://localhost (or your hostname behind Caddy)
```

This brings up three example clusters side by side:
- A Garage container (single-node, dev-quality)
- A MinIO container (single-node, dev-quality)
- An AWS S3 connection (uses env-supplied creds)

Sign in with the env-seeded admin (default `admin / changeme`).

See `docs/configuration.md` for production env vars.

## Comparison vs other OSS admin UIs

| Feature                              | basement v1.5 | khairul169/garage-webui | Noooste/garage-ui | OpenMaxIO       |
|--------------------------------------|------------------|-------------------------|-------------------|-----------------|
| Garage admin                         | yes (v1 + v2)    | yes                     | yes               | no              |
| MinIO admin                          | yes              | no                      | no                | yes (MinIO-only)|
| AWS S3 admin                         | yes (driver)     | no                      | no                | no              |
| Multi-cluster from one UI            | yes              | no                      | no                | no              |
| OIDC / SSO                           | yes              | no                      | yes               | (MinIO-driven)  |
| Flexible role/permission matrix      | yes (27 caps)    | no                      | yes (teams)       | (MinIO-driven)  |
| Per-user encrypted S3 credentials    | yes (region-keyed) | no                    | no                | no              |
| Cross-backend sync (Migrate wizard)  | yes              | no                      | no                | no              |
| Scheduled backups + GFS retention    | yes (v1.5)       | no                      | no                | no              |
| Point-in-time restore wizard         | yes (v1.5)       | no                      | no                | no              |
| Bucket lifecycle wizard              | yes              | no                      | no                | (MinIO-driven)  |
| Policy simulator (what-if)           | yes              | no                      | no                | no              |
| Delete protection (two-phase)        | yes              | no                      | no                | no              |
| Layout editor                        | yes (Garage)     | yes                     | yes               | n/a             |
| Open source license                  | MIT              | AGPL                    | MIT               | AGPL (fork)     |
| Status (as of 2026-05-22)            | active v1.5      | active v1.1.0           | active v0.5       | active fork     |

Full competitive write-up:
[`competitive-landscape-2026-05-19.md`](https://github.com/mattjackson/basement-internal)
(internal — link to summary appendix below; full doc in private repo).

## Roadmap

- v0.5.0 — multi-cluster admin + 4 drivers + OIDC (shipped)
- v0.6.x — end-user shell (file browser for non-admin users) (shipped)
- v0.7.x — end-user sharing + bucket grants (shipped)
- v0.8.x — cross-backend sync (Pull / Push between any two clusters) (shipped)
- v0.9.x — operator polish: ADR-0001 three-tier RBAC, lifecycle wizard, policy simulator, usage dashboard, migrate wizard (shipped)
- v1.0 — production-ready milestone: at-rest encryption for admin_token + S3 secrets, audit log subsystem, metrics persistence + time-series chart on `/admin/usage` (shipped — see [docs/release-notes/v1.0.0.md](docs/release-notes/v1.0.0.md))
- v1.1 — region tier replaces cluster-tier at the user persona (ADR-0002); `bucket_user` role deprecated; per-user keychain at `/files/keys`; sync + share become region-aware (shipped — see [docs/release-notes/v1.1.0.md](docs/release-notes/v1.1.0.md))
- v1.2 — sudo-style admin elevation per [ADR-0003](docs/adr/0003-sudo-style-admin-elevation.md) (USER → ADMIN → ELEVATED state machine with re-auth at each transition); key-first user keychain (multiple access keys per endpoint); `unique(userId, endpoint)` relaxed to `unique(userId, endpoint, alias)` (shipped — see [docs/release-notes/v1.2.0.md](docs/release-notes/v1.2.0.md))
- v1.3 — multi-user polish: OIDC group → role auto-mapping; driver-aware endpoint hints; per-region S3 addressing toggle (path-style / virtual-host); rotate-key flow; folder navigation in the bucket browser; invite-token polish + bulk-import keys; per-cluster `cluster_admin` assignment UI; two-mode elevation (USER / ADMIN) with operator-configurable TTL per [ADR-0003 amendment](docs/adr/0003-sudo-style-admin-elevation.md#amendment-v130a4--two-mode-simplification--operator-configurable-ttl) (shipped — see [docs/release-notes/v1.3.0.md](docs/release-notes/v1.3.0.md))
- v1.4 — scale + perf: virtualized bucket browser for 10K+ object directories; `Driver.PerBucketStatsAvailable()` capability gate; paginated audit log + Export CSV; paginated key permissions editor with filter + sticky Save bar; batch object operations + sticky action bar; storage growth analytics (`Growth (Nd)` column, top-growing-buckets panel, anomaly banner, 7d / 30d / 90d range selector); Garage block-scrub UI at `/admin/clusters/{cid}/scrub` (shipped — see [docs/release-notes/v1.4.0.md](docs/release-notes/v1.4.0.md))
- **v1.5 (current)** — backup story: scheduled bucket-to-bucket backups with cron engine; mirror + snapshot modes; GFS retention with auto-prune; 3-step restore wizard with snapshot-level deep-link; mirror-mode short-circuit for backups that don't keep history (shipped — see [docs/release-notes/v1.5.0.md](docs/release-notes/v1.5.0.md))
- **v2.0 — scoping doc pending operator review.** Major-version slot. Likely themes from the v1.x carry-over backlog: async/long-running restore with poll-able progress; B2 / R2 / Wasabi as first-class drivers; multi-select move + copy in the bucket browser; `/v1/worker` feature-detection on the block-scrub UI; in-product surface for backing up `BASEMENT_DATA_DIR` itself. Scope locks once the operator signs off on the scoping doc

## Architecture

- **Backend**: Go 1.23+, chi router, embedded JSON store
- **Frontend**: React 19 + TanStack Router/Query + shadcn/ui + Tailwind 4
- **Auth**: bcrypt + JWT in `__Host-` cookie (SameSite=Strict) + OIDC (coreos/go-oidc)
- **Drivers**: Go interface; per-backend translation layer; capability flags drive UI gating (no driver-name checks)
- **Policy enforcer**: `internal/auth/policy/` — capability registry (compiled-in), Role + RoleAssignment store, `Can(user, capability, scope)` primitive plus `CanWithReason()` for the simulator
- **Per-user region keychain**: `internal/store/user_regions.go` — AES-GCM encrypted secrets, keyed off JWT signing secret; one record per (user, endpoint); per-request signing via `Registry.ForUserRegion(endpoint, accessKeyID, secretKey, region)`
- **Persistence**: JSON files under `BASEMENT_DATA_DIR` (default `/var/lib/basement`); atomic write via tmp+fsync+rename
- **Design contracts**:
  - [`docs/adr/0001-rbac-three-tier-creds.md`](docs/adr/0001-rbac-three-tier-creds.md) — role / capability / scope model
  - [`docs/adr/0002-region-tier-user-model.md`](docs/adr/0002-region-tier-user-model.md) — region tier at the user persona
  - [`docs/adr/0003-sudo-style-admin-elevation.md`](docs/adr/0003-sudo-style-admin-elevation.md) — USER → ADMIN → ELEVATED state machine (v1.2)

See `docs/configuration.md` for env reference,
`docs/release-notes/v1.5.0.md` for the current release changelog,
`docs/release-notes/v1.4.0.md` for the v1.4 scale + perf write-up,
`docs/release-notes/v1.3.0.md` for the v1.3 multi-user-onboarding
write-up, `docs/release-notes/v1.2.0.md` for the v1.2 sudo-elevation +
key-first write-up, `docs/release-notes/v1.1.0.md` for the v1.1
region-tier write-up, and `docs/release-notes/v1.0.0.md` for the v1.0
baseline.

## Contributing

PRs welcome. Driver authors especially — basement is designed to
accept new backends. See `docs/driver-authoring.md` (TODO post-v0.5).

## License

MIT. See [LICENSE](./LICENSE).
