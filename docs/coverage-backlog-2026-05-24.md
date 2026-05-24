# Coverage Backlog — 2026-05-24

Generated from analysis of `docs/coverage-gaps-2026-05-24.md` and `docs/frontend-coverage-2026-05-24.md`.

## Overview

**Go Coverage:** 62.8% overall (17 packages below 80%)
**Frontend Coverage:** Test suite has 41 failing tests blocking coverage data collection

---

## Go Packages Requiring Test Coverage

### HIGH PRIORITY — Critical API Paths (<50% coverage)

#### `internal/api/admin_service_accounts.go` (~38-75% coverage)
**Need ~6-8 tests for:**
- [ ] `validateServiceAccountScope` (38.9%) - scope validation edge cases
- [ ] `createServiceAccountHandler` (69.4%) - full CRUD flow integration
- [ ] `updateServiceAccountHandler` (45.1%) - update mutations + RBAC checks
- [ ] `deleteServiceAccountHandler` (42.3%) - cascade delete behavior
- [ ] `rotateServiceAccountHandler` (44.4%) - rotation lifecycle + secret handling

#### `internal/api/admin_users.go` (0% coverage on key handlers)
**Need ~5 tests for:**
- [ ] `listAllUsersHandler` (0%) - pagination + filtering
- [ ] `createUserHandler` (0%) - user creation + invite flow
- [ ] `deleteUserHandler` (0%) - deletion cascade + audit logging

#### `internal/api/admin_buckets.go` (0% on create/update/delete)
**Need ~5 tests for:**
- [ ] `listBucketsHandler` (0%) - list with filters
- [ ] `createBucketHandler` (0%) - bucket creation validation
- [ ] `updateBucketHandler` (0%) - metadata updates
- [ ] `deleteBucketHandler` (0%) - cascade cleanup

#### `internal/api/backup_runner.go` (multiple 0% paths)
**Need ~8 tests for:**
- [ ] `pruneSnapshots` (0%) - GFS retention enforcement
- [ ] `deleteAllUnderPrefix` (0%) - bulk deletion safety checks
- [ ] `Load/Save/List/Delete` interfaces (0%) - store adapter tests
- [ ] `resolveBackupConn` (30%) - connection resolution edge cases

#### `internal/api/user_syncs.go` (mostly 0% coverage)
**Need ~6 tests for:**
- [ ] `userListSyncsHandler` (0%) - list with filters
- [ ] `userGetSyncHandler` (0%) - detail retrieval
- [ ] `userDeleteSyncHandler` (0%) - sync deletion
- [ ] `userPauseSyncHandler` (0%) - pause/resume state machine
- [ ] `userResumeSyncHandler` (0%) - resume validation

### MEDIUM PRIORITY — Driver Adapters (<50% coverage)

#### `internal/drivers/garage/` (~47% overall, many 0% methods)
**Need ~15 tests for:**
- [ ] Cluster operations: `HealthCheck`, `ListNodes`, `GetLayout` (all 0%)
- [ ] S3 object ops: `StreamObject`, `PutObjectStream` (0%)
- [ ] Lifecycle: `PerBucketStatsAvailable`, `GetLifecycle`, `PutLifecycle` (all 0%)
- [ ] Versioning: all 7 versioning methods at 0%
- [ ] Object Lock: all 6 object lock methods at 0%
- [ ] Encryption: all 4 encryption methods at 0%

#### `internal/drivers/minio/` (~43% overall)
**Need ~12 tests for:**
- [ ] Versioning methods (7 at 0%)
- [ ] Lifecycle methods (3 at 0%)
- [ ] Object Lock methods (6 at 0%)
- [ ] Encryption methods (5 at 0%, some util funcs at 0%)

#### `internal/drivers/aws_s3/` (~79% overall, specific gaps)
**Need ~8 tests for:**
- [ ] Lifecycle: all 4 lifecycle methods at 0%
- [ ] Scrub operations: all 3 scrub methods at 0%
- [ ] Stream ops: `StreamObject`, `PutObjectStream` (both 0%)

### MEDIUM PRIORITY — Gateway Layer (<50% coverage)

#### `internal/gateway/` (~39% overall)
**Need ~12 tests for:**
- [ ] `backend_impl.go`: `ListBuckets`, `adminBridge`, `CopyObject`, `CreateBucket`, `DeleteBucket` (all 0%)
- [ ] FTP gateway: all 4 config methods at 0%
- [ ] NFS gateway: all 4 config methods at 0%
- [ ] S3 gateway: all 4 config methods at 0%

#### `internal/gateway/webdav/` (~66% overall)
**Need ~8 tests for:**
- [ ] File operations: `Mkdir`, `deletePrefix`, `Rename`, `Write`, `Readdir` (multiple 0%)
- [ ] FileInfo methods: `Sys`, `Mode` at 0%

### LOW PRIORITY — Infrastructure / Helpers (<60% coverage)

#### `internal/clilib/` (~69% overall, client.go all 0%)
**Need ~4 tests for:**
- [ ] `NewClientWithHTTP` (0%) - HTTP client construction
- [ ] `Endpoint`, `Error` methods (both 0%)
- [ ] `DeleteJSON` (0%) - DELETE request handler

#### `internal/clustersecret/` (~68% overall)
**Need ~4 tests for:**
- [ ] `LockAll`, `HasAdmins` at 0%
- [ ] `EqualBytes` at 0%
- [ ] `DeleteWrappedCSK` (0%)

#### `internal/auth/oidc.go` (multiple 0% methods)
**Need ~5 tests for:**
- [ ] `Issuer`, `AutoProvision`, `ElevationAuthCodeURL`, `Exchange` (all 0%)

---

## Frontend Test Fixes Needed

### Blocking Issues — Prevent Coverage Collection

#### PersonaPill React Hooks Issue
**File:** `src/components/layout/PersonaPill.tsx:75`
```typescript
const sessionEndedFiredRef = useRef<Set<number>>(new Set());
```
**Problem:** "Cannot read properties of null (reading 'useRef')" - multiple React instances or version mismatch
**Fix Priority:** HIGH — blocks coverage on all affected components

#### UserMenu Mock Issues  
**File:** `src/shared/ui/UserMenu.test.tsx`
- Missing `useOrgCapabilities` export in mock
- Need to add partial mock:
```typescript
vi.mock(import("@/shared/api/queries"), async (importOriginal) => {
  const actual = await importOriginal()
  return { ...actual, useOrgCapabilities: vi.fn() }
})
```

#### LoginForm Mock Issues
**File:** `src/shared/auth/LoginForm.tsx`
- Missing `useActiveSkin` export in mock

#### Test File Cleanup
- Remove/rename `react-error-321.spec.ts` — uses Playwright import, not vitest
- Prefix test files with `-` to exclude from route tree warnings:
  - `src/routes/admin/__tests__/first-run.test.tsx` → `-first-run.test.tsx`

---

## Estimated Effort

| Category | Packages | Tests Needed | Hours |
|----------|----------|--------------|-------|
| High Priority (API) | 5 | ~30 | 12-16h |
| Medium (Drivers) | 3 | ~35 | 14-20h |
| Medium (Gateway) | 2 | ~20 | 8-12h |
| Low (Infra) | 4 | ~15 | 4-6h |
| Frontend Fixes | N/A | ~10 fixes | 3-5h |
| **Total** | | **~115 tests/fixes** | **41-59h** |

---

## Recommended Order for v1.13.8+

### Sprint 1 (Week 1): Frontend Test Fixes + Critical API
1. Fix PersonaPill React hooks issue (blocking all coverage)
2. Add missing mocks to UserMenu, LoginForm tests  
3. Remove/rename Playwright test file
4. Implement `admin_service_accounts.go` CRUD tests
5. Implement `backup_runner.go` snapshot pruning tests

### Sprint 2 (Week 2): Driver Coverage
1. Garage driver S3 object operations
2. Minio versioning implementation tests
3. AWS S3 lifecycle + scrub tests

### Sprint 3 (Week 3): Gateway + Remaining API
1. WebDAV file operation tests
2. `admin_users.go` user management tests
3. `user_syncs.go` sync CRUD tests

### Sprint 4 (Week 4): Infrastructure Polish
1. clilib client construction tests
2. clustersecret encryption/decryption edge cases
3. OIDC auth flow tests

---

## Notes

- **Smoke Test Results:** postdeploy-ui had 28 FAIL / 7 OK, feature-smoke had 0 FAIL (excellent), mobile-audit had 124 MINOR console errors (React 321)
- **Frontend React Error #321** is widespread across all viewports — investigate before coverage work
- Consider pairing coverage work with React version consistency audit
