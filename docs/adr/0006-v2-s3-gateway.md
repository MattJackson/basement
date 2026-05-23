# ADR-0006: v2.0 — basement IS a backend (S3 gateway)

- **Status**: Proposed — awaiting operator decision (HALT per [[project_long_haul_autonomy]])
- **Date**: 2026-05-23
- **Triggered by**: v1.x roadmap complete; ADR-0004 Option A queued as v2.0
- **Builds on**: [ADR-0001](./0001-rbac-three-tier-creds.md), [ADR-0002](./0002-region-tier-user-model.md), [ADR-0005](./0005-federation.md), v1.7 service accounts (bearer M2M auth), v1.9 Gateway interface
- **Enables**: "Migrate away from MinIO entirely"; basement becomes the canonical S3 endpoint over any backend

## Context

v1.0 → v1.10 shipped basement as a polished multi-backend admin UI + control plane. v2.0 is the long-haul vision: **basement IS the S3 endpoint**. Clients (aws-cli, rclone, mc, s3fs, Veeam, k8s CSI, etc.) point at `s3.basement.local` instead of their backend's S3 port. basement terminates the S3 request, verifies auth against basement-issued credentials, routes to backend(s) via the v1.6 federation map, audits + rate-limits + applies policy at the gateway layer.

The substrate is already there:
- **Gateway interface** (v1.9.0c) — `internal/gateway/Gateway` + `Backend` + `Registry`; stub `s3` gateway already registered. The whole S3 gateway is "implement the existing interface."
- **Backend abstraction** (v1.9.0c) — `Backend` is already S3-shaped: ListBuckets, ListObjects, Get/Put/Delete/Head/Copy. The data plane that powers WebDAV today powers the S3 gateway tomorrow.
- **Service accounts** (v1.7.0a) — basement-issued `BMNT`-prefixed credentials with per-capability scopes. The thing operators present to the gateway.
- **Bearer middleware** (v1.7.0b) — already verifies `BMNT:secret` shape; SigV4 is the additional verification format.
- **Federation** (v1.6) — bucket-to-backend(s) topology; gateway routes inbound requests over this map.

## Decision

Implement the S3 gateway as a new `internal/gateway/s3/` package satisfying the existing `Gateway` interface. SigV4 verification, S3 request parsing, response shaping. Multiplex into the existing `Backend` interface to reach storage.

### Scope of S3 API to implement

**Tier 1 — Required for `aws s3 ls/cp/rm/sync` + rclone + mc to work:**
- Service-level: `ListBuckets`, `HeadBucket`, `GetBucketLocation`
- Object-level: `ListObjectsV2`, `HeadObject`, `GetObject`, `PutObject`, `DeleteObject`, `CopyObject`, `DeleteObjects` (batch)
- Multipart: `CreateMultipartUpload`, `UploadPart`, `CompleteMultipartUpload`, `AbortMultipartUpload`, `ListParts`, `ListMultipartUploads`
- Presign: GET/PUT presigned URL handling (basement signs on behalf of operator)
- ~30 endpoints total

**Tier 2 — Common enough that operators expect them:**
- Bucket lifecycle ops (already wired in v0.9.0i): `GetBucketLifecycleConfiguration`, `PutBucketLifecycleConfiguration`, `DeleteBucketLifecycleConfiguration`
- Bucket versioning (v1.10.0a): `GetBucketVersioning`, `PutBucketVersioning`, `ListObjectVersions`, `GetObjectVersion`
- Bucket Object Lock (v1.10.0c): `GetObjectLockConfiguration`, `PutObjectLockConfiguration`, `GetObjectRetention`, `PutObjectRetention`, `GetObjectLegalHold`, `PutObjectLegalHold`
- Bucket encryption (v1.10.0d): `GetBucketEncryption`, `PutBucketEncryption`, `DeleteBucketEncryption`
- Object tagging: `GetObjectTagging`, `PutObjectTagging`, `DeleteObjectTagging` (forward to backend)

**Tier 3 — Out of scope for v2.0:**
- Replication (basement's federation supersedes this — operators federate via basement, don't configure S3 replication)
- Inventory, analytics, intelligent tiering (cost engine v2.4 territory)
- Access points, IAM policies (basement's policy matrix supersedes)
- Notifications/events (basement's webhooks supersede)

### Auth model

**SigV4 verification** is the primary mechanism. The gateway:
1. Parses `Authorization: AWS4-HMAC-SHA256 Credential=BMNT.../...region/s3/aws4_request, SignedHeaders=..., Signature=...`
2. Extracts AccessKeyID (`BMNT...`) and looks up via `serviceaccount.GetByAccessKey`
3. Reconstructs the canonical request from method/path/query/headers/body-hash
4. Computes signing key from `serviceaccount.Secret` (the bcrypt hash WILL NOT work — gateway needs the plaintext secret derivation; **decision**: store an HMAC-derived signing-key seed alongside bcrypt hash at mint time, OR ship a separate gateway-only verification path)
5. HMAC-compares with provided signature
6. On success: build `UserContext` per `gateway.Backend` interface; dispatch to backend op

**Bearer-via-Basic fallback** (already supported by `internal/auth/bearer.go` v1.7.0b): for tools that don't speak SigV4 (rare in S3 land). Lower priority.

### Routing logic

For each inbound S3 op:
1. Extract bucket from request (path-style `/{bucket}/{key}` OR virtual-host `{bucket}.{host}/{key}`)
2. Resolve bucket to backend(s) via the federation map:
   - If bucket is part of a `FederatedBucket`: READS → nearest healthy replica (latency + lag-aware); WRITES → primary
   - If bucket is a plain UserRegion bucket: route directly to that region's backend
3. Forward op via `Backend` interface methods
4. Stream response back to client

### Data plane considerations

- **Stream pass-through**: never buffer Get/Put bodies in basement memory. Use `io.Copy` style streaming from backend → client (GET) or client → backend (PUT).
- **HTTP/2**: enabled on the gateway listener (Go's net/http server supports it natively over TLS).
- **Connection pooling**: per-backend `http.Client` with sane MaxIdleConns + IdleConnTimeout.
- **Multipart**: relay parts directly — basement holds the upload-ID mapping (basement-issued ID → backend-issued ID) but never buffers parts.
- **Rate limiting**: per-AccessKey token-bucket. Configurable in OrgCapabilities.

### Listen address

`s3.gateway.listen` config (default `:8443`). Operator's TLS cert (basement's existing TLS setup OR a separate cert via Caddy/ACME). DNS: operator points `s3.basement.local` (or whatever) at the basement host.

### Multi-bucket transparency

Same client URL can target buckets across DIFFERENT physical backends:
```
aws s3 cp file1.txt s3://lsi/        # routes to home Garage
aws s3 cp file2.txt s3://offsite/    # routes to B2
```

The federation map (v1.6) is the routing table. No federation needed for a simple "this bucket lives on this region" mapping — that's just the UserRegion → backend pointer that already exists.

### Audit

Every S3 op emits an audit entry with: actor (SA ID), action (`s3:get_object` etc.), resource (`bucket/key`), result, IP. The audit log already supports this shape; gateway just emits.

Audit volume will be HIGH (one entry per S3 op). Defaults: only mutating ops audited by default; reads logged at INFO level (configurable). Operator can tune via OrgCapabilities.

## Consequences

### Good

- **One S3 endpoint** for the operator's environment, no matter how many physical backends are behind it
- **basement owns auth + audit** uniformly across all backends — no more chasing audit logs in N different MinIO/Garage/AWS consoles
- **Cross-backend transparency**: federated buckets appear as one bucket to clients
- **Migration story**: operator can swap a bucket's backend (Garage → AWS, or vice versa) WITHOUT clients reconfiguring — federation handles the data move, gateway keeps serving the same URL
- **Differentiator**: no other open-source multi-backend admin UI does this. MinIO is single-backend; OpenMaxIO is MinIO-only; khairul169/garage-webui is Garage-only

### Painful

- **HUGE scope** — implementing enough S3 to satisfy real clients is months of work. Even Tier 1 is ~30 endpoints, each with subtle edge cases (multipart edge cases alone are notorious)
- **Bandwidth bottleneck**: basement now sits in the data path. CPU + bandwidth at the basement host become the constraint. Mitigated by streaming + HTTP/2 but not eliminated
- **Single point of failure**: today basement crashing = admin pane gone. Post-v2.0 it = all object access gone. Operator needs to design for HA (run multiple basement instances behind a load balancer + share state via a coordination layer)
- **Compatibility surface is huge**: AWS keeps adding S3 features; basement must keep up
- **SigV4 verification is fiddly**: subtle signing-key derivation bugs cause auth failures that look like client bugs. Need exhaustive test coverage against real aws-cli + rclone + mc traffic
- **Service-account secret storage**: bcrypt hash isn't reversible, so SigV4 verification needs a different approach. Three options (pick one):
  1. **Store HMAC-derivable secret material**: encrypt the plaintext secret at rest (AES-GCM keyed off JWT secret, same as UserRegion.SecretEnc). SigV4 verifier decrypts to derive signing key.
  2. **Pre-compute SigV4 signing-key material at mint**: derive `kDate / kRegion / kService / kSigning` for the next N days at mint + cache. Refresh as needed.
  3. **Maintain two credentials per SA**: a bcrypt hash for bearer auth + an encrypted plaintext for SigV4. Doubles storage but separates concerns.
  
  **Recommendation: Option 1** (encrypt at rest). Simplest; reuses existing crypto infra; matches how AWS itself stores secrets.

### Open questions

- **HA story**: should v2.0 ship with multi-instance coordination (shared state in Redis/etcd)? Or punt to v2.x? **Recommendation**: punt. Single-instance basement gateway works for home-lab + small-team use cases that are basement's audience. HA is enterprise feature; ship v2.0 + add HA in v2.5+ if demand arises.
- **TLS cert mgmt**: should basement auto-ACME its gateway cert? Or expect operator's reverse proxy (Caddy) to terminate? **Recommendation**: both. Default config = expect operator's reverse proxy. Optional ACME integration as a v2.x add.
- **Multi-region routing**: when a federated bucket has replicas in different geographic regions, the gateway should route READS to the nearest replica. v2.0 first cut = primary always, latency-based routing in v2.1.

## Implementation plan

**Approximately 8 cycles for v2.0.** Each is substantial.

| Cycle | Deliverable |
|---|---|
| v2.0.0a | SigV4 verifier — pure crypto package, no HTTP yet. Test against AWS SDK Sign() output. |
| v2.0.0b | Service-account secret encryption-at-rest migration; gateway-compatible signing-key derivation. Backward-compat: bcrypt still used for bearer auth. |
| v2.0.0c | S3 request parser — path-style + virtual-host addressing. Handles every S3 verb's URL/query/header parsing. Pure parsing, no dispatch yet. |
| v2.0.0d | S3 response shaper — XML serialization for every supported op. Handles Error responses correctly (AWS S3 error code matrix). |
| v2.0.0e | Gateway implementation — Tier 1 read ops (ListBuckets / HeadBucket / ListObjectsV2 / HeadObject / GetObject). Wires v2.0.0a-d. Stream pass-through. End-to-end against aws-cli ls/cp from S3 to local. |
| v2.0.0f | Gateway Tier 1 write ops — PutObject / DeleteObject / CopyObject / multipart. End-to-end against aws s3 cp local-to-S3 + sync. |
| v2.0.0g | Tier 2 ops — versioning + object-lock + encryption + lifecycle + tagging passthroughs. Federation routing for federated buckets. |
| v2.0.0h | Release: smoke (extensive — every supported op verified via aws-cli OR mc OR rclone), release notes, v2.0 tag. Mandatory comprehensive smoke + manual exercise with all four clients. |

**Estimated calendar time**: 2 months of focused work assuming the freshman-dispatch cadence we've maintained through v1.x.

## What I need from operator

1. **Approve the scope** — Tier 1 + Tier 2 as listed, Tier 3 punted? Or add/remove items?
2. **Approve the secret storage approach** — Option 1 (encrypt at rest) recommended?
3. **HA story for v2.0**: ship single-instance + defer HA? Or block v2.0 on HA?
4. **TLS cert mgmt**: bring-your-own (current Caddy posture) confirmed, or basement-managed ACME?
5. **Listen port**: `:8443` reasonable default, or different?
6. **Naming**: `internal/gateway/s3/` package name OK, or different?

After your call: dispatch v2.0.0a (SigV4 verifier) + the chain.

## Halt

Senior is halting per `[[project_long_haul_autonomy]]`. No further dispatches without operator decision.
