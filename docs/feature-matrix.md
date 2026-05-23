# Feature matrix — capability × driver

Honest answer to "does basement support feature X on backend Y?"
This is what the UI consults at runtime — capability flags drive
gating, never driver-name checks.

Source of truth: each driver's `Capabilities(ctx) Caps` impl —
`internal/drivers/garage/cluster.go`,
`internal/drivers/garage_v1/cluster.go`,
`internal/drivers/aws_s3/cluster.go`,
`internal/drivers/minio/cluster.go`.

## Driver-level capabilities (`driver.Caps`)

| Capability | AWS S3 | MinIO | Garage v1 | Garage v2 | Notes |
|---|:---:|:---:|:---:|:---:|---|
| Cluster layout | readonly | readonly | stage-apply-revert | stage-apply-revert | Garage's nodes/zones/capacity layout is editable from the UI; AWS + MinIO are managed services, no layout knob. |
| Bucket quotas | no | no | yes | yes | Garage exposes per-bucket bytes + objects quotas. |
| Bucket aliases | no | no | yes | yes | Garage buckets carry multiple aliases; AWS + MinIO are name-keyed. |
| Key model | IAM | IAM | garage | garage | Garage's API mints per-bucket-grant keys; AWS + MinIO use IAM-style keys with per-bucket policy attached. |
| Presigned URLs | yes | yes | when S3 wired | when S3 wired | Garage drivers advertise presign once the operator wires S3 endpoint + creds (admin-only deploys without S3 advertise false). |
| Multipart upload | yes | yes | when S3 wired | when S3 wired | Same gating as presign. |
| Object browse | yes | yes | when S3 wired | when S3 wired | UI bucket browser uses ListObjectsV2; admin-only deploys advertise false. |
| Server-side copy | yes | when S3 wired | when S3 wired | when S3 wired | `s3:CopyObject` for same-backend copies. |
| Bucket versioning | yes | yes | **no** | **no** | Garage's content-addressed block store conflicts with versioned overwrites — driver advertises Versioning=false; UI renders an Unsupported card. |

## v1.10 compliance + integrity surfaces

| Surface | AWS S3 | MinIO | Garage v1 | Garage v2 | Notes |
|---|:---:|:---:|:---:|:---:|---|
| Bucket versioning UI | full | full | stub (501) | stub (501) | UI renders an Unsupported branch on Garage. |
| Object Lock (Governance + Compliance + Legal hold) | full | full | stub (501) | stub (501) | Same gating — Garage advertises unsupported. |
| Default SSE-S3 | full | full | stub (501) | stub (501) | Bucket-level default SSE for at-rest encryption. |
| Default SSE-KMS | full | live with KMS | stub (501) | stub (501) | Requires a configured KMS key ID. |

## Cross-cutting features

These work across all four drivers (capability-agnostic):

| Feature | Coverage | Notes |
|---|---|---|
| Multi-cluster admin | all 4 | Add N clusters of any driver mix from `/admin/clusters`. |
| Cross-cluster bucket list | all 4 | `/admin/buckets` aggregates across every registered cluster. |
| Cluster-to-cluster migrate wizard | all 4 | Moves buckets across any driver pair. |
| Per-user encrypted region keychain | all 4 | `internal/store/user_regions.go` — region tier (ADR-0002). |
| Sudo-style admin elevation | all 4 | UI-tier, not backend-specific (ADR-0003). |
| Policy matrix + simulator | all 4 | 27 capabilities × roles × scopes, evaluated in basement's policy engine. |
| Audit log | all 4 | Every mutating op writes a JSONL entry to `{DATA_DIR}/audit/`. |
| Scheduled bucket-to-bucket backups (v1.5) | all 4 | GFS retention; mirror + snapshot modes. Backs up to any other registered cluster. |
| Multi-backend federation + failover (v1.6) | all 4 | `FederatedBucket` spans any mix of drivers. |
| Service accounts (v1.7) | all 4 | `BMNT…:secret` bearer credentials with per-capability scope. |
| HMAC-signed bucket webhooks (v1.7) | all 4 | Outbound POST with `X-Basement-Signature`. |
| MCP server (v1.8) | all 4 | Ten tools over the same Driver interface; backend-agnostic. |
| Mobile PWA (v1.8) | all 4 | UI-tier; works against any driver. |
| WebDAV gateway (v1.9) | all 4 | Reads from any region the user has access to. |
| Layout editor | Garage v1 + Garage v2 | UI hidden on AWS/MinIO (Layout=`readonly` capability). |
| Garage block-scrub UI | Garage v1 + Garage v2 | Garage-specific admin op; UI hidden elsewhere. |
| First-run onboarding wizard (v1.11) | all 4 | Driver picker is part of the cluster step; any combination works. |

## Driver implementation status

| Driver | Admin API | S3 data plane | Notes |
|---|:---:|:---:|---|
| AWS S3 | n/a (managed) | full | IAM key model; no cluster-management ops. |
| MinIO | full | full | `mc admin` shape; full S3 + admin parity. |
| Garage v1 | full | when S3 wired | Production-mature Garage v1 admin API. |
| Garage v2 | full | when S3 wired | First UI for the Garage v2 admin API (v1.11.0.1 fixes the admin-tier connection path; v1.11.0.5 fixes the key-grant decode). |

"When S3 wired" means: the driver advertises Presign + Multipart +
ObjectBrowse + ServerSideCopy as `true` only when an S3 endpoint +
default access key + secret are configured. Admin-only Garage
connections (no S3 endpoint) advertise those flags as `false` and
the UI hides the corresponding panels.

## Known driver-parity gaps (v1.11.0.5 cycle)

From `feature-smoke-bugs.md`:

| Bug | Area | Drivers affected | Status |
|---|---|---|---|
| BUG01 | bucket-rename via PATCH `aliases` no-ops | Garage v1 + v2 | OPEN — needs alias-diff implementation in `UpdateBucket`. |
| BUG03 | `/admin/clusters/{cid}/driver-info` 404 | all (endpoint missing entirely) | OPEN — handler needs wiring. |
| BUG04 | multipart-abort drops object key | all (router shape missing `{key}`) | OPEN — route needs the key segment. |
| BUG05 | snapshots-list 500 on UserRegion-backed backup | all (resolveBackupConn) | OPEN — should degrade to 200 with `note`. |
| BUG06 | WebDAV PROPFIND blocked at reverse-proxy edge | all (Caddyfile config) | OPEN — `deploy/Caddyfile` needs WebDAV-verb allowlist. |

See [`feature-smoke-bugs.md`](feature-smoke-bugs.md) for the full
write-up + repro per bug.

## See also

- [`architecture.md`](architecture.md) — where driver code lives in
  the tree + how it plugs into the request lifecycle.
- [`integrations/`](integrations/) — gateway-tier capability surface
  (orthogonal to the driver axis).
- [`adr/`](adr/) — why the capability-flag approach (no driver-name
  checks anywhere in the UI).
