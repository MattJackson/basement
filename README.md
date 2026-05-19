# basement

**One pane of glass for Garage, MinIO, OpenMaxIO, and AWS S3.**

A multi-backend, multi-persona admin + end-user UI for self-hosted S3-compatible object storage. Run a Garage cluster at home, a MinIO/OpenMaxIO cluster at the office, and an AWS account for offsite backup — manage all of them from one place. No more switching between four single-backend admin tools just because the buckets live in different places.

> **Status:** v0.1.x — Garage v1 and AWS S3 drivers shipped. MinIO/OpenMaxIO driver, OIDC, and multi-cluster admin land at v0.5.0 (the competitive milestone). v1.0 is reserved for the long-haul "feature-complete" release; we're staying on `0.x` until the project is fully load-bearing for the post-MinIO crowd.

---

## Why this exists

MinIO Console was gutted in February 2025 (admin features removed from OSS, reduced to a read-only object browser), put in maintenance mode in December 2025, and archived in February 2026. Self-hosters running MinIO are mid-migration as of this writing. The replacements that emerged are all narrow:

- **OpenMaxIO** restored the original Console UI but is a MinIO-only fork.
- **Noooste/garage-ui** ships OIDC and team-based access control but is Garage-only.
- **khairul169/garage-webui** targets Garage's pre-v1 admin API and silently breaks on v1.0+.
- **Ceph RGW Dashboard** is strong but tied to ceph-mgr — disproportionate for a homelab.
- **s3manager / rclone / Cyberduck** are file *browsers*, not admin tools. No IAM, no policies, no lifecycle, no quotas.

**Nobody in OSS does multi-backend *admin*** — only multi-backend file browsing. Every real admin UI in the space is single-backend. That's the gap basement fills.

The bet: one identity layer + one UI manages Garage + MinIO + OpenMaxIO + AWS side-by-side. Operators running mixed fleets — or mid-migration off MinIO — stop running four separate admin tools.

The long-haul vision (v0.6+): a federated file-manager for end users where the user thinks "my stuff," not "which backend." Cross-backend copy, mirror, and sync. Share a file without showing the recipient where the data physically lives. rclone's power, Dropbox's polish, self-hosted.

---

## Status

| Capability | Status | Lands in |
|---|---|---|
| Garage v1 admin (buckets, keys, layout, delete protection) | shipped | v0.1.x |
| AWS S3 driver (S3 plane + capabilities-gated admin) | shipped | v0.1.x |
| Multi-cluster admin (Clusters as a top-level resource) | in flight | v0.2.0 |
| MinIO / OpenMaxIO driver | planned | v0.3.0 |
| OIDC + local-password coexistence | planned | v0.4.0 |
| **Competitive milestone:** three drivers + OIDC + multi-cluster | planned | **v0.5.0** |
| End-user UI scaffold (My files, browse, download) | planned | v0.6+ |
| Upload, sharing, server-routed share links | planned | v0.7 |
| Cross-backend copy / sync / mirror | planned | v0.8 |
| Lifecycle wizards, scoped credentials, migration tooling | planned | v0.9 |
| Feature-complete | reserved | v1.0 |

Honest take: pre-v0.5.0 you get a working Garage v1 admin UI with an AWS driver behind a capabilities-aware UI. That's already useful — it's what the operator runs in production today. The "one pane of glass" tagline becomes literal at v0.5.0 when three drivers run side-by-side from one instance.

---

## Quick start (Docker)

Run basement against a Garage cluster on the same host:

```bash
docker run -d --name basement \
  -p 8080:8080 \
  -v basement-data:/var/lib/basement \
  -e BASEMENT_DRIVER=garage-v1 \
  -e BASEMENT_DRIVER_GARAGE_ADMIN_URL=http://<garage-host>:3903 \
  -e BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN=<your-garage-admin-token> \
  -e BASEMENT_DRIVER_GARAGE_S3_URL=http://<garage-host>:3900 \
  -e BASEMENT_DRIVER_GARAGE_S3_REGION=garage \
  -e BASEMENT_DRIVER_GARAGE_S3_ACCESS_KEY=<s3-access-key> \
  -e BASEMENT_DRIVER_GARAGE_S3_SECRET_KEY=<s3-secret-key> \
  -e BASEMENT_ADMIN_USER=admin \
  -e BASEMENT_ADMIN_PASSWORD_HASH='<bcrypt-hash>' \
  -e BASEMENT_JWT_SECRET="$(openssl rand -base64 32)" \
  ghcr.io/mattjackson/basement:latest
```

Visit `http://localhost:8080` and log in. Full env-var reference: [`docs/configuration.md`](docs/configuration.md). Reference docker-compose stack (basement + Garage + Caddy with TLS): [`deploy/docker-compose.yml`](deploy/docker-compose.yml).

> **Caddy gotcha:** if you reverse-proxy the S3 data plane through Caddy, you must preserve the `Host` header (`header_up Host {host}`) or SigV4 signatures will fail. See [`deploy/Caddyfile`](deploy/Caddyfile).

---

## Comparison

| Tool | Multi-backend admin | OIDC | Targets | Status |
|---|---|---|---|---|
| **basement** (this) | yes (Garage / AWS today; MinIO at v0.3.0) | yes (v0.4.0) | multi | active, pre-v0.5.0 |
| khairul169/garage-webui | no | no | Garage only | ~1k stars, active |
| Noooste/garage-ui | no | yes | Garage only | 128 stars, active |
| MinIO Console | no | gone from OSS | MinIO only | archived Feb 2026 |
| OpenMaxIO object browser | no | n/a | MinIO only | active fork (May 2025+) |
| Ceph RGW Dashboard | no | yes | Ceph RGW only | tied to ceph-mgr |
| s3manager / rclone gui | (browse only) | no | multi-browse | active, no admin |
| Cyberduck | (browse only) | no | multi-browse | active desktop client |

Receipts for the table above (MinIO Console archival, OpenMaxIO scope, etc.) are kept in the internal competitive-landscape doc; everything cited here is publicly verifiable.

---

## Architecture (briefly)

```
┌─────────────┐     ┌──────────────────┐     ┌───────────────┐
│   Browser   │────▶│  basement     │────▶│  Garage v1    │
│             │     │  (Go, ~15 MB     │     │  MinIO / OMx  │
│             │◀────│  static binary,  │◀────│  AWS S3       │
│             │     │  scratch image)  │     │  …            │
└─────────────┘     └──────────────────┘     └───────────────┘
                       │         │
                       ▼         ▼
                   JWT cookie  Driver registry
                   (httpOnly)  (per-cluster, capabilities-aware)
```

- **Backend:** Go 1.23+, single static binary, distroless image, `chi` router, `slog`. JSON files for users/grants/shares; JSONL for the audit log. No SQLite — KISS for the volumes this manages.
- **Frontend:** React 18 + Vite + TypeScript strict, TanStack Router/Query, shadcn/ui on Radix + Tailwind. Dark mode default. Single SPA, two route trees (`/admin` and `/`).
- **Driver interface:** ~50 LOC of Go interface; each backend gets a ~200-LOC adapter that translates basement's API into the backend's wire format. The frontend never knows which driver is running.
- **Capabilities flags:** drivers self-declare what they support (`Layout`, `Quotas`, `BucketAliases`, `Multipart`, etc.). The UI feature-flags off these per-cluster, so AWS buckets don't show a `Layout` tab and Garage v1 doesn't show admin-token management.
- **Auth:** username + bcrypt password → HS256 JWT in `__Host-basement_session` httpOnly cookie. OIDC (`coreos/go-oidc`) lands alongside local password at v0.4.0.

The Go layer exists to confine the admin token (never exposed to the browser), enforce basement's own grants above the backend, and own the audit/rate-limit hook points. Frontend speaks only basement's own versioned API (`/api/v1/*`); backend shifts (Garage v2 RPC names, MinIO admin API differences) are absorbed in one place per driver.

---

## Configuration

Every option is an environment variable prefixed `BASEMENT_*`. Canonical reference: [`docs/configuration.md`](docs/configuration.md). The taxonomy is:

- `BASEMENT_DRIVER` — which driver to load (`garage-v1`, `aws-s3`, more at v0.5.0). Multi-cluster admin (v0.2.0) makes this seed the `default` connection and adds runtime-managed connections beyond it.
- `BASEMENT_DRIVER_<NAME>_<KEY>` — per-driver config (URLs, tokens, S3 keys).
- `BASEMENT_ADMIN_USER`, `BASEMENT_ADMIN_PASSWORD_HASH` — single admin (multi-user lands with OIDC at v0.4.0).
- `BASEMENT_JWT_SECRET` — HS256 signing secret, ≥32 bytes after base64 decode.
- `BASEMENT_LISTEN`, `BASEMENT_DATA_DIR`, `BASEMENT_PUBLIC_URL`, `BASEMENT_LOG_LEVEL`, `BASEMENT_SESSION_TTL`, `BASEMENT_AUDIT_RETENTION_DAYS` — server knobs with reasonable defaults.
- `BASEMENT_OIDC_*` — reserved for v0.4.0; harmless to ignore today.

---

## Roadmap

- **v0.2.0** — multi-cluster pivot (Connections as a top-level resource; in flight as of HEAD v0.1.32)
- **v0.3.0** — MinIO / OpenMaxIO driver
- **v0.4.0** — OIDC + capability-gated admin tokens
- **v0.5.0** — competitive milestone: Garage + MinIO + AWS drivers running side-by-side, OIDC, multi-cluster shipped
- **v0.6+** — end-user UI scaffold (browse, "My files," per-user grants)
- **v0.7** — upload, download, server-routed revocable share links
- **v0.8** — cross-backend copy / mirror / sync (the federation layer)
- **v0.9** — lifecycle wizards, scoped/expirable credentials, observability, migration tooling
- **v1.0** — feature-complete, "the answer"

We're staying on `0.x` until v1.0 actually means *the answer*. Don't read low version numbers as "not ready" — read them as "we haven't spent the v1.0 positioning signal yet."

---

## License

MIT. See [`LICENSE`](LICENSE).

Note: basement is the *management plane* (MIT) running on top of storage backends with their own licenses (Garage is AGPL, MinIO/OpenMaxIO is AGPL, AWS S3 is AWS). basement talks to those backends over HTTP as a separate process — no license inheritance. If you need a permissively-licensed admin UI on top of an AGPL data plane, that's what this is for.

---

## Contributing

Issues and PRs welcome. The project follows a structured contribution rhythm — design decisions are captured in a decision log, freshman tasks are written out as prompts before being implemented, and reviewers cite line numbers when pushing back. If you want to contribute something larger than a bugfix, open an issue first so we can align on shape.

For Garage-API specifics, basement targets Garage's **v2 admin API** (RPC-style under `/v2/*`) on Garage 1.0+. There's also a `garage-v1` driver for clusters still on the v1 admin API (Garage 1.0.1 in practice — the operator's home cluster ships v1 endpoints under `/v1/*`).

---

**One pane of glass for Garage, MinIO, OpenMaxIO, and AWS S3.** Pre-v0.5.0 today; that's the destination.
