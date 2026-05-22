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
stop seeing the cluster plumbing.

## Features

- **Multi-cluster admin** — Add N clusters, manage them side by side
- **Four drivers** — Garage v1, Garage v2, AWS S3, MinIO / OpenMaxIO; driver-capability-honest UI (no driver-name checks)
- **First UI to support Garage v2 admin API** — vendored spec, refreshed on upstream updates
- **OIDC + local password** — Sign in with Authentik / Keycloak / Pocket-ID; local password as break-glass
- **Three-tier role model** — Host Admin / Cluster Admin / User; orthogonal axes, any combo per account
- **Flexible policy matrix** — 27 capabilities × roles × scopes editable at `/admin/policies`; two seeded roles (host_admin, cluster_admin) plus operator-defined custom roles. `bucket_user` is deprecated in v1.1 — bucket access is the S3 key's grant on the cluster, not a basement role
- **Region-tier user keychain** — one credential per endpoint at `/files/keys`; the backend is authoritative for which buckets the key can reach (no per-bucket grant explosion in basement state); AES-GCM keyed off JWT secret
- **Two deployment postures** — Company mode (default, Host Admin curates clusters) vs Multi-tenant mode (users BYO buckets via own keys)
- **What-if policy simulator** — "Can user X do capability Y on scope Z?" with reasoning trace
- **Bucket lifecycle wizard** — "After 30 days, delete" without writing JSON; capability-gated per driver
- **Storage overview dashboard** — per-cluster totals + top buckets by size/objects
- **Cluster-to-cluster migrate wizard** — 3-step bulk copy across drivers (fans out to existing sync engine)
- **Cross-backend sync** — Pull/Push between any two clusters; resumable jobs persisted to disk
- **Bucket + Key admin** — CRUD, quotas, per-bucket permissions, delete protection via two-phase confirm tokens
- **Layout editor** (Garage) — Stage / apply / revert cluster topology changes
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

| Feature                              | basement v1.1 | khairul169/garage-webui | Noooste/garage-ui | OpenMaxIO       |
|--------------------------------------|------------------|-------------------------|-------------------|-----------------|
| Garage admin                         | yes (v1 + v2)    | yes                     | yes               | no              |
| MinIO admin                          | yes              | no                      | no                | yes (MinIO-only)|
| AWS S3 admin                         | yes (driver)     | no                      | no                | no              |
| Multi-cluster from one UI            | yes              | no                      | no                | no              |
| OIDC / SSO                           | yes              | no                      | yes               | (MinIO-driven)  |
| Flexible role/permission matrix      | yes (27 caps)    | no                      | yes (teams)       | (MinIO-driven)  |
| Per-user encrypted S3 credentials    | yes (region-keyed) | no                    | no                | no              |
| Cross-backend sync (Migrate wizard)  | yes              | no                      | no                | no              |
| Bucket lifecycle wizard              | yes              | no                      | no                | (MinIO-driven)  |
| Policy simulator (what-if)           | yes              | no                      | no                | no              |
| Delete protection (two-phase)        | yes              | no                      | no                | no              |
| Layout editor                        | yes (Garage)     | yes                     | yes               | n/a             |
| Open source license                  | MIT              | AGPL                    | MIT               | AGPL (fork)     |
| Status (as of 2026-05-21)            | active v1.1      | active v1.1.0           | active v0.5       | active fork     |

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
- **v1.1 (current)** — region tier replaces cluster-tier at the user persona (ADR-0002); `bucket_user` role deprecated; per-user keychain at `/files/keys`; sync + share become region-aware (shipped — see [docs/release-notes/v1.1.0.md](docs/release-notes/v1.1.0.md))
- v1.2 — sudo-style admin elevation per [ADR-0003](docs/adr/0003-sudo-style-admin-elevation.md) (USER → ADMIN → ELEVATED state machine with re-auth at each transition); per-cluster-admin role assignment UI; OIDC group-claim → role auto-mapping

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
`docs/release-notes/v1.1.0.md` for the current release changelog, and
`docs/release-notes/v1.0.0.md` for the v1.0 baseline.

## Contributing

PRs welcome. Driver authors especially — basement is designed to
accept new backends. See `docs/driver-authoring.md` (TODO post-v0.5).

## License

MIT. See [LICENSE](./LICENSE).
