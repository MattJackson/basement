# ADR-0002: Region tier replaces Cluster at the user persona

- **Status**: Accepted
- **Date**: 2026-05-21
- **Decision-maker**: Operator (product manager); senior wrote up
- **Triggered by**: Operator session on v1.0.0d trying to use Connect-a-bucket — *"users -> should see regions not clusters. they connect to a region. cluster admins in the admin section should see clusters. is that right model?"* and *"user adds a region, in that region are buckets. cluster admin creates a key and assigns that key to buckets."*
- **Builds on**: [ADR-0001](./0001-rbac-three-tier-creds.md)

## Context

ADR-0001 established three roles (Host Admin / Cluster Admin / User) and per-user encrypted credentials. The implementation correctly separated admin-tier creds (admin_token) from user-tier creds (per-user S3 key). But it kept ONE primitive on the user persona: **the User still talks about "clusters."**

The user-side data model:
- `Connection` record per (s3_endpoint, owner) pair
- BucketGrant per (userId, connectionId, bucketId, accessKey, secret)
- `/files` lists "My Clusters" — including any BYO Connection the user created via Connect-a-bucket

Result: when a user connects a bucket via `/files/buckets/connect`, basement creates a brand-new Connection record. The user sees TWO clusters at `/files` — the org-curated one AND their own BYO phantom — even though the BYO is just "I have an S3 key for buckets at some endpoint."

This conflates two concepts the User shouldn't care about:
1. **Cluster** — a backend instance with admin authority. User doesn't manage this.
2. **Region** — an S3 endpoint where buckets live. User connects to this.

A single Cluster can serve multiple Regions (multi-AZ AWS); multiple Clusters can serve the same Region (HA Garage). The User cares about Regions, period.

## Decision

basement adopts a **region-tier abstraction at the user persona**. The Cluster concept survives unchanged at the admin tier.

### Vocabulary

- **Region**: an S3 endpoint URL plus a label. Identified by the canonical endpoint (e.g. `https://s3.pq.io` → slug `s3.pq.io`).
- **UserRegion**: a User's keychain entry — `(userId, regionEndpoint, alias, accessKeyId, secretKey_encrypted)`. The User has ONE per (user, endpoint) pair.
- **Cluster** (admin tier, unchanged): a specific backend instance with admin_url + admin_token.

A Cluster exposes one or more Regions (each via its s3_endpoint). A Region can be served by zero, one, or many Clusters. basement tracks Clusters at the admin layer; Regions are a USER-FACING view derived from Connections + user-supplied endpoints.

### The flow per ADR-0002

**Cluster Admin** (matthew on `classe`):
1. Owns the cluster (admin_token + admin_url)
2. Creates buckets via `/admin/clusters/{cid}/buckets`
3. Creates S3 keys via `/admin/clusters/{cid}/keys`
4. Assigns keys to buckets with R/W/O permissions
5. Hands `(s3_endpoint, access_key, secret)` to the user out-of-band (Slack/email)

**User** (wife, say):
1. Receives credentials from Cluster Admin
2. Goes to `/files/regions/new` (renamed from `/files/buckets/connect`)
3. Form: alias ("home"), endpoint URL, access_key_id, secret_key, region name (optional, e.g. "garage")
4. basement stores a UserRegion record
5. `/files` lists her UserRegions, one row per
6. Tap a region → basement signs `ListBuckets` with her key → backend returns whatever buckets the key can see
7. Tap a bucket → object browser, all requests signed with the same key

**Host Admin** (also matthew):
- Sees both `/admin/*` AND `/files` (persona switcher)
- In `/files`, sees ONLY their own UserRegions — not other users'
- Their existing legacy admin shortcut at `/files/{cid}` for browsing every bucket on every cluster GOES AWAY — admins use `/admin/clusters/{cid}` for that, with admin_token

### What disappears from the user persona

- The concept of "a user has a cluster" (was wrong from the start)
- Per-bucket BucketGrant records — the user has ONE region credential, the backend enforces per-bucket access via the S3 key's bucket grants
- The matrix's `bucket_user` role at scope `bucket:{cid}:{bid}` — replaced by "user has a UserRegion that lets them sign requests"
- The phantom Connection rows created by Connect-a-bucket

### What stays unchanged

- Cluster Admin's tools (`/admin/clusters/*`)
- Policy matrix at `/admin/policies` (now ONLY Host Admin + Cluster Admin roles)
- Per-user audit log attribution (the user's accessKeyId still signs each request)
- All four drivers + their capability flags
- Sync engine (sync still operates cluster-to-cluster; cross-region sync requires both sides' admin authority)

### Data model

```go
// internal/store/user_regions.go (NEW)
type UserRegion struct {
    ID           string    `json:"id"`            // UUID
    UserID       string    `json:"userId"`
    Alias        string    `json:"alias"`         // user-chosen, e.g. "home", "work"
    Endpoint     string    `json:"endpoint"`      // canonical URL
    Region       string    `json:"region"`        // S3 region label, e.g. "garage", "us-east-1"
    AccessKeyID  string    `json:"accessKeyId"`
    SecretKeyEnc []byte    `json:"secretKeyEnc"`  // AES-GCM, JWT secret key
    CreatedAt    time.Time `json:"createdAt"`
    LastUsedAt   time.Time `json:"lastUsedAt,omitempty"`  // bumped on each successful sign
}

// Uniqueness: one UserRegion per (userId, endpoint) — same user can't add the same endpoint twice with different aliases
```

The existing `BucketGrant` table is **deprecated**. Migration: convert existing BucketGrants by deduping on `(userId, endpoint)` and merging the access key from any one of them (they should all be the same per user per endpoint in practice). Drop the table after migration.

### API

**Replaces**:
- `POST /api/v1/user/buckets/connect` → `POST /api/v1/user/regions` (body: alias, endpoint, accessKeyId, secretKey, region)
- `GET /api/v1/user/clusters` → `GET /api/v1/user/regions` (returns the user's UserRegions, NOT Connections)
- `GET /api/v1/user/clusters/{cid}/buckets` → `GET /api/v1/user/regions/{regionId}/buckets` (signs ListBuckets with the UserRegion's key)
- `GET /api/v1/user/clusters/{cid}/buckets/{bid}/objects` → `GET /api/v1/user/regions/{regionId}/buckets/{bid}/objects`
- All object ops (`GET/PUT/DELETE`, `presign-get/put`) move under `/api/v1/user/regions/{regionId}/buckets/{bid}/...`

**Stays**:
- All `/api/v1/admin/*` endpoints unchanged
- Public share endpoints (`/api/v1/share/*`) unchanged — shares don't need the user's region

### URL scheme

| Old (cluster-tier) | New (region-tier) |
|---|---|
| `/files/clusters/new` (deleted v0.9.0e) | n/a (was already gone) |
| `/files/buckets/connect` | `/files/regions/new` |
| `/files/{cid}` | `/files/{regionId}` |
| `/files/{cid}/b/{bid}` | `/files/{regionId}/b/{bid}` |
| `/files/keys` | `/files/keys` (unchanged — shows access keys across all user regions) |
| `/files/shares` | `/files/shares` (unchanged) |
| `/files/syncs` | `/files/syncs` (unchanged — admin feature) |

### Object browser bug discovered during ADR investigation

While testing the current Connect-a-bucket flow during operator-led setup, found that v0.9.0e's freshman shipped a wrong assumption: "for Garage, alias IS the bucket ID for ListBuckets purposes" is true for `ListBuckets` but **false for `ListObjects` / `GetObject` / etc.** Garage's S3 API routes those by alias-as-host OR full bucketID, depending on configuration. The current BucketGrant.BucketID column stores the alias, which the driver then plugs into S3 URL paths — 404 from Garage on every object op.

ADR-0002's implementation also fixes this: UserRegion stores no bucket IDs at all. ListBuckets returns the live list from the backend (signed with the user's key) — alias-vs-ID mapping happens inside the driver, transparent to basement.

## Consequences

### Good

- **Operator's confusion gone**: User sees Regions (their keychain), Cluster Admin sees Clusters (their backends). One concept per persona.
- **Backend is authoritative for bucket visibility** — basement no longer maintains a "which buckets does user X see" cache; it queries with the user's key.
- **Per-bucket grant explosion gone**: a User who has access to 50 buckets in one region needs ONE UserRegion row, not 50 BucketGrant rows.
- **Cluster Admin's workflow stays the same**: create key, assign to buckets in the admin UI. The "set up a user" handoff is: hand them the key + endpoint. Out-of-band, just like AWS IAM today.
- **The 404 alias-vs-ID bug in ListObjects automatically fixed** because the driver no longer relies on basement telling it bucketIDs.

### Painful

- **Existing BucketGrants** need migration to UserRegions. Dedup by (userId, endpoint) — typically idempotent because users only had one key per endpoint anyway.
- **API rename** is a breaking change. v1.1.0 release notes should call it out clearly. Operators upgrading will see a transient period where old `/user/clusters` URLs 404 — frontend ships with new URLs in the same release.
- **Cluster admin shortcut for browsing every bucket** (matthew's "I'm host admin, I see everything") goes away from `/files`. That power moves to `/admin/clusters/{cid}` where the admin uses the admin_token. Less convenient for the dogfood case but conceptually cleaner.
- **Public share routes** continue to embed the source ConnectionID — they need lookup translation post-migration. Not a UX-visible break; just internal plumbing.

### Open questions

- Should the User see SHARED Regions (Host Admin curates regions in advance, "click here to use", auto-creates a UserRegion from a pre-distributed key)? Could be a v1.2 feature. Doesn't block this ADR.
- How does the Region label render when two UserRegions hit the same endpoint but were aliased differently? Show both aliases? Pick one? Operator decision; default is "show the user's alias, fall through to endpoint hostname if alias is blank."

## Implementation plan

Eight steps. Each a separate freshman cycle. Senior writes the ADR (this file).

1. **v1.1.0a** — `internal/store/user_regions.go`: UserRegion type + UserRegions interface + file-backed store + AES-GCM secret encryption (reuse `crypto.go`) + uniqueness on (userId, endpoint) + tests
2. **v1.1.0b** — Backend API `/api/v1/user/regions/*` (CRUD + ListBuckets + ListObjects + GetObject + presign-get/put + multipart). All signed with the UserRegion's key. Capability gates updated.
3. **v1.1.0c** — Frontend rewrite of user persona: `/files/regions/new`, `/files/{regionId}`, `/files/{regionId}/b/{bid}`. Delete the old `/files/buckets/connect` + `/files/$cid` routes.
4. **v1.1.0d** — Migration: scan `bucket_grants.json`, dedupe by (userId, endpoint), write `user_regions.json`. Print summary to slog. Idempotent. Delete `bucket_grants.json` after green migration.
5. **v1.1.0e** — Strip the admin-shortcut path from `/files/{...}` handlers. Admin browses via `/admin/clusters/{cid}` (already exists). Update Host Admin's persona switcher copy.
6. **v1.1.0f** — Update `/admin/policies` matrix: deprecate `bucket_user` role (still exists for back-compat, returns no-op effect). Add a banner: "Bucket-level access is now controlled by S3 key assignment on the cluster admin side, not by basement roles."
7. **v1.1.0g** — Audit log: every user-side ListBuckets / ListObjects / presign now attributes via the UserRegion's accessKeyId (already wired in v1.0.0c, just verify it still hits)
8. **v1.1.0h** — Release notes for v1.1.0 + README + screenshots refresh + tag v1.1.0

## Relation to existing memory

- `[[role_model_three_axes]]` — Refines ADR-0001's three axes. User axis is now Region-bound, not Cluster-bound.
- `[[feedback_basement_doesnt_invent_permissions]]` — ADR-0002 fully realizes this doctrine. basement stops tracking per-bucket grants and asks the backend.
- `[[feedback_popups_max_2_fields]]` — `/files/regions/new` has 5 fields; remains a route, not a dialog. No change.
- `[[persona_split_user_vs_admin]]` — Sharpened by ADR-0002. Admin persona owns Clusters; User persona owns Regions; the URL split (`/files` vs `/admin/*`) cleanly mirrors data ownership.
- `[[feedback_driver_parity]]` — No driver interface change required by ADR-0002 (Regions are a store-layer abstraction, drivers don't see them).

## Refinement: v1.2.0d (key-first model)

- **Date**: 2026-05-21
- **Triggered by**: Operator product decision after multi-tenant
  dogfooding — "each ACCESS KEY is the primary user noun, not the
  region/endpoint."

A user may now register multiple `UserRegion` rows against the same
endpoint with different aliases ("Work S3", "Personal S3"). Each card
on `/files` is one of these keys.

### What changes

- **Uniqueness key** moves from `(userId, endpoint)` to
  `(userId, endpoint, alias)`. Same alias still 409s
  `DUPLICATE_REGION`.
- **`/files` heading** stays "My Regions" but the subtitle reframes:
  "Each card is one of your access keys — click to browse the buckets
  it can see."
- **`/files/keys`** becomes "My Keys" (per-key admin view: copy
  access-key-ID, last-used, delete one key without touching siblings
  at the same endpoint).
- **`/files/keys/new`** is the canonical "Add a key" form;
  `/files/regions/new` keeps working as a redirect alias for in-flight
  bookmarks.
- **`GetByUserEndpoint`** picks the FIRST match by insertion order
  when multiple keys share an endpoint. Sufficient for the sync
  resolver because every key at one endpoint bridges to the same
  admin `Connection`; the resolver logs a debug note when ambiguity
  exists so an operator inspecting share/sync traffic can see which
  alias was implicitly bridged.
- **Frontend hook rename**: `useCreateUserRegion` →
  `useCreateUserKey` (the user-facing noun shifts to "key").
  `useUserRegions` kept — it still returns the storage type.
- **Storage type stays `UserRegion`** server-side. Renaming the file
  + Go type would churn every consumer for zero behavioural benefit
  — the (key + endpoint) tuple is still what's persisted.

### Migration

None. Existing rows already satisfy the new `(userId, endpoint,
alias)` uniqueness (the v1.1.x constraint was strictly stronger).
On-disk JSON shape unchanged.

## Tags

`rbac`, `architecture`, `breaking-change`, `v1.1`, `v1.2`,
`region-tier`, `key-first`, `supersedes-byo-cluster`
