# Feature-smoke bug report (v1.11.0.5 cycle)

Generated: 2026-05-23
Source: `scripts/feature-smoke.ts`
Target: `https://basement.pq.io`

Totals: pass=46, skip=2, fail=0, bugs=6

## Status as of v1.11.0.6 (driver/handler sweep)

| Bug   | Area                  | Status                                                                                |
|-------|-----------------------|---------------------------------------------------------------------------------------|
| BUG01 | bucket-rename         | **FIXED** in v1.11.0.6 (Garage v2 + v1 alias-diff via Add/RemoveBucketAlias).         |
| BUG02 | key-grant-flags-lost  | **FIXED** in v1.11.0.5 (`internal/drivers/garage/keys.go` decode fix).                |
| BUG03 | driver-info endpoint  | **FIXED** in v1.11.0.6 (`driverInfoHandler` wired under `adminG`, per-cluster Caps).  |
| BUG04 | multipart-abort key   | **FIXED** in v1.11.0.6 (abort handler accepts `?key=` query param; FE + smoke).       |
| BUG05 | snapshots 500         | **FIXED** in v1.11.0.6 (`snapshotListingDriver` falls back to UserRegion S3 key).     |
| BUG06 | WebDAV PROPFIND edge  | **OPEN** — Caddyfile in `deploy/` needs WebDAV verbs whitelist; deployment config.    |

All driver/handler-side bugs from the v1.11.0.5 smoke run are closed.
BUG06 is the only remaining item, and it lives in operator-owned
infra (Caddyfile) rather than basement code, so it tracks separately.



## Per-feature pass/fail summary

| Feature | Pass | Fail | Skip |
|---------|------|------|------|
| bootstrap | 4 | 0 | 0 |
| A. Cluster + driver basics | 7 | 0 | 0 |
| B. UserRegions | 5 | 0 | 0 |
| C. Bucket object operations | 2 | 0 | 0 |
| D. Backups | 6 | 0 | 0 |
| E. Federations | 0 | 0 | 1 |
| F. Webhooks | 4 | 0 | 0 |
| G. Service accounts | 3 | 0 | 0 |
| H. Lifecycle rules | 2 | 0 | 0 |
| I. Versioning (Garage stub) | 2 | 0 | 0 |
| J. Object Lock (Garage stub) | 2 | 0 | 0 |
| K. SSE (Garage stub) | 2 | 0 | 0 |
| L. WebDAV gateway | 2 | 0 | 0 |
| M. MCP server | 0 | 0 | 1 |
| N. Audit log | 2 | 0 | 0 |
| O. Onboarding wizard | 2 | 0 | 0 |
| UI sanity (one render) | 1 | 0 | 0 |

## Skips (deferred to follow-up cycles)

| Feature | Reason |
|---------|--------|
| E. Federations | Blocked on `v1.11.0.4` federation fix landing. Smoke coverage for create / replicate-wait / failover / resync / delete is scaffolded but disabled until the parallel cycle ships. |
| M. MCP server | `basement-mcp` is a stdio-only process tested via `cmd/basement-mcp` unit tests; not exercisable through the HTTP smoke surface. |

## Bugs

### A. Cluster + driver basics (3 bugs)

#### BUG01 — bucket-rename: PATCH `/admin/clusters/{cid}/buckets/{bid}` with `{aliases:[...]}` silently no-ops **[FIXED — v1.11.0.6]**

PATCH returns 200 with the **old** aliases array unchanged. The Garage v2 `UpdateBucket` driver impl (`internal/drivers/garage/buckets.go:59`) reads only `update.Quotas` from the `driver.BucketUpdate` body — `update.Aliases` is ignored entirely. Garage v2's alias model is separate from `UpdateBucket`: aliases are added/removed via `AddBucketAlias` / `RemoveBucketAlias` endpoints. The handler in `internal/api/admin_buckets.go:updateBucketHandler` happily passes the `aliases` field through and returns success, then re-fetches and returns the unchanged bucket.

Repro: `PATCH /api/v1/admin/clusters/{garage-v2-cid}/buckets/{bid}` with body `{"aliases":["new-name"]}` → 200, body still shows the old alias.

Fix (v1.11.0.6): `garage.driver.UpdateBucket` now diffs the requested aliases against `GetBucketInfo.globalAliases` and fans out per-delta `AddBucketAlias` / `RemoveBucketAlias` calls (adds-before-removes so a rename never zeroes the alias set mid-flight). `garage_v1` driver got the parity fix using the dedicated `/v1/bucket/alias/global` PUT/DELETE endpoints. Both drivers re-fetch via `GetBucketInfo` after the diff so the response carries the canonical post-update state.

#### BUG02 — key-grant-flags-lost: Garage v2 driver decoded per-bucket permissions wrong, dropping every grant to all-false **[FIXED INLINE — v1.11.0.5]**

`internal/drivers/garage/keys.go: getKeyInfoResponse.BucketsPermissions` was typed as `[]bucketPermissionResp` (flat `read/write/owner`), but the Garage v2 `GetKeyInfo` response nests them under `permissions: {read, write, owner}` (KeyInfoBucketResponse, garage-admin-v2.json:3490-3527). `keyFromInfo` therefore returned all-false flags on every grant readback, which silently routed every downstream call signed with that key into 401 `USER_KEY_REJECTED` against the backend.

Repro before fix: `PATCH /admin/clusters/{cid}/keys/{kid}` with `{bucketsPermissions:[{bucketId,read:true,write:true,owner:true}]}` returns 200 with `buckets:[{...read:false,write:false,owner:false}]` even though `AllowBucketKey` succeeded on the Garage side.

Fix: switch the field to `[]keyInfoBucketResponse` and read `b.Permissions.Read/Write/Owner` in `keyFromInfo`. `garage_v1` driver already handled this correctly.

The `BUG02` line still appears in the live smoke output because the deploy is at v1.11.0.3; the fix lands with v1.11.0.5.

#### BUG03 — driver-info-endpoint: GET `/api/v1/admin/clusters/{cid}/driver-info` returns 404 **[FIXED — v1.11.0.6]**

The cycle brief assumed a per-cluster driver-capabilities endpoint exists. Inventory of `internal/api/server.go` routes shows only the global `/api/v1/capabilities` (which queries `s.drv`, the default driver), not a `{cid}`-scoped variant. A per-cluster endpoint matters once a deploy holds multiple cluster connections with different drivers (e.g. one Garage v2 + one S3 + one MinIO).

Repro: `GET /api/v1/admin/clusters/{any-cid}/driver-info` → 404.

Fix (v1.11.0.6): added `driverInfoHandler` in `admin_clusters.go` and wired the route under `adminG.Get("/admin/clusters/{cid}/driver-info", s.driverInfoHandler)`. Uses the v1.11.0.2 `driverForRouteCluster` dispatch pattern + reads the `Driver` name off the `Connection` record so the wire response includes both the configured driver (e.g. `garage` vs `garage-v1`) and the live `Caps` from that cluster's driver instance.

### C. Bucket object operations (1 bug)

#### BUG04 — multipart-abort handler routes empty Key to S3, gets rejected **[FIXED — v1.11.0.6]**

`internal/api/user_regions.go:userAbortRegionMultipartHandler` constructs `driver.MultipartUpload{UploadID: uploadID, Bucket: bid}` and passes that to `drv.AbortMultipart`. The Garage v2 driver (`internal/drivers/garage/s3.go:305`) calls `s3.AbortMultipartUploadInput{Bucket, Key, UploadId}` — but `upload.Key` is empty because the route `DELETE /multipart/{uploadId}` has no `{key}` path segment. AWS S3 SDK rejects the request before it leaves the wire.

Repro:
```
POST /api/v1/user/regions/{rid}/buckets/{bid}/multipart/init {"key":"x","contentType":"y"} → 200 with uploadId
DELETE /api/v1/user/regions/{rid}/buckets/{bid}/multipart/{uploadId} → 400 INVALID with "input member Key must not be empty"
```

Side-effect: the multipart upload stays in-flight on the backend, which then blocks bucket-delete with `BucketNotEmpty`.

Fix (v1.11.0.6): the abort handler now accepts the object key as a `?key=<urlencoded>` query param (preferred — keeps DELETE bodyless) or a `{"key":"..."}` JSON body (forward-compat). Missing key returns 400 `INVALID_REQUEST` with a clear `object key required (pass as ?key=<encoded> query param)` message instead of relying on the SDK's downstream error string. FE callers (`useUserRegionMultipartAbort`, `UploadDialog`) and the smoke harness updated to pass the key.

### D. Backups (1 bug)

#### BUG05 — list-snapshots returns 500 on a freshly-created snapshot-mode backup **[FIXED — v1.11.0.6]**

`GET /api/v1/user/backups/{id}/snapshots` returns 500 when the backup is snapshot-mode but has never run. `userListBackupSnapshotsHandler` (`internal/api/user_backups.go:454`) calls `resolveBackupConn` which returns `region has no admin bridge` for UserRegion-backed destinations (since UserRegions don't carry the admin token). The 500 should be either a 200 with `[]` or a 503 with a clear "destination region has no admin bridge" message — never a bare 500.

Repro: create a snapshot-mode backup pointing at a UserRegion destination, then `GET /user/backups/{id}/snapshots` → 500 `SNAPSHOT_LIST_FAILED: region has no admin bridge (endpoint "...")`.

Fix (v1.11.0.6): better than the proposed degradation — the snapshot lister now actually works against UserRegion destinations. Added `snapshotListingDriver` helper that tries the admin-bridge path first (unchanged behaviour for admin Connection destinations) and falls back to building a region-scoped driver from the UserRegion's stored S3 credentials when no admin bridge exists. Snapshot enumeration is pure S3 `ListObjects` against the destination bucket's snapshot-prefix tree — exactly what the UserRegion's own key was authorized to write, so the same key can read it back. No more 500; the endpoint returns the real snapshot list (or `[]` when no snapshots have been written yet) regardless of destination tier.

### L. WebDAV gateway (1 bug)

#### BUG06 — webdav-propfind-edge: PROPFIND `/webdav/` returns 405 (blocked by Caddy edge)

The reverse-proxy in front of basement strips non-standard verbs (PROPFIND, PROPPATCH, MKCOL, MOVE, COPY, LOCK, UNLOCK). The basement WebDAV gateway speaks them all, but external clients hitting basement.pq.io never reach the handler. Workaround: hit basement directly on its internal port (not through Caddy), or extend the Caddyfile to whitelist WebDAV verbs.

Repro: `PROPFIND https://basement.pq.io/webdav/` with `Depth: 0` and Basic auth → 405 from edge.

Fix scope: trivial deployment config — `deploy/Caddyfile` needs an explicit `methods` whitelist that includes the WebDAV verbs. Punted to a follow-up cycle (touches infra, not code).

## Safety verification

The smoke run captures a baseline of operator-owned resources (non-`feat-smoke-`-prefixed regions / service-accounts / webhooks / backups / federations) before any mutation. End-of-run compares the snapshot. Both runs after the v1.11.0.5 fix:

```
baseline: regions=1 sa=0 webhooks=0 backups=0 federations=0
after:    regions=1 sa=0 webhooks=0 backups=0 federations=0
PASS — operator-snapshot drift check (baseline == after)
```

Operator's `classe` cluster and `lsi` / `cheshire` buckets verified untouched after the run.
