# Feature-smoke bug report

Generated: 2026-05-23
Source: `scripts/feature-smoke.ts`
Target: `https://basement.pq.io`

Totals: pass=46, skip=2, fail=0, bugs=6

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

## Bugs

### A. Cluster + driver basics (3 bugs)

- **BUG01** (bucket-rename): PATCH aliases returned 200 but aliases ["feat-smoke-1779553964204-3jnjvp-bucket-a"] missing new feat-smoke-1779553964204-3jnjvp-bucket-a-renamed
- **BUG02** (key-grant-flags-lost): PATCH grant returned r=false w=false o=false (expected all true)
- **BUG03** (driver-info-endpoint): GET /api/v1/admin/clusters/{cid}/driver-info returned 404 — endpoint does not exist
  - repro: `GET /api/v1/admin/clusters/f0d431f2-3114-d614-d5b0-9ff8a235fdf9/driver-info → 404`

### C. Bucket object operations (1 bug)

- **BUG04** (multipart-abort): multipart abort → 400
  - repro: `body: {"error":{"code":"INVALID","message":"operation error S3: AbortMultipartUpload, serialization failed: serialization failed: input member Key must not be empty"}}
`

### D. Backups (1 bug)

- **BUG05** (backup-snapshots): list snapshots → 500

### L. WebDAV gateway (1 bug)

- **BUG06** (webdav-propfind-edge): PROPFIND /webdav/ blocked by edge (status=405) — workaround: hit basement directly
