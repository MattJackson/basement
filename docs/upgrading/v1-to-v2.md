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
