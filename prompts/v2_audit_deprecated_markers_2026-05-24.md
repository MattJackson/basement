You are a freshman dev on basement-ui. Working dir: /Users/mjackson/Developer/basement-ui.

# Audit cycle: catalog every deprecated/legacy/backward-compat marker

This is an AUDIT cycle, not an implementation cycle. You produce a STRUCTURED REPORT, not code changes. The senior will read your report and dispatch follow-up cleanup cycles (v2.0.0-beta.2, beta.3, etc.).

## Why

Per [[v2_clean_break]] the v2.0.0 milestone drops all backward-compat shims, deprecated code, and legacy code paths. Senior needs a complete catalog to plan the cleanup cycles. The memory says "~36 files" — confirm or correct that number; enumerate every single one.

## What to scan

Search the entire repo (NOT vendored deps, NOT node_modules, NOT internal/web/dist, NOT generated code, NOT *.lock files):

```
internal/**/*.go (skip _test.go from the count but list separately)
cmd/**/*.go
frontend/src/**/*.{ts,tsx}
frontend/tests/**/*.{ts,tsx}
docs/**/*.md  (mostly historical — note but don't flag as cleanup)
CHANGELOG.md  (skip)
```

## What to look for

Match (case-insensitive, regex-friendly):

1. `// deprecated` — Go single-line comments
2. `// legacy` — Go single-line comments
3. `// for backward compat` / `// for backward-compat` / `// for back compat` / `// for compat`
4. `// backwards-compat` / `// backwards compat` / `// backwards compatibility`
5. `// for compatibility` / `// for compat`
6. `// kept for back-compat` / `// kept for compat`
7. `/* @deprecated */` and `@deprecated` JSDoc tags (TS/TSX)
8. `// TODO: remove after v2` / `// TODO: drop in v2` / `// TODO: clean up post-vX`
9. `const DEPRECATED_` / `DEPRECATED_` constant prefixes
10. `Deprecated:` Go doc comment style (godoc reserved word)
11. `// v1.x.0b legacy gateway field` / similar version-tagged legacy markers
12. `// dual-mirror` / `// mirror to old shape`
13. `if (typeof X === "old-shape")` / `if (X.legacyField)` patterns
14. `// 301 redirect for back-compat` / `// 308 for back-compat`
15. `// shim` / `// shimmed` (anything tagged as a temporary bridge)

## Report format

Write the report to `docs/audit-v2-cleanup-2026-05-24.md` (create file). Structure:

```markdown
# v2.0 Cleanup Audit (2026-05-24)

Total files with at least one marker: N
Total markers found: M
Estimated cleanup cycles needed (1 cycle = ~10-15 files of similar concern): K

## Summary by category

| Category | Files | Markers |
|---|---|---|
| Deprecated API surface | ... | ... |
| Legacy data shapes | ... | ... |
| Backward-compat redirects | ... | ... |
| Deprecated dependencies | ... | ... |
| Other | ... | ... |

## Findings (one row per marker)

### internal/auth/policy/types.go

- **Line 47**: `// Deprecated: bucket_user role removed in v2.0.0a` — kept as tombstone after v2.0.0a; **action: drop entirely in beta.2**
- ...

### frontend/src/shared/api/queries.ts

- **Line 234**: `// for backward compat with v1.7 service-account secret format` — **action: investigate, may still be used by clients on old token format**
- ...
```

For each entry include:
- file path (repo-relative)
- line number(s)
- the marker text VERBATIM
- 3-5 lines of surrounding context (the code being annotated)
- recommended action: one of:
  - **drop**: clear-cut, no risk; included in next cleanup cycle
  - **investigate**: looks like compat but unclear if it's still needed; senior must decide
  - **keep**: tombstone-style comment for historical context (e.g. `// Deprecated: foo was removed in v2.0a, do not re-add`) — these we keep
  - **already-handled**: matches an already-shipped removal (e.g. bucket_user remnants)

## Don't

- Do NOT make any code changes. Audit only.
- Do NOT count comments inside `_test.go` files toward the cleanup total (list them in a separate "Test files" subsection — they may be removed when the deprecated code goes)
- Do NOT count entries in CHANGELOG.md or docs/release-notes/ (those are historical)
- Do NOT include entries in node_modules, vendor/, generated OpenAPI bindings, or third-party code
- Do NOT include matches inside string literals (e.g. an error message that says "deprecated") unless the literal IS the deprecation

## After writing the report

Commit ONLY the report file:

```bash
git -C /Users/mjackson/Developer/basement-ui add docs/audit-v2-cleanup-2026-05-24.md
git -C /Users/mjackson/Developer/basement-ui commit -m "audit: catalog v2.0 cleanup markers (N files, M markers)"
git -C /Users/mjackson/Developer/basement-ui push origin HEAD:main
git -C /Users/mjackson/Developer/basement-ui ls-remote origin main | head -1
```

NO TAG for this cycle — it's an audit, not a release.

## Hard constraints

- NO Co-Authored-By, NO `--no-verify`, NO emojis
- ONE commit
- Push HEAD:main (no tag)
- `git commit <path>` pathspec form (NEVER `git add -A` or `.`)
- The report must be readable — use markdown tables and headers liberally

## Acceptance

1. `docs/audit-v2-cleanup-2026-05-24.md` exists on origin/main
2. Every marker category from the "What to look for" list has been searched (state "0 found" for any category with no matches)
3. Total counts are accurate (cross-check by running `grep -c` after the fact)
4. Each finding has a recommended action

Return summary AFTER push succeeds. Estimated complexity: 30-60 min (mostly grep + write).
