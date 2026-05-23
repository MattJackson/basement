# Comparison research notes

Working notes for the "Compared to other OSS admin UIs" table in `README.md`.
Verified against project READMEs, GitHub repo metadata, source code searches,
and upstream backend documentation.

**Verification date:** 2026-05-23

## Projects compared

| Project | Repo | Latest release | Last push | License |
|---|---|---|---|---|
| basement | github.com/MattJackson/basement | v1.11.0.11 (2026-05) | active | AGPL-3.0 |
| MinIO Console (opens3 fork) | github.com/opens3/console | (no tagged releases; main moves) — last commit 2025-11-03 | 2025-11 | AGPL-3.0 |
| OpenMaxIO Object Browser | github.com/OpenMaxIO/openmaxio-object-browser | inherited `v1.7.6` (pre-fork); last commit 2025-06-24 | 2025-06 | AGPL-3.0 |
| khairul169/garage-webui | github.com/khairul169/garage-webui | v1.1.0 (2025-09-01) | 2025-09 | MIT |
| Noooste/garage-ui | github.com/Noooste/garage-ui | v0.6.2 (2026-05-15) | 2026-05 | MIT |

Note: github.com/minio/console was archived as part of MinIO's April 2026
relicense/wind-down (the whole minio/minio repo is archived since 2026-04-25).
The two viable AGPL forks are **opens3/console** and
**OpenMaxIO/openmaxio-object-browser**. The user's brief refers to "MinIO
Console" and "OpenMaxIO"; we treat the opens3 fork as the de-facto continuation
of MinIO Console (it explicitly says so in its README), and OpenMaxIO as the
named alternative.

## Per-row sourcing

### Backends supported

- **basement** — Garage v1 + v2, AWS S3, S3-compatible (per README "What it does").
- **MinIO Console (opens3)** — MinIO only. README: "community-maintained console for MinIO"; `CONSOLE_MINIO_SERVER` env var is required.
- **OpenMaxIO** — MinIO / OpenMaxIO only. README: "fork of MinIO Console", `CONSOLE_MINIO_SERVER` env var only.
- **khairul169/garage-webui** — Garage only. README + config use Garage admin API. Single endpoint per instance. Docker-compose example pins `dxflrs/garage:v2.0.0` but issue #19 (open) reports `/v1/status` broken on Garage 2.0.0 — so practical support is v1 with partial v2.
- **Noooste/garage-ui** — Garage only, "v2.1.0+" required (README "Prerequisites").

### OIDC SSO

- **basement** — yes (README explicitly lists Authentik, Pocket-ID, Keycloak).
- **MinIO Console (opens3)** — yes. `sso-integration/` directory; `sso_test.go` initializes `OpenIDProviders` config; uses Dex in tests.
- **OpenMaxIO** — yes (forked from same MinIO Console base which had OpenID provider support).
- **khairul169/garage-webui** — no. README "Authentication" section only documents `AUTH_USER_PASS` (single bcrypt-hashed user/pass via env var); no OIDC docs or config.
- **Noooste/garage-ui** — yes. README: "no auth, basic credentials, or OIDC (Keycloak, Authentik, etc.)". `OIDCLoginView.tsx` present.

### Multi-user RBAC

- **basement** — yes, three-axis (UI Admin / Cluster Admin per cluster / User per bucket).
- **MinIO Console (opens3)** — yes (MinIO IAM policy attachment per user/group; see README "Setup" creating users + policies via `mc admin policy attach`).
- **OpenMaxIO** — yes (same MinIO IAM model inherited).
- **khairul169/garage-webui** — no. Single admin via `AUTH_USER_PASS`. No multi-user.
- **Noooste/garage-ui** — basic. README mentions "team access control" in title but documented feature set is only "create keys, assign per-bucket permissions"; no role definitions.

### Bucket browser

- **basement** — yes (README: "virtualized browser, folder navigation, batch actions").
- **MinIO Console (opens3)** — yes (README lists object browser with advanced upload).
- **OpenMaxIO** — yes (forked; same UI).
- **khairul169/garage-webui** — yes ("Integrated objects/bucket browser").
- **Noooste/garage-ui** — yes (README "browse buckets, drag-and-drop file uploads").

### Federation / multi-cluster replication

- **basement** — yes (v1.6 federation; per-bucket multi-backend with event-driven replication).
- **MinIO Console (opens3)** — yes (Site Replication: `SiteReplication.tsx`, `ReplicationSites.tsx` in source).
- **OpenMaxIO** — yes (Site Replication inherited from MinIO Console).
- **khairul169/garage-webui** — no (no replication UI; relies on Garage's built-in replication factor at the cluster level which is a server config not a UI feature).
- **Noooste/garage-ui** — no (no federation/replication UI).

### Bucket versioning UI

- **basement** — yes (v1.10).
- **MinIO Console (opens3)** — yes (`EnableVersioningModal.tsx` in source; MinIO backend supports it).
- **OpenMaxIO** — yes (forked).
- **khairul169/garage-webui** — n/a. Garage backend does not support versioning per Garage S3 compatibility docs.
- **Noooste/garage-ui** — n/a. Same Garage backend limitation. (`versioning?: boolean` exists in types but is read-only display from backend capability.)

### S3 Object Lock UI

- **basement** — yes (v1.10; Governance + Compliance + per-version Legal Hold).
- **MinIO Console (opens3)** — yes (in MinIO Console source for many years).
- **OpenMaxIO** — yes (forked).
- **khairul169/garage-webui** — n/a (Garage backend does not support Object Lock).
- **Noooste/garage-ui** — n/a (Garage backend does not support Object Lock).

### Server-side encryption UI (SSE-S3 / SSE-KMS)

- **basement** — yes (v1.10).
- **MinIO Console (opens3)** — yes (`EnableBucketEncryption.tsx`, KMS pages in source).
- **OpenMaxIO** — yes (forked).
- **khairul169/garage-webui** — n/a (Garage does not implement SSE-S3 / SSE-KMS).
- **Noooste/garage-ui** — n/a (Garage backend limitation).

### WebDAV / SMB / NFS gateway

- **basement** — yes for WebDAV (v1.9 — `Gateway`/`Backend`/`Registry` interfaces; WebDAV shipped, SMB/NFS/FTP/S3 register as stubs).
- **MinIO Console (opens3)** — no. No mention of WebDAV/SMB/NFS in source.
- **OpenMaxIO** — no.
- **khairul169/garage-webui** — no.
- **Noooste/garage-ui** — no.

### MCP server (AI agent integration)

- **basement** — yes (v1.8, MCP server + service-account config UX).
- **All others** — no (no MCP integration documented in any).

### Mobile / PWA

- **basement** — yes (installable PWA, touch-friendly browser below 640px).
- **MinIO Console (opens3)** — no PWA; React desktop layout.
- **OpenMaxIO** — no PWA.
- **khairul169/garage-webui** — partial. Has `<link rel="manifest" href="/site.webmanifest" />` in `index.html` and a "Mobile" screenshots section; no service worker, so not a full installable offline PWA. Mobile responsive.
- **Noooste/garage-ui** — no. Roadmap lists "Mobile-friendly object browser" as TODO.

### Service-account / M2M tokens

- **basement** — yes (v1.7, service accounts + bearer tokens).
- **MinIO Console (opens3)** — yes (MinIO STS / service accounts).
- **OpenMaxIO** — yes (inherited).
- **khairul169/garage-webui** — partial (creates Garage access keys but no separate service-account concept).
- **Noooste/garage-ui** — partial (access-key management only).

### Scheduled backups (S3 → S3) + restore

- **basement** — yes (v1.5; mirror or snapshot, GFS retention, point-in-time restore wizard).
- **All others** — no.

### Webhooks / event notifications

- **basement** — yes (v1.7).
- **MinIO Console (opens3)** — yes (bucket notifications surface in UI).
- **OpenMaxIO** — yes (inherited).
- **khairul169/garage-webui** — no.
- **Noooste/garage-ui** — no.

### First-run install wizard

- **basement** — yes (v1.11; auto-bootstrap, browser wizard for cluster/admin/policy).
- **MinIO Console (opens3)** — no (requires manual `mc admin user add` + policy attach pre-launch).
- **OpenMaxIO** — no (same).
- **khairul169/garage-webui** — no (requires pre-existing `garage.toml` + admin token).
- **Noooste/garage-ui** — partial (token-auth auto-enables when pointed at `garage.toml`).

### Prometheus `/metrics` endpoint

- **basement** — yes.
- **MinIO Console (opens3)** — yes (MinIO server exposes Prometheus; Console surfaces dashboards).
- **OpenMaxIO** — yes (inherited).
- **khairul169/garage-webui** — no (Garage server has its own `/metrics`; no UI-side endpoint).
- **Noooste/garage-ui** — no (same).

### Audit log UI

- **basement** — yes (v1.0).
- **MinIO Console (opens3)** — partial (MinIO audit logs go to external sinks; Console surfaces some logs).
- **OpenMaxIO** — partial (inherited).
- **khairul169/garage-webui** — no.
- **Noooste/garage-ui** — no.

### License

- **basement** — AGPL-3.0.
- **MinIO Console (opens3)** — AGPL-3.0.
- **OpenMaxIO** — AGPL-3.0.
- **khairul169/garage-webui** — MIT.
- **Noooste/garage-ui** — MIT.

### Latest release / activity (as of 2026-05-23)

- **basement** — v1.11.0.11, 2026-05.
- **MinIO Console (opens3)** — no GitHub releases; last commit 2025-11-03.
- **OpenMaxIO** — last commit 2025-06-24 (effectively dormant).
- **khairul169/garage-webui** — v1.1.0, 2025-09-01.
- **Noooste/garage-ui** — v0.6.2, 2026-05-15.

## Context for table framing

Upstream MinIO (the server) was archived on GitHub 2026-04-25 and the project
moved to AIStor under a commercial license. The MinIO admin Console
historically had OIDC, RBAC, site replication, versioning, Object Lock, SSE
UI, and bucket notifications — that feature set is preserved in the AGPL
community forks (opens3/console and OpenMaxIO/openmaxio-object-browser).

Garage as a backend lacks versioning, Object Lock, and SSE entirely, so any
Garage-only admin UI necessarily lacks those rows (marked "n/a" rather than
"no").
