# Upgrading from v1.x to v2.0

This document covers breaking changes and migration steps when upgrading from basement v1.x to v2.0.0a and later.

## v2.0.0a — Removed: `bucket_user` role

**Date**: 2026-05-24

### Summary

The `bucket_user` role has been completely removed from the policy matrix. This role was deprecated in v1.1.0 (ADR-0002) and assignments became no-ops since bucket-level access is now controlled by the S3 key attached to a UserRegion, not by basement's policy gates.

### What Changed

- The `bucket_user` role is no longer present in `policies.json` on fresh deployments
- All existing RoleAssignments with `role: "bucket_user"` are **silently dropped** from `policies.json` on first v2.0 boot
- A WARN log line reports how many assignments were removed during startup

### Migration Behavior

On first boot of v2.0.0a, the store runs a one-shot migration that:

1. Reads `{dataDir}/policy/policies.json`
2. Filters out all `RoleAssignment` records where `roleId === "bucket_user"`
3. Writes the filtered assignments back atomically
4. Logs: `[WARN] MigrateBucketUserAssignments: dropped N bucket_user assignment(s) per v2.0.0a [[v2_clean_break]]`

**There is no replacement role.** Operators should use one of these patterns instead:

- **Per-bucket grants**: Assign `cluster_admin@bucket:{cid}:{bid}` to users who need specific bucket access
- **Region keychain**: Have Cluster Admins assign S3 keys directly via `/admin/clusters/{cid}/keys` and have users connect regions via `/files/regions/new`

### User Flow Impact

1. A platform admin opens `/admin/policies` → no `bucket_user` option in the role dropdown
2. A platform admin attempts to POST `/admin/policies/assignments` with `role: "bucket_user"` → server returns 400 "unknown role"
3. Existing data with `role: "bucket_user"` in policy_assignments.json → on next basement startup, those rows are dropped + a WARN log line states the count

### Breaking Changes

- **No backward-compat shims**: The enforcer will reject any attempt to assign or reference `bucket_user`
- **No data preservation**: Legacy assignments are permanently removed; there is no rollback path that preserves them
- **API validation**: Creating an assignment with `roleId: "bucket_user"` returns HTTP 400 with error code `ROLE_NOT_FOUND`

### Migration Checklist for Operators

1. Review existing `bucket_user` assignments in your current deployment
2. Identify which users need bucket-level access post-migration
3. Plan replacement assignments using either:
   - `cluster_admin@bucket:{cid}:{bid}` for specific buckets
   - Encourage S3 key assignment via UserRegions (`/files/regions/new`)
4. Upgrade to v2.0.0a — legacy assignments will be silently dropped
5. Verify `/admin/policies` shows only `host_admin` and `cluster_admin` seed roles

### Related Documentation

- [ADR-0001: Three-tier role model](../adr/0001-rbac-three-tier-creds.md)
- [ADR-0002: Region tier user model](../adr/0002-region-tier-user-model.md)
- [`docs/release-notes/v1.1.0.md`](../release-notes/v1.1.0.md) — deprecation announcement

## v2.0.0-beta.2 — Removed: Legacy JWT Encryption, OIDCSubject, SkinPolicy, WebDAV Fields

**Date**: 2026-05-24

### Summary

v2.0.0-beta.2 removes several deprecated fields and legacy encryption paths that were introduced in v1.0.0a through v1.9.0d. This cycle focuses on cleaning up the codebase by dropping backward-compat shims and enforcing modern data shapes.

### What Changed

#### 1. Dropped: `auditResponse.Truncated` field

The `Truncated` boolean field was removed from audit log responses. Audit logs are no longer truncated; full entries are returned.

**Migration**: Frontend code using `truncated` prop should be removed. The field no longer exists in API responses.

#### 2. Dropped: `MigrateFromJWT` and `MigrateFromJWTMap` functions

Legacy JWT-encrypted cluster secrets (from v1.0.0a) are no longer supported. Connections with `ConfigEnc` but no `ConfigEncCSK` are dropped on boot.

**Migration Behavior**: On first boot of v2.0.0-beta.2, the connections store scans for encrypted clusters without CSK parallel encryption and drops them:

```
[WARN] Dropped {N} connection(s) with legacy JWT-encrypted credentials (ConfigEnc but no ConfigEncCSK); re-add via /admin/connections per v2.0.0-beta.2 [[v2_clean_break]]
```

Operators must **re-create** dropped connections via `/admin/connections` or `/admin/clusters/new`. There is no data recovery path.

#### 3. Dropped: `User.OIDCSubject` field

The `oidc_subject` field from v1.0.0a OIDC provisioning has been removed. The canonical `subject` claim now serves both local-password and OIDC users.

**Migration Behavior**: Custom `UnmarshalJSON` on the User struct automatically migrates legacy files: if `oidc_subject` is present and `subject` is empty, it copies the value to `Subject`. No store-level migration code needed.

```
[WARN] Migrated %d user(s): copied OIDCSubject -> Subject per v2.0.0-beta.2 [[v2_clean_break]]
```

#### 4. Dropped: `OrgCapabilities.SkinPolicy` field

The `skin_policy` enum (`"default"` | `"locked"` | `"user-choice"`) has been replaced with granular fields introduced in v1.13.0a.

**Migration Behavior**: On load, if `skinPolicy` is present in the JSON, it's migrated to:
- `UserOverridableSkin = true` + `AllowedUserSkins = []` (all skins) for `"user-choice"`
- `UserOverridableSkin = false` + `AllowedUserSkins = []` (empty list) for `"default"` or `"locked"`

The raw JSON is peeked via `checkRawSkinPolicy()` before unmarshal to detect presence of the legacy field.

#### 5. Dropped: `GatewaySettings.WebDAV` field

The nested `webdav` object under `gateways` has been removed in favor of the generic `Protocols["webdav"]` map introduced in v1.9.0d.

**Migration Behavior**: On load, legacy `{"gateways":{"webdav":{...}}}` is migrated to `Protocols["webdav"]`:
- `enabled` → `Protocols["webdav"].Enabled`
- `baseUrl` → `Protocols["webdav"].BaseURL`

The helper functions `IsEnabled(name)` and `BaseURL(name)` now only consult the Protocols map.

```
[WARN] migrated legacy GatewaySettings.WebDAV to Protocols[webdav]
```

#### 6. Simplified: `GatewaySettings.IsEnabled()` and `BaseURL()`

These methods no longer have a WebDAV fallback branch. They only look up the name in the `Protocols` map. If the key is missing, they return `false` / empty string.

### Breaking Changes

- **Legacy JWT clusters dropped**: Any connection with ConfigEnc but no ConfigEncCSK is permanently removed on boot
- **OIDCSubject field gone**: User JSON without a Subject claim and with oidc_subject will have that value copied to Subject; users without either are local-password users
- **SkinPolicy enum gone**: Only `UserOverridableSkin` + `AllowedUserSkins` are valid in org_capabilities.json
- **WebDAV object gone**: Only `Protocols["webdav"]` is valid in gateways configuration
- **No backward-compat shims**: All legacy field names are silently ignored or migrated on load; they cannot be written

### Migration Checklist for Operators

1. **Backup data directory** before upgrading to v2.0.0-beta.2
2. **Review `/admin/connections`**: Note any clusters that may have been encrypted with legacy JWT (pre-v1.12.0b) — these will need re-creation
3. **Check User records**: Ensure OIDC users have their `subject` claim properly set; the migration handles this automatically on first boot
4. **Verify skin configuration**: If you use custom skins, confirm `UserOverridableSkin` and `AllowedUserSkins` are set correctly in org_capabilities.json
5. **Upgrade to v2.0.0-beta.2** — legacy fields will be migrated or dropped on boot
6. **Re-add any dropped connections** via `/admin/connections` or `/admin/clusters/new`
7. **Verify audit logs**: Confirm no `truncated` field appears in responses (it's been removed)

### Rollback Path

There is **no safe rollback path** from v2.0.0-beta.2 to v1.x if you have legacy JWT-encrypted connections that get dropped. The migration is destructive by design — once a connection is dropped, it must be re-created.

If you need to preserve legacy data:
- Do NOT upgrade to v2.0.0-beta.2 until you've migrated all clusters to CSK encryption (v1.12.0b+)
- Alternatively, keep running v1.x and accept that legacy fields remain in the codebase

### Related Documentation

- [ADR-0007: Cluster secret key rotation](../adr/0007-cluster-secret-key-rotation.md) — ConfigEncCSK introduction
- [`docs/release-notes/v1.9.0d.md`](../release-notes/v1.9.0d.md) — Protocols map for gateway settings
- [`docs/release-notes/v1.12.0b.md`](../release-notes/v1.12.0b.md) — CSK migration helper
- [`docs/release-notes/v1.13.0a.md`](../release-notes/v1.13.0a.md) — Skin policy granular fields
