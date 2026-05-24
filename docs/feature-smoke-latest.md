# Feature-smoke bug report

Generated: 2026-05-24
Source: `scripts/feature-smoke.ts`
Target: `https://basement.pq.io`

Totals: pass=46, skip=2, fail=0, bugs=2

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

### L. WebDAV gateway (2 bugs)

- **BUG01** (webdav-no-dav-header): OPTIONS /webdav/ returned no DAV header (status=403)
- **BUG02** (webdav-propfind-edge): PROPFIND /webdav/ blocked by edge (status=405) — workaround: hit basement directly
