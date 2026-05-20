# basement

> One pane of glass for Garage, MinIO/OpenMaxIO, and AWS S3.

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
Three drivers ship in v0.5.0 (Garage v1, AWS S3, MinIO/OpenMaxIO),
with a driver interface that lets the project keep up with the
ecosystem.

## Features

- **Multi-cluster admin** — Add N clusters, manage them side by side
- **Three drivers** — Garage v1, AWS S3, MinIO / OpenMaxIO
- **Garage v2 support** — First UI to support Garage v2 admin API (vendor spec; refresh on upstream updates)
- **OIDC + local password** — Sign in with Authentik / Keycloak /
  Pocket-ID; local password as break-glass
- **Bucket + Key admin** — CRUD, quotas, per-bucket permissions,
  delete protection via two-phase confirm tokens
- **Layout editor** (Garage) — Stage / apply / revert cluster
  topology changes
- **Role split** — admin vs super-admin; destructive ops gated to
  super-admin
- **Driver-capability-honest** — UI hides features the backend
  doesn't support, doesn't fail on click
- **Single static binary** — Go backend + embedded React frontend;
  Docker image fits in ~10MB

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

| Feature                              | basement v0.5 | khairul169/garage-webui | Noooste/garage-ui | OpenMaxIO       |
|--------------------------------------|------------------|-------------------------|-------------------|-----------------|
| Garage admin                         | yes (v1 + v2)    | yes                     | yes               | no              |
| MinIO admin                          | yes              | no                      | no                | yes (MinIO-only)|
| AWS S3 admin                         | yes (driver)     | no                      | no                | no              |
| Multi-cluster from one UI            | yes              | no                      | no                | no              |
| OIDC / SSO                           | yes              | no                      | yes               | (MinIO-driven)  |
| Multi-role RBAC (admin / super)      | yes              | no                      | yes (teams)       | (MinIO-driven)  |
| Delete protection (two-phase)        | yes              | no                      | no                | no              |
| Layout editor                        | yes (Garage)     | yes                     | yes               | n/a             |
| Open source license                  | MIT              | AGPL                    | MIT               | AGPL (fork)     |
| Status (as of 2026-05-19)            | active v0.5      | active v1.1.0           | active v0.5       | active fork     |

Full competitive write-up:
[`competitive-landscape-2026-05-19.md`](https://github.com/mattjackson/basement-internal)
(internal — link to summary appendix below; full doc in private repo).

## Roadmap

v0.5.0 (now): multi-cluster admin + three drivers + OIDC.
v0.6.x: end-user shell (file browser for non-admin users).
v0.7.x: end-user sharing + multi-cluster grants.
v0.8.x: cross-backend sync ("migrate this bucket from MinIO to
        Garage" with one click).
v0.9.x: AWS-console-quality wizards (lifecycle, scoped credentials,
        policy simulator, usage analytics).
v1.0:   the long-haul "this is THE answer" version.

## Architecture

- **Backend**: Go 1.23+, chi router, embedded JSON store
- **Frontend**: React 19 + TanStack Router/Query + shadcn/ui + Tailwind
- **Auth**: bcrypt + JWT in HttpOnly cookie + OIDC (coreos/go-oidc)
- **Drivers**: Go interface; per-backend translation layer
- **Persistence**: JSON files under `BASEMENT_DATA_DIR`

See `docs/configuration.md` for full env reference.

## Contributing

PRs welcome. Driver authors especially — basement is designed to
accept new backends. See `docs/driver-authoring.md` (TODO post-v0.5).

## License

MIT. See [LICENSE](./LICENSE).
