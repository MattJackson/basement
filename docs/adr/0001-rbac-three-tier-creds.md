# ADR-0001: Three-tier role model, three-tier credentials

- **Status**: Accepted
- **Date**: 2026-05-20
- **Decision-maker**: Operator (product manager); senior wrote up
- **Supersedes**: nothing (first ADR)
- **Triggered by**: Operator audit during v0.8.0d.30 — "we have user permissions messy. Editing a cluster is where you add the s3 api but it needs creds. that feels wrong. That feels like user access blurring with cluster admin."

## Context

basement shipped through v0.8.x without a clear separation between
who-is-who. The Edit Cluster form (`/admin/clusters/{cid}/edit`)
currently accepts BOTH:

- `admin_url` + `admin_token` — Garage admin API credentials (cluster-tier)
- `s3_endpoint` + `access_key_id` + `secret_key` — Garage S3 API
  credentials (object-tier)

Both get stored on the Connection record. At request time the driver
uses `admin_token` for cluster-management ops (create bucket, manage
keys, edit layout) AND uses the single `access_key_id`/`secret_key`
pair for EVERY user's S3 ops (list objects, presign download/upload).

This means:

1. Every basement user who browses files signs S3 requests as the
   same identity. Audit logs on the backend see "all activity by GK…",
   not "matthew at 14:32, wife at 14:45."
2. The cluster CONFIG (which feels cluster-tier) holds user-tier
   credentials. Anyone with permission to edit a cluster (Host Admin
   today) implicitly grants themselves user-level access to every
   bucket that key reaches.
3. RBAC is conceptually impossible: per-user/per-bucket grants can't
   be enforced because every user uses the same S3 identity.
4. The Edit Cluster form has 9 fields blurring two distinct concerns,
   making it both visually busy and conceptually muddled.

## Decision

basement adopts an explicit three-tier role model. Each role
operates at one scope, with credentials appropriate to that scope.

### The three roles

| Role | Scope | What they manage | What creds they need |
|---|---|---|---|
| **Host Admin** | basement platform | basement state: users, signup mode, OIDC, enabled drivers, allow-user-clusters toggle | **None** for backends — only their basement login |
| **Cluster Admin** (per cluster) | A specific cluster | Cluster CONFIG: buckets, keys, layout, quotas | `admin_url` + `admin_token` for that cluster's backend admin API |
| **User** (per bucket grant) | A specific bucket | Object content: list/get/put/delete objects, create shares for prefixes they own | Their OWN scoped S3 key for that bucket, minted by Cluster Admin |

### The two-deployment-mode toggle

basement has TWO deployment postures, gated by a Host Admin org
capability flag (`allowUserBackends`, already in store/org_capabilities.go):

- **Company mode** (default, `allowUserBackends=false`):
  - Host Admin curates the cluster list. Users can't add their own.
  - Users see exactly the buckets they've been granted on
    Host-Admin-approved clusters.
  - This is basement.pq.io's posture.

- **Multi-tenant mode** (`allowUserBackends=true`):
  - Users can BYO clusters (really: BYO buckets via S3 keys) using
    only the drivers Host Admin has enabled (`userBackendDrivers`).
  - Each user's BYO additions are scoped to them (not visible to
    other users; not promoted to the org cluster list).

### Mental model: Users add **buckets**, not clusters

A "User adding a cluster" is really "User adding a bucket they have
S3 credentials for." The cluster object in basement DB for a
user-added entry is a thin reference that holds:

- The S3 endpoint URL (which may match an existing org cluster, or
  not)
- The user's access_key_id + secret_key for that one bucket
- NO admin_token (user doesn't have admin access)

UI implication: the user persona's "Add cluster" flow should be
relabeled and reframed as "**Add bucket access**" or "**Connect a
bucket**" with fields for endpoint + access key + bucket alias. The
admin persona's "Add cluster" stays as-is (admin URL + admin token
for the whole cluster).

### Credential storage

| What | Where stored | Encryption |
|---|---|---|
| Host Admin login (matthew) | env `BASEMENT_ADMIN_USER` + `BASEMENT_ADMIN_HASH` (bcrypt) | bcrypt hash, env-time |
| OIDC users | `users.json` (no password column for OIDC accounts) | n/a |
| Local-password users | `users.json` with bcrypt hash | bcrypt hash |
| Cluster admin_token | `connections.json` per Connection | **TODO**: must encrypt at rest before v1.0 (today plaintext on disk) |
| User per-bucket S3 keys | NEW `user_keys.json` table per user × bucket | **TODO**: must encrypt at rest — uses the same secret as JWT signing for now (low-bar acceptable for v0.9; revisit before v1.0) |

### What the Edit Cluster form changes

Today (v0.8.x):
- 9 fields blurring admin + user tiers

After this ADR (v0.9.x):
- **Admin persona's Edit Cluster** (`/admin/clusters/{cid}/edit`):
  Only `label`, `color`, `admin_url`, `admin_token`, `s3_endpoint`
  (for presign destination only — NOT for user S3 ops). NO
  `access_key_id` / `secret_key` fields anymore.
- **User persona's "Add bucket access"** (NEW route — replaces
  `/files/clusters/new`): `bucket_alias`, `s3_endpoint`,
  `access_key_id`, `secret_key`. Stored as a Grant (user × bucket)
  with the user's S3 key, NOT as a new Connection.

### What the runtime does differently

When a User browses files at `/files/{cid}/b/{bid}`:

1. basement looks up the User's grant for `{cid, bid}` (matthew →
   lsi → claude key — or matthew → his-personal-bucket → his BYO key)
2. Mints an S3 client with THAT user's key, not the cluster's key
3. Signs the ListObjects / presign request with it
4. Backend audit log shows the user's key — accountability restored

When a Cluster Admin creates a bucket at `/admin/clusters/{cid}/buckets`:

1. basement uses the cluster's `admin_token` to call
   Garage `/v1/bucket/create`
2. Backend audit log shows the admin operation

When a Host Admin invites a user at `/admin/users/new`:

1. basement creates the user record locally
2. No backend calls happen — Host Admin doesn't touch backends

## Consequences

### Good

- Per-user accountability on backend audit logs
- RBAC at the bucket × user × permission level becomes meaningful
- Form clarity: each Edit page does one thing
- Operator's mental model aligns: "Users manage buckets and below;
  Cluster Admins manage clusters and the things on them; Host
  Admins just manage the website."
- Multi-tenant deployment story (`allowUserBackends=true`) becomes
  coherent: BYO bucket access is a user action, doesn't pollute the
  org cluster list.

### Painful

- Migration: existing Connection records with `access_key_id` +
  `secret_key` in config need to be either (a) split into a new
  Grant record for matthew, or (b) those fields silently ignored
  with a one-time WARN log. **Decision: option (b) for v0.9.x —
  warn-log + ignore, let operator re-grant explicitly. Promote to
  hard fail in v1.0.**
- Existing matthew workflow today: he uses one of his Garage keys
  to browse `lsi`. Post-refactor, he needs to grant himself a
  specific key for that bucket via the new Grant flow. Senior to
  ship a migration helper for the single-cluster, single-user
  deployment (basement.pq.io).
- Per-user encrypted key storage is new infrastructure — must NOT
  go to disk unencrypted. v0.9.x ships with HMAC-protected at-rest
  encryption using the JWT secret as the key (acceptable for
  single-server self-hosted deploys; revisit for multi-tenant SaaS
  in v2.0).

### Open questions (defer to follow-up ADRs)

- Encrypted at-rest for `admin_token` itself — currently plaintext.
  ADR-0002 candidate.
- OIDC-issued, short-lived bucket keys via OIDC group claims —
  ADR-0003 candidate. Out of v0.9 scope.
- Cluster Admin role assignment: today everyone with basement
  login is implicitly Cluster Admin on every cluster. Need a Grant
  table for `user × cluster → cluster-admin role`. Probably v1.0.
- Sync (cross-cluster copy): which user identity signs the source-
  side read vs destination-side write? Likely uses the User's grants
  on BOTH sides — if you don't have read on src AND write on dst,
  the sync rejects. ADR-0004 candidate; covered partially by
  existing memory `feedback_basement_doesnt_invent_permissions`.

## Flexibility: role/permission matrix, not hardcoded roles

Operator clarification (2026-05-20): *"it needs to be flexible in
nature where roles and permissions can change. a matrix somewhere."*

Roles + capabilities are **data**, not code. The three tiers above
(Host Admin / Cluster Admin / User) are **seed presets** the system
ships with — they're rows in the matrix, not enum values in Go.
Operator can add, edit, or remove roles; reassign which capabilities
each role has; reassign which scopes a role covers.

### Capability vocabulary (the columns)

Capability ID is `domain:verb`. Domains map to scopes (next section).
Initial seeded set:

| Domain | Capabilities |
|---|---|
| `host` | `manage_users`, `manage_signup_mode`, `manage_drivers`, `manage_org_caps`, `manage_policies` |
| `cluster` | `create`, `edit`, `delete`, `test`, `view_layout`, `edit_layout` |
| `bucket` | `create`, `edit_alias`, `set_quota`, `delete`, `view` |
| `key` | `create`, `edit_permissions`, `delete`, `view` |
| `objects` | `list`, `get`, `put`, `delete`, `share_create`, `share_revoke` |
| `policy` | `view_matrix`, `edit_matrix`, `assign_role` |

New driver capabilities or new product features add rows here. The
list itself is checked-in code (compiled-in registry — adding a
capability requires a code change because something has to *implement*
it). What's data is **which roles get which capabilities**.

### Scope grammar (the resource axis)

Scopes are URI-style strings the enforcer pattern-matches:

| Scope | Means |
|---|---|
| `host:*` | basement platform — singular |
| `cluster:*` | every cluster |
| `cluster:{cid}` | one specific cluster |
| `bucket:{cid}:*` | every bucket on a cluster |
| `bucket:{cid}:{bid}` | one specific bucket |
| `key:{cid}:*` | every key on a cluster |
| `key:{cid}:{kid}` | one specific key |

Wildcards (`*`) are explicit, not implicit — a role with
`cluster:716e…` does NOT auto-cover its buckets unless the role also
has `bucket:716e…:*`.

### Role definition shape

A Role is `(id, label, capabilities[], description)`:

```json
{
  "id": "cluster_admin",
  "label": "Cluster Admin",
  "description": "Manages buckets, keys, layout on a cluster they're assigned to.",
  "capabilities": [
    "cluster:edit", "cluster:test", "cluster:view_layout", "cluster:edit_layout",
    "bucket:*", "key:*", "objects:list"
  ]
}
```

The capabilities array can use `domain:*` shorthand for "every verb
in that domain." `*:*` is reserved for full superuser (only the
built-in `host_admin` seed gets it; operator can revoke).

### Assignment shape

A RoleAssignment is `(userId, roleId, scope)`:

```json
{ "userId": "matthew", "roleId": "host_admin",    "scope": "host:*" }
{ "userId": "matthew", "roleId": "cluster_admin", "scope": "cluster:716e..." }
{ "userId": "matthew", "roleId": "bucket_user",   "scope": "bucket:716e...:lsi" }
{ "userId": "wife",    "roleId": "bucket_user",   "scope": "bucket:716e...:family-photos" }
{ "userId": "father",  "roleId": "cluster_admin", "scope": "cluster:9a8b..." }
```

A user with no assignments has zero capabilities — secure default.

### Enforcer interface

```go
// internal/auth/policy/enforcer.go
type Enforcer interface {
    Can(userID, capability, scope string) bool
    Capabilities(userID, scope string) []string  // for UI gating
    AssignmentsFor(userID string) []RoleAssignment
}
```

UI gates render based on `Capabilities(user, scope)` — the
delete-bucket button doesn't show unless `bucket:delete` is in the
returned list for that bucket's scope. **No driver-name checks, no
role-name checks in the UI** — only capability checks. Aligns with
[[feedback_generic_driver_middleman]].

### Storage

- `policies.json` (NEW) — single file, atomic write:
  ```json
  {
    "roles":      [Role, Role, ...],
    "assignments":[RoleAssignment, RoleAssignment, ...]
  }
  ```
- Encrypted at rest (same scheme as admin_token — JWT secret as key
  for v0.9; revisit for v1.0).
- Capability registry (`internal/auth/policy/capabilities.go`) lives
  in code — the set of valid `capability` IDs the enforcer accepts.
  Roles referencing an unknown capability fail validation at write
  time + warn at read time.

### Built-in seed (immutable rows)

Three roles are seeded on first boot, with the `seed: true` flag.
They CAN be edited (capabilities added/removed) but CANNOT be
deleted entirely — that prevents accidental lockout. Operator can
clone a seed to make a custom role.

| ID | Default capabilities |
|---|---|
| `host_admin` | `*:*` (everything) |
| `cluster_admin` | `cluster:edit|test|view_layout|edit_layout`, `bucket:*`, `key:*`, `objects:list` |
| `bucket_user` | `objects:list|get|put|share_create|share_revoke`, `bucket:view` |

### UI surface

- **`/admin/policies`** (NEW route, requires `policy:view_matrix`):
  Three-pane editor.
  - Left: list of Roles (click to edit capabilities)
  - Middle: matrix view (roles × capability domains, checkboxes)
  - Right: list of Assignments (filter by user / role / scope)
- **`/admin/users/{username}`** (extend existing): show all
  assignments for that user, allow Host Admin to add/remove.
- **Capability badges** on relevant UI elements: greyed/hidden if
  the current user lacks the capability for the visible scope.

### Why a matrix and not RBAC libraries (Casbin etc.)

- basement is single-server / small-org scoped; a 200-line
  matrix + enforcer is sufficient and auditable
- No external dep, no policy language to learn
- Same JSON file the operator backs up everything else with
- v2.0 can swap in Casbin if multi-tenant SaaS demands it; the
  Enforcer interface is the seam.

## Implementation plan

Senior writes the ADR (this file). Then in order:

1. **v0.9.0b** — `internal/auth/policy/`: capability registry, Role
   + RoleAssignment types, Enforcer interface + file-backed impl,
   `policies.json` seed-on-empty. NO consumer code yet — just the
   subsystem + tests.
2. **v0.9.0c** — Add `Grant` schema for `(userId, connectionId,
   bucketId, accessKeyId, secretKey-encrypted)` to
   `internal/store/`. Grants store the CREDS; assignments store
   the POLICY. They're separate concerns linked by `(userId,
   bucketScope)`.
3. **v0.9.0d** — Refactor Edit Cluster page: remove
   `access_key_id` and `secret_key` fields; keep `admin_url` +
   `admin_token` + `s3_endpoint`. Backend driver returns clear
   error if user-tier S3 op attempted without a Grant.
4. **v0.9.0e** — Rewrite `/files/clusters/new` as "Add bucket
   access" — fields: alias, s3_endpoint, access_key_id, secret_key.
   Creates a Grant + a `bucket_user` RoleAssignment for the
   current user. Reuses an existing Connection if `s3_endpoint`
   matches, else creates a user-scoped Connection.
5. **v0.9.0f** — Runtime: user-facing endpoints
   (`/user/clusters/{cid}/buckets/{bid}/...`) call
   `enforcer.Can(user, "objects:list", "bucket:cid:bid")` before
   acting, then look up the User's Grant for that bucket, build a
   per-request S3 client with the Grant's key. Admin-facing
   endpoints similarly gate on `cluster:edit` / `bucket:create`
   etc. Replace all hardcoded `if isUIAdmin {}` checks with
   capability checks.
6. **v0.9.0g** — `/admin/policies` UI: matrix editor for
   capabilities × roles, assignment list with add/remove. Host
   Admin only (gated by `policy:edit_matrix`).
7. **v0.9.0h** — Migration helper: on first boot of v0.9.0g, detect
   existing Connection with `access_key_id` + `secret_key` in
   config; offer one-time "Grant these creds to matthew on bucket
   X with bucket_user role?" prompt in the UI on Host Admin login.

Each step is a separate freshman cycle dispatched against the
relevant prompt.

## Relation to existing memory

- `[[role_model_three_axes]]` — this ADR formalizes that memory's
  three axes into a binding contract.
- `[[feedback_basement_doesnt_invent_permissions]]` — basement
  surfaces what the backend enforces; never invents. This ADR adds
  "and never blurs whose creds are doing which call."
- `[[persona_split_user_vs_admin]]` — the `/` vs `/admin` route
  split is the surface manifestation of this ADR.
- `[[v05_scope]]` — predates this ADR; some assumptions there
  about "all users see all clusters" supersede here.

## Resolution (v2.0)

The bucket_user role was completely removed in v2.0.0a per [[v2_clean_break]].
No backward-compat shims were added; legacy assignments are dropped silently at boot.
Bucket-level access now flows exclusively through:

1. Region keychain S3 keys (UserRegions) — primary path for user-tier access
2. cluster_admin role with bucket-scoped assignments — for operators who need
   explicit policy-based grants

This completes the architectural shift from basement-enforced per-bucket grants to
backend-enforced access via S3 credentials.

## Tags

`rbac`, `architecture`, `breaking-change`, `v0.9`, `creds-hygiene`

