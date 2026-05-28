# ADR-0009: Capability-Based RBAC (Operations Matrix)

Status: Accepted (operator-locked 2026-05-28)
Supersedes nothing; refines [ADR-0001](0001-rbac-three-tier-creds.md).

## Context

The three-axis role model (UI Admin / Cluster Admin / User) from
ADR-0001 has been enforced through scattered conditionals:

- `frontend/src/shared/auth/ProtectedRoute.tsx` — pattern-matches
  `location.pathname` against `activeRole.kind` (~100 lines of
  if/else). Every new route requires touching this file.
- Components — `{activeRole?.kind === "ui-admin" && <Link>}`
  duplicated across the codebase. 22+ sites.
- Backend — `auth.ActiveRoleClusterMiddlewareFromPath()` at
  `internal/auth/active_role.go:58` explicitly allows UI Admin as
  "super-admin" on cluster routes (line 68-72). 22+ enforcement
  sites between RequireRole, ActiveRoleMiddleware variants, and
  inline `RequireActiveRole(...)` calls.

This produced a recurring class of bugs (beta.6, beta.30, beta.36
items 1c/3) where:

- A role can see a link to a route it can't access (FE leak)
- A role can hit an API endpoint it shouldn't (BE drift)
- "UI Admin can edit cluster contents" was assumed; operator confirms
  this is WRONG (2026-05-28)

The fix is structural: replace scattered role-pattern checks with a
single capability table read by both frontend and backend.

## Decision

### 1. The wiring-vs-contents split (locked)

| Layer | Belongs to | Examples |
|---|---|---|
| **Cluster connection** | UI Admin | admin_url, admin_token, driver, label, color, region defaults |
| **Cluster contents** | Cluster Admin | buckets, keys, encryption admins, lifecycle rules, grants |
| **Platform** | UI Admin | users, policies, audit, system settings, skins, OIDC, onboarding, service accounts |
| **Cross-cluster aggregates** | UI Admin | `/admin/buckets` (all clusters), `/admin/usage` |
| **Own resources** | User | files, keys, shares, backups, federations, webhooks |

UI Admin is **not** a super-admin. They do not see cluster contents.
If one person needs to do both, they hold both roles and switch
via the persona pill.

### 2. Operations matrix

Each operation in basement has a single capability key. Roles map to
capability sets. Both FE and BE read the same table.

#### Platform operations (UI Admin)

| Capability | Description |
|---|---|
| `platform.users.list` | List basement login accounts |
| `platform.users.create` | Create a new login account |
| `platform.users.update` | Update an account's metadata |
| `platform.users.delete` | Delete an account |
| `platform.policies.list` | List platform-wide policies |
| `platform.policies.write` | Create/edit/delete policies |
| `platform.service-accounts.list` | List service accounts |
| `platform.service-accounts.write` | Create/rotate/revoke service accounts |
| `platform.audit.read` | Read audit log |
| `platform.skins.write` | Create/edit/delete skins |
| `platform.oidc.write` | Configure OIDC providers |
| `platform.system.write` | Edit system settings |
| `platform.onboarding.write` | Run onboarding flows |

#### Cluster wiring (UI Admin)

| Capability | Description |
|---|---|
| `cluster.wiring.list` | List all clusters |
| `cluster.wiring.create` | Register a new cluster in basement (POST /admin/clusters) |
| `cluster.wiring.update` | Edit cluster connection config (PATCH /admin/clusters/{cid}) |
| `cluster.wiring.delete` | Deregister a cluster from basement |
| `cluster.wiring.test` | Run health/test probe against a cluster's wiring |
| `cluster.buckets.aggregate` | Read cross-cluster bucket aggregate (`/admin/buckets`) |
| `cluster.usage.aggregate` | Read cross-cluster usage stats |

#### Cluster contents (Cluster Admin, scoped to their cluster)

| Capability | Description |
|---|---|
| `cluster.contents.read` | Read cluster detail page (buckets list, keys list, admin list) |
| `cluster.buckets.create` | Create a bucket on this cluster |
| `cluster.buckets.update` | Edit a bucket's config |
| `cluster.buckets.delete` | Delete a bucket |
| `cluster.buckets.lifecycle.write` | Edit lifecycle rules on a bucket |
| `cluster.keys.create` | Create an access key on this cluster |
| `cluster.keys.rotate` | Rotate an access key |
| `cluster.keys.delete` | Delete an access key |
| `cluster.encryption-admins.list` | List encryption (CSK) admins for this cluster |
| `cluster.encryption-admins.add` | Add an encryption admin |
| `cluster.encryption-admins.remove` | Remove an encryption admin |
| `cluster.encryption.unlock` | Unlock the cluster's encryption with the admin password |
| `cluster.encryption.lock` | Lock the cluster |
| `cluster.grants.list` | List grants tying users to this cluster's regions |
| `cluster.grants.write` | Create/edit/delete grants |

#### User operations (User)

| Capability | Description |
|---|---|
| `self.files.read` | List/download own files in own buckets |
| `self.files.write` | Upload/delete own files |
| `self.keys.list` | List own S3 keys |
| `self.keys.create` | Create an own S3 key |
| `self.keys.rotate` | Rotate an own S3 key |
| `self.keys.delete` | Delete an own S3 key |
| `self.shares.write` | Create/edit/revoke own shares |
| `self.backups.write` | Configure own backups |
| `self.federations.write` | Configure own federation endpoints |
| `self.webhooks.write` | Create/edit/delete own webhooks |

### 3. Role → capability mapping

```
ui-admin:
  - All platform.* capabilities
  - All cluster.wiring.* capabilities
  - cluster.usage.aggregate, cluster.buckets.aggregate
  - NOT cluster.contents.*, cluster.buckets.*, cluster.keys.*,
    cluster.encryption-admins.*, cluster.grants.* (those are
    cluster-admin's job)

cluster-admin (scoped to their assigned cluster):
  - All cluster.contents.*, cluster.buckets.*, cluster.keys.*,
    cluster.encryption-admins.*, cluster.encryption.*,
    cluster.grants.* capabilities (scoped to ar.Cluster only)
  - NOT cluster.wiring.* (they don't manage the connection)
  - NOT platform.* (they don't manage basement-level state)

user:
  - All self.* capabilities (scoped to the active region tier)
```

### 4. Implementation

#### Backend

- `internal/auth/capabilities.go` (new): defines the capability
  enum + role-to-capability map + `Can(claims, capability) bool` +
  `RequireCapability(cap) Middleware`.
- `internal/api/server.go`: replace scattered `RequireRole(...)`
  and `ActiveRoleXxxMiddleware()` with `RequireCapability(cap)`
  at route mount time.
- The "UI Admin is super-admin" branch at
  `internal/auth/active_role.go:68-72` is REMOVED. UI Admin gets
  `cluster.wiring.*` but NOT `cluster.contents.*`.
- `/api/v1/auth/me` response gains a `capabilities: string[]` field
  computed from the user's active role.

#### Frontend

- `frontend/src/shared/auth/capabilities.ts` (new): mirrors the
  Go capability strings. Single source of truth for the FE.
- `useCan(cap: string): boolean` hook reads `useUser().data?.capabilities`.
- `<Can capability="..."><Link>...</Link></Can>` component for
  conditional rendering — replaces `{activeRole?.kind === ... && ...}`.
- `frontend/src/shared/auth/route-capabilities.ts` maps each route
  pattern to the capability it requires. `ProtectedRoute` reads
  from this map instead of the current pathname-switch.

#### Migration

- Phase A: ADR + matrix (this document)
- Phase B: Backend `capabilities.go` + `/auth/me` response
- Phase C: Backend middleware migration
- Phase D: Frontend `useCan` + `<Can>` + `ProtectedRoute` rewrite
- Phase E: Sweep — replace all `activeRole?.kind === ...` sites
  with `useCan(...)`

Each phase is a separate beta cycle. Tests at every layer.

## Consequences

### Positive

- Adding a route requires ONE entry in `route-capabilities.ts`
  (FE) and ONE `RequireCapability(...)` on the mount (BE). No
  scattered conditionals.
- "What can X do" has a single answer (read the table).
- FE/BE drift becomes impossible (both read the same table).
- The "UI Admin can edit cluster contents" misconception is
  structurally prevented.

### Negative

- One-time migration cost — 22+ FE sites + 22+ BE sites to sweep.
- Backwards compat: cluster admins who used to be able to be
  "rescued" by a UI Admin will no longer be. Operator confirms
  this is the right call (UI Admin can hold a cluster-admin role
  for the same cluster if rescue is needed).
- `ar.Kind` checks remain valid for surface-switching (which shell
  renders), but never for permission enforcement.

### Out of scope

- Custom roles or per-user capability overrides (Phase 2 if ever
  needed)
- Time-bounded elevations (the existing sudo mode stays orthogonal)
- Multi-tenancy beyond cluster scoping
