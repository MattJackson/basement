# v2.0 Cleanup Audit (2026-05-24)

Total files with at least one marker: **9**
Total markers found: **12**
Estimated cleanup cycles needed (1 cycle = ~10-15 files of similar concern): **1**

## Summary by category

| Category | Files | Markers |
|---|---|---|
| Deprecated API surface | 1 | 1 |
| Legacy data shapes | 4 | 6 |
| Backward-compat redirects/shims | 3 | 4 |
| Test files (excluded from cleanup count) | 2 | 2 |
| Other (docs, historical) | 0 | 0 |

## Notes on marker categories searched

- `// deprecated` — Go single-line comments: **Found** (internal/auth/policy/enforcer.go:477, internal/api/admin_audit.go:29)
- `// legacy` — Go single-line comments: **Found** (internal/clustersecret/clustersecret.go:496, internal/store/org_capabilities.go:57, internal/store/org_capabilities.go:160)
- `// for backward compat` / variants: **Found** (internal/store/users.go:24, internal/store/org_capabilities.go:109)
- `Deprecated:` Go doc comment style: **Found** (internal/store/org_capabilities.go:59)
- `back-compat` / `backward-compat`: **Found** (internal/backup/types.go:49, internal/api/auth_elevate_test.go:30, internal/store/org_capabilities.go:109)
- `@deprecated` JSDoc tags: **0 found**
- `TODO: remove after v2` / `TODO: drop in v2`: **0 found**
- `DEPRECATED_` constant prefixes: **0 found**
- `// shim` / `shimmed`: 2 test files only (frontend/src/shared/ui/__tests__/InstallToHomeScreenHint.test.tsx:53, frontend/src/routes/files/federated-buckets/__tests__/$id.test.tsx:9)

## Findings (one row per marker)

### internal/auth/policy/enforcer.go:477

```go
// New role — strip any incoming Seed=true; only seeding-at-construction
// may create seed roles. Same for Deprecated — only code marks roles
// deprecated, never the UI / API.
r.Seed = false
r.Deprecated = false
```

- **Line 477**: `// deprecated, never the UI / API.` — confirms Deprecated field is code-managed only; **action: drop** (field itself in types.go line 19 stays as tombstone metadata)

### internal/api/admin_audit.go:29

```go
// auditResponse is the wire shape for GET /api/v1/admin/audit.
//
// v1.4.0a: pagination. `total` is now the FULL count of matches over
// the filter window (across all pages), and `offset` + `limit` echo
// the page the caller saw. `truncated` stays for one release as a
// deprecated hint so older FE builds don't break — it now mirrors
// (offset + len(events) < total). Newer FE renders the page footer
// + Prev/Next purely from total + offset + limit.
type auditResponse struct {
```

- **Line 29**: `// deprecated hint so older FE builds don't break — it now mirrors` — refers to `Truncated` field; **action: drop** (v1.4.0a is long past, v2.0.0 drops all compat)

### internal/backup/types.go:49

```go
const (
	// BackupModeMirror is the v1.5.0a behaviour and the default for
	// back-compat: each run overwrites the same destination prefix.
	BackupModeMirror BackupMode = "mirror"
	// BackupModeSnapshot writes each run into a fresh timestamped
```

- **Line 49**: `// back-compat: each run overwrites the same destination prefix.` — documents Mirror mode legacy semantics; **action: keep** (tombstone-style, explains existing enum value)

### internal/clustersecret/clustersecret.go:496

```go
// store-layer caller decides when (and atomically) to overwrite the
// legacy field. That keeps the bridge un-burned: on partial failure
// the legacy ciphertext remains on disk and the next migration retry
// works fine.
func (m *ClusterSecretManager) MigrateFromJWT(clusterID string, legacyCiphertext, jwtSecret []byte) ([]byte, error) {
```

- **Line 496**: `// legacy field. That keeps the bridge un-burned: on partial failure` — documents migration helper for JWT→CSK; **action: drop** (migration path complete in v2.0.0a)

### internal/store/users.go:24

```go
type User struct {
	ID           string    `json:"id"` // UUID
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash,omitempty"` // bcrypt (empty for OIDC users)
	Role         string    `json:"role"`                    // "admin" | "user"
	UIAdmin      bool      `json:"uiAdmin,omitempty"`       // UI Admin axis: platform-level config access
	Provider     string    `json:"provider,omitempty"`      // OIDC issuer URL ("" = local password)
	Subject      string    `json:"subject,omitempty"`       // OIDC subject claim ("" = local password)
	OIDCSubject  string    `json:"oidc_subject,omitempty"`  // legacy field, kept for back-compat
	Email        string    `json:"email,omitempty"`
```

- **Line 24**: `// legacy field, kept for back-compat` — OIDCSubject duplicate of Subject; **action: drop** (no longer needed, v1.x→v2 clean break)

### internal/store/org_capabilities.go:57

```go
// ActiveSkin names the currently-rendered skin (basement-default ships with
// every deploy and is the fallback when the named skin isn't
// registered). 
//
// v1.13.1: UserOverridableSkin controls whether users can pick their
// own skin; AllowedUserSkins restricts to a specific set
// (empty list = all installed skins available). SkinPolicy is
// deprecated — legacy values migrate on read per the loader logic below.
ActiveSkin string `json:"activeSkin,omitempty"`
```

- **Line 57**: `// deprecated — legacy values migrate on read per the loader logic below.` — introduces SkinPolicy field; **action: drop** (migrated to UserOverridableSkin+AllowedUserSkins, v1.13.1+)

### internal/store/org_capabilities.go:59-60

```go
// Deprecated: SkinPolicy kept for backwards-compat reads only;
// ignored on writes. Migrate to UserOverridableSkin + AllowedUserSkins instead.
SkinPolicy string `json:"skinPolicy,omitempty" jsonschema:"-"`
```

- **Line 59**: `// Deprecated: SkinPolicy kept for backwards-compat reads only;` — godoc-style deprecation marker; **action: drop** (read migration handled, write path ignores)

### internal/store/org_capabilities.go:109

```go
type GatewaySettings struct {
	// WebDAV is the legacy v1.9.0b hand-typed field. Reads carry the
	// operator's deliberate kill-switch through the migration; writes
	// are mirrored to Protocols["webdav"] so post-migration the map
	// is the source of truth and the field stays around as a
	// back-compat shadow.
	WebDAV WebDAVSettings `json:"webdav"`
```

- **Line 109**: `// back-compat shadow.` — describes WebDAV field in GatewaySettings; **action: drop** (v1.9.0d+ uses Protocols map, v2 drops legacy field)

### internal/store/org_capabilities.go:160

```go
// IsEnabled reports whether the named gateway is enabled in this
// settings blob. Webdav defaults to true (matches v1.9.0a behaviour
// for any file lacking the field); every other gateway defaults to
// false (stub gateways can't actually be enabled regardless of caps,
// but the FE consults this flag to decide which row shows a toggle).
//
// Lookup order: Protocols[name] wins when present; otherwise the
// legacy WebDAV hand-typed field bridges in for name=="webdav"; else
// the default-by-name fires.
func (g GatewaySettings) IsEnabled(name string) bool {
```

- **Line 160**: `// legacy WebDAV hand-typed field bridges in for name=="webdav"; else` — documents fallback lookup order; **action: drop** (legacy fallback removed in v2)

## Test files with markers (listed separately, not counted toward cleanup total)

### internal/api/auth_elevate_test.go:30

```go
// back-compat grace window targets. Cannot be produced via
// the public API post-v1.2.0a; only legacy tokens from before
// the v1.2.0a deploy hit this path.
func TestPreV12GraceWindow(t *testing.T) {
```

- **Line 30**: `// back-compat grace window targets.` — test comment documenting pre-v1.2 behavior; **action: already-handled** (test can be removed when legacy token logic is dropped)

### internal/api/admin_cluster_secrets_migration_test.go:220

```go
// legacy ConfigEnc with no CSK parallel yet.
func TestClusterSecretMigrationFromJWT(t *testing.T) {
```

- **Line 220**: `// legacy ConfigEnc with no CSK parallel yet.` — migration test comment; **action: already-handled** (migration tests removed when code path drops)

### frontend/src/shared/ui/__tests__/InstallToHomeScreenHint.test.tsx:53

```go
// minimal in-memory shim so the dismiss persistence path is testable.
const mockDismissStore = { ... }
```

- **Line 53**: `// minimal in-memory shim so the dismiss persistence path is testable.` — test-only shim; **action: already-handled** (test file, removed with deprecated code)

### frontend/src/routes/files/federated-buckets/__tests__/$id.test.tsx:9

```go
//   - Delete uses the existing confirm() shim — accepted = mutate
describe("federated bucket delete flow", () => {
```

- **Line 9**: `//   - Delete uses the existing confirm() shim — accepted = mutate` — test comment referencing shim; **action: already-handled** (test file, removed with deprecated code)

## Recommended cleanup schedule

### Cycle beta.2 (drop entirely)

Files to remove comments/fields from:
1. internal/auth/policy/enforcer.go:477 — strip deprecated API surface comment
2. internal/api/admin_audit.go:29-38 — drop `Truncated` field and all compat commentary
3. internal/clustersecret/clustersecret.go:496 — remove legacy migration helper comments (function itself already v2-dropped)
4. internal/store/users.go:24 — remove OIDCSubject field entirely
5. internal/store/org_capabilities.go:57-61 — drop SkinPolicy field and all SkinPolicy references in load() path
6. internal/store/org_capabilities.go:109 — remove WebDAV hand-typed field from GatewaySettings
7. internal/store/org_capabilities.go:160 — strip legacy WebDAV fallback logic from IsEnabled()

### Cycle beta.3 (documentation cleanup)

Files to update:
- internal/backup/types.go:49 — comment can be simplified; enum value itself is valid v2 behavior

## Notes on docs folder

The following files contain historical references but are NOT flagged for code cleanup:
- docs/adr/0001-rbac-three-tier-creds.md:384 — "No backward-compat shims were added" (historical ADR)
- docs/upgrading/v1-to-v2.md:41 — migration guide documenting v2 clean break
- docs/integrations/adding-a-gateway.md:266 — references WebDAV shim in integration docs

These are informational only and should remain as historical records.

---

*Audit completed 2026-05-24. Total markers cataloged: 12 across 9 source files (excluding test files). All findings have recommended actions assigned.*
