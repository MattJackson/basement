# basement architecture

This is the operator/contributor walkthrough of how basement is put
together. It supplements the ADRs (which capture *why* each major
piece exists) with a *what talks to what* view.

## One-paragraph summary

basement is a single static Go binary serving an embedded React app
plus a JSON HTTP API. The Go side is a chi router fronting a small
set of stores (JSON files under `BASEMENT_DATA_DIR`, atomic write
via tmp + fsync + rename) plus a driver registry that talks to
backend S3 / Garage / MinIO admin APIs. The React app uses TanStack
Router + Query + shadcn/ui + Tailwind 4. Auth is JWT in a `__Host-`
cookie (local users + OIDC) plus bearer credentials for M2M
(`BMNT…` service accounts). Everything sensitive is AES-256-GCM
encrypted at rest, keyed off `BASEMENT_JWT_SECRET`.

## Process model

```
                +-------------------------+
  inbound HTTP  |   basement (Go binary)  |
  TCP :8080     |  - chi router            |
  ============> |  - JSON API              |
                |  - embedded React (vfs)  |
                |  - WebDAV gateway        |
                |  - /metrics              |
                +-----+-----+--------+-----+
                      |     |        |
                      |     |        +--- audit/YYYY-MM-DD.log (JSONL)
                      |     |
                      |     +--- BASEMENT_DATA_DIR/*.json  (state)
                      |
                      |          (admin API)              (S3 API)
                      +-->  Garage / MinIO / AWS S3  <-->  same backends
                              (admin token)                (per-user S3 keys)
```

One process, one port, no sidecar daemons. Object data does not
traverse basement in v1.x — drivers issue HTTP calls and stream
responses back to the client without staging bytes through the
control plane. (v2.0 S3 gateway changes this for inbound writes
on a per-bucket basis; see [ADR-0006](adr/0006-v2-s3-gateway.md).)

## Top-level package layout

```
cmd/
  basement-server/    # main(): config load, wire registry, start HTTP
  basement-mcp/       # MCP stdio server (v1.8) — ten tools over a Backend client

internal/
  api/                # chi handlers: /api/v1/admin/* + /api/v1/user/* + /api/v1/auth/*
  auth/               # bcrypt + JWT + OIDC + sudo elevation (ADR-0003)
  audit/              # daily JSONL append-only log + Prometheus bridge
  config/             # env-var load + v1.11.0c auto-bootstrap (.jwt-secret, .initial-admin-password)
  driver/             # Driver interface + capability flags + Caps shape
  drivers/
    garage/           # Garage v2 admin API client (driverName="garage")
    garage_v1/        # Garage v1 admin API client (driverName="garage-v1")
    aws_s3/           # AWS S3 driver (IAM-shaped keys; no admin layout)
    minio/            # MinIO driver (S3 + mc-admin shape)
  federation/         # FederatedBucket types + replication engine (ADR-0005)
  gateway/            # Gateway/Backend/Registry interfaces (v1.9)
    webdav/           # WebDAV implementation
    smb/ nfs/ ftp/ s3/  # registered stubs (v2.x roadmap)
  metrics/            # Prometheus exporter + 14 metric families (v1.11.0f)
  serviceaccount/     # M2M BMNT-prefixed bearer creds (v1.7)
  store/              # JSON file persistence with atomic write + AES-GCM crypto
  web/                # embed.FS for the built React app (frontend/dist/)
  webhooks/           # outbound webhook dispatcher with HMAC signing (v1.7)

frontend/             # React 19 + TanStack Router/Query + shadcn/ui + Tailwind 4
  src/routes/         # file-system-routed pages: admin/* + files/* + auth/*
```

## Request lifecycle

A logged-in operator clicking *Create bucket* in the UI:

1. Browser sends `POST /api/v1/admin/clusters/{cid}/buckets` with a JSON body and the `__Host-basement_session` cookie.
2. `internal/api/server.go` middleware validates the JWT, loads the user, checks elevation state (ADR-0003) and policy (`capability:scope` lookup against the role assignments).
3. The handler resolves the cluster via `s.reg.For(ctx, cid)` — `reg` is the per-cluster driver `Registry`. Each cluster carries its own decrypted admin token.
4. The driver method (`internal/drivers/garage/buckets.go:CreateBucket`) shapes the backend request, signs the admin call, and parses the response.
5. The handler writes an audit entry via `audit.Logger.Append(ctx, action, scope, result)`; the metrics bridge increments `basement_audit_events_total{action="bucket:create"}` in lock-step.
6. JSON response back to the browser; TanStack Query invalidates the bucket-list cache and the UI re-renders.

Every mutating request follows this shape: cookie → middleware →
handler → driver → audit + metrics → JSON response.

## Auth

Three auth surfaces. They share storage but differ on the wire:

| Auth tier | Where | Wire shape | Stored as |
|-----------|-------|------------|-----------|
| Local user (UI) | `__Host-basement_session` cookie | Bcrypt-verified login, HS256 JWT in cookie | bcrypt hash in `users.json` |
| OIDC user (UI) | Same cookie, `__Host-basement_oidc_state` for the dance | go-oidc verifier; provisioning rules in `org_capabilities.json` | Subject ID in `users.json` |
| Service account (M2M) | `Authorization: Bearer BMNT…:secret` | Bcrypt-verified at handler entry | bcrypt of the secret half in `service_accounts.json` |

The cookie carries `SameSite=Strict` and the `__Host-` prefix
(implies `Secure` + `Path=/`, no `Domain`). All three tiers run
through the same `authMiddleware` in `internal/api/server.go`; the
identity it produces (`*auth.User`) drives every downstream policy
check.

Sudo-style admin elevation (ADR-0003) is a per-session state machine
gated on the elevation TTL stored in `org_capabilities.json`.
Destructive admin verbs require `ELEVATED` mode; the UI shows a
banner while elevated.

## Persistence

basement holds its own state under `BASEMENT_DATA_DIR` (default
`/var/lib/basement`) as JSON files written atomically:

```
{DATA_DIR}/
  .jwt-secret                    # 32 random bytes (hex), 0600 — v1.11.0c bootstrap
  .initial-admin-password        # 24-char plaintext, 0600 — v1.11.0c bootstrap
  users.json                     # local users + OIDC subject mappings
  user_regions.json              # per-user S3 creds (AES-256-GCM encrypted)
  connections.json               # cluster admin tokens (encrypted)
  bucket_grants.json             # legacy per-bucket grants (retained for migration)
  invites.json                   # outstanding invite tokens
  shares.json                    # public share tokens
  oidc_group_mappings.json       # OIDC group -> basement role mappings
  org_capabilities.json          # org-wide settings (elevation TTL, gateway toggles)
  service_accounts.json          # M2M bearer creds (secrets hashed, AKIDs visible)
  webhooks.json                  # bucket-event webhook subscriptions + HMAC secrets
  federated_buckets.json         # FederatedBucket records (ADR-0005)
  backups.json                   # scheduled S3 -> S3 backup jobs (v1.5)
  audit/YYYY-MM-DD.log           # daily append-only audit log (JSONL)
```

Each `*.json` file is written as `write to tmp file → fsync →
rename`, so any single file is always self-consistent. Cross-file
consistency is not guaranteed without a filesystem snapshot — see
[`deployment/backup-basement.md`](deployment/backup-basement.md).

## Driver registry

The driver layer is a thin adapter over each backend's admin API.
The `Driver` interface (`internal/driver/driver.go`) declares the
verb set; each backend has its own subpackage:

- `internal/drivers/garage/` — Garage v2 admin API (driverName=`garage`)
- `internal/drivers/garage_v1/` — Garage v1 admin API (driverName=`garage-v1`)
- `internal/drivers/aws_s3/` — AWS S3 + IAM keys (driverName=`aws-s3`)
- `internal/drivers/minio/` — MinIO admin + S3 (driverName=`minio`)

Each driver implements `Capabilities(ctx) Caps` advertising what it
supports. The UI gates feature surfaces on capability flags — no
driver-name checks. See [`feature-matrix.md`](feature-matrix.md) for
the full per-driver matrix.

The cluster registry (`internal/driver.Registry`) holds one driver
instance per (cluster, owner) and decrypts the admin token on demand.

## Federation engine (ADR-0005, v1.6)

The federation engine continuously mirrors writes from a primary
backend to replicas:

- `internal/federation/types.go` — `FederatedBucket{Primary, Replicas[], Policy}`.
- `internal/federation/engine.go` — polling loop with per-bucket
  ticks; for each replica, `computeDiff(LastSync)` returns objects
  to push, then `replicateBatch` streams them.
- `audit:federation:replicate_object` for each successful object,
  `audit:federation:replicate_error` for failures.
- Health: per-replica `LastSync` + `LagBytes` + `Health`
  (`in-sync` | `lagging` | `stale` | `pending` | `broken`).
- Event-driven (v1.7) — bucket-event webhooks fire an in-process
  pub/sub topic that the engine subscribes to, collapsing polling
  ticks into sub-second replication latency.

The v1.11.0.4 fix (LastSyncSlack) addresses the whole-second-mtime
race that left replicas empty after a boot tick fired at a
nanosecond boundary.

## Metrics + logging

`/metrics` exposes 14 Prometheus families (v1.11.0f). The audit
pipeline drives most of them automatically via the `audit.Logger`
wrapper — handlers don't manually instrument. See
[`observability/README.md`](observability/README.md).

All structured logs flow through `log/slog` with
`BASEMENT_LOG_FORMAT=json|text`. A request log line per HTTP
request, plus per-component slog lines (federation tick, webhook
delivery, etc.).

## Gateway architecture (v1.9)

The `Gateway` axis (`internal/gateway/`) is orthogonal to the
`Driver` axis. Gateways control *how clients talk to basement*;
drivers control *how basement talks to storage*.

- `internal/gateway/Gateway` — interface every gateway implements
  (`Name`, `Capabilities`, `Start/Stop`, `HTTPHandler` for
  HTTP-mounted or `ListenAddress` for port-bound).
- `internal/gateway/Backend` — narrowed Auth + data-plane contract
  every gateway calls (`AuthBasic` / `AuthBearer` / `AuthSigV4`,
  `ListBuckets`, `Get/Put/Delete/Copy`, etc.).
- `internal/gateway/Registry` — boot-time roster the operator UI
  reads at `/admin/system → Gateways`.

WebDAV (`internal/gateway/webdav/`) is the only implemented gateway
in v1.x. SMB / NFS / FTP / S3 are registered stubs; full
implementations land in the v2.x line (see
[`integrations/`](integrations/) per-protocol notes).

## v2.0 — basement IS a backend (S3 gateway)

The next major. Inbound S3 requests terminated and SigV4-verified
by basement, routed via the v1.6 federation topology, authed against
v1.7 service-account credentials. The `Backend` interface that v1.9
shipped is already S3-shaped — the v2.0 implementation is a SigV4
verifier + an S3 request parser on top of the same data plane that
powers WebDAV today. Full design in
[ADR-0006](adr/0006-v2-s3-gateway.md) — Proposed, awaiting operator
decision as of v1.11.

## See also

- [`adr/`](adr/) — design rationale for each major piece (RBAC,
  region model, sudo elevation, v2 scoping, federation, v2 S3 gateway).
- [`feature-matrix.md`](feature-matrix.md) — capability × driver table.
- [`testing.md`](testing.md) — test pyramid + CI gates.
- [`deployment/`](deployment/) — operator-side production posture.
- [`integrations/`](integrations/) — per-gateway client guides.
