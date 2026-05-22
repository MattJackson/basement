# Changelog

All notable changes to basement are recorded here. See the linked
release-notes files in `docs/release-notes/` for the full per-release
write-up; this file is the at-a-glance index.

## v1.1.0 — 2026-05-21

Region tier replaces phantom Connections at the user persona
(ADR-0002). Per-user keychain at `/files/keys`; one credential per
endpoint; backend authoritative for bucket visibility. `bucket_user`
role deprecated. Sync + share become region-aware. Legacy
`/api/v1/user/clusters/*` + `/user/buckets/connect` + `/user/keys`
endpoints removed.

Full notes: [`docs/release-notes/v1.1.0.md`](docs/release-notes/v1.1.0.md)

### Cycles

- **v1.1.0a** — UserRegion store + AES-GCM secret encryption + uniqueness on (userId, endpoint)
- **v1.1.0b** — Region API endpoints `/api/v1/user/regions/*` (CRUD + bucket + object ops)
- **v1.1.0c** — Frontend rewrite of `/files` persona: `/files/regions/new`, `/files/{regionId}`, `/files/{regionId}/b/{bid}`
- **v1.1.0c.1–c.4** — Garage ListBuckets S3-fallback fixes + smoke-test alignment
- **v1.1.0d** — Garage admin bridge + `bucket_grants.json → user_regions.json` migration + region-keychain UI
- **v1.1.0e** — Retire legacy cluster-tier user endpoints + bucket-grants cleanup
- **v1.1.0f** — Deprecate `bucket_user` role in policy matrix
- **v1.1.0g** — Region-to-connection resolver for sync + share engines + `region:list_buckets` audit hook
- **v1.1.0h** — Object-tier audit hooks (`region:list_objects`, `region:presign_get/put`, `region:multipart_*`, `region:delete_object`) + release notes + README + CHANGELOG

## v1.0.0 — 2026-05-21

The production-ready milestone. Three-tier role model (Host Admin /
Cluster Admin / User), flexible policy matrix at `/admin/policies`,
per-user encrypted S3 credentials, audit log subsystem, metrics
persistence + time-series chart on `/admin/usage`.

Full notes: [`docs/release-notes/v1.0.0.md`](docs/release-notes/v1.0.0.md)

### Cycles

- **v1.0.0a** — encrypt admin_token + secret_key in `connections.json` at rest
- **v1.0.0b** — retire legacy `internal/store/grants.go` in favour of BucketGrants
- **v1.0.0c** — audit log subsystem + `/admin/audit` viewer
- **v1.0.0d** — metrics persistence + time-series chart on `/admin/usage`

## Pre-v1.0

See git tag history (`v0.4.0` through `v0.9.0m.1`) for the lead-up
to the v1.0 milestone — multi-cluster admin (v0.5.0), four drivers
+ OIDC (v0.5.x), end-user shell + sharing (v0.6.x–v0.7.x),
cross-backend sync (v0.8.x), operator polish + ADR-0001 RBAC
(v0.9.x).
