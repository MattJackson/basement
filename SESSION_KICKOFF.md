# Session Kickoff — basement v2.0.0 drive

> Resume script for the next session. Re-read this before doing anything.

## TL;DR — where we are right now

- **Live deploy:** `v2.0.0-beta.37` (Watchtower auto-rolls from latest tag)
- **Latest release-candidate tag:** `v2.0.0-rc.2`
- **GA tag:** NOT yet (`v2.0.0` final awaits explicit operator OK)
- **Big in-flight migration:** ADR-0009 capability-based RBAC, 5 phases
  — Phase A + B shipped, Phase C running (beta.39 freshman), Phase D + E queued

## First three commands when resuming

```bash
# 1) Get oriented
git -C /Users/mjackson/Developer/basement-ui pull --ff-only origin main
git -C /Users/mjackson/Developer/basement-ui log --oneline -10
git -C /Users/mjackson/Developer/basement-ui tag -l | tail -10

# 2) Check live + version
curl -s https://basement.pq.io/api/v1/health
curl -s https://basement.pq.io/api/v1/version

# 3) Check if there's a freshman cycle in progress
ls -la /tmp/basement/freshman_beta*.log 2>/dev/null | tail -5
git -C /Users/mjackson/Developer/basement-ui status --short    # any WIP from freshman?
```

## What's the next action

Decision tree:

1. **Is a freshman cycle running?** (background bash with opencode)
   - Yes → wait for notification, then continue migration sequence
   - No → check if last freshman pushed cleanly, then dispatch next

2. **Last shipped tag dictates next move:**

| Last tag | Next action |
|---|---|
| `v2.0.0-beta.37` (current) | Dispatch `prompts/v2.0.0-beta.39_capability_middleware_migration_2026-05-28.md` if not already running |
| `v2.0.0-beta.39` | Dispatch `prompts/v2.0.0-beta.40_capability_frontend_useCan_2026-05-28.md` |
| `v2.0.0-beta.40` | Dispatch `prompts/v2.0.0-beta.41_capability_sweep_components_2026-05-28.md` |
| `v2.0.0-beta.41` | ADR-0009 migration complete. Run full smoke + a11y pass; ask operator about v2.0.0 GA tag |

## The architectural arc to keep in mind

ADR-0009 capability migration (read `docs/adr/0009-capability-based-rbac.md`):

```
Phase A ✓ ADR + operations matrix (commit 09514ab)
Phase B ✓ internal/auth/capabilities.go + UserResponse.Capabilities (commit 54f33f7, in beta.37 tree)
Phase C ⏳ backend RequireCapability middleware + sweep + DELETE super-admin branch (beta.39)
Phase D … FE useCan + ProtectedRoute rewrite (beta.40)
Phase E … sweep all activeRole.kind === ... sites in components (beta.41)
```

**The architectural anchor being removed:** `internal/auth/active_role.go:68-72`
"UI Admin is super-admin — passes any cluster route". That branch is the
root cause of the recurring leak class (beta.6, beta.30, beta.36 item 3).
Phase C deletes it.

**The locked role visibility split** (2026-05-28):

| Layer | Owner |
|---|---|
| Cluster connection (admin_url, admin_token, driver, label) | UI Admin |
| Cluster contents (buckets, keys, encryption admins, lifecycle, grants) | Cluster Admin |
| Platform (users, policies, audit, system, skins) | UI Admin |
| Cross-cluster aggregates (`/admin/buckets`, `/admin/usage`) | UI Admin |
| Own resources (`/files`, `/keys`, ...) | User |

UI Admin is NOT a super-admin. If one person needs both, they hold two
roles and switch via the persona pill.

## Working agreements (durable, don't violate)

- **Freshman is the default** — every delegatable task → opencode cycle.
  Senior only for: speed, multi-domain synthesis, or corrective fixes on
  close-but-wrong freshman output.
- **ONE freshman at a time** — operator machine constraint. Serial
  dispatch by default; parallel only when paths are proven disjoint.
- **No `git add -A` or `git add .`** — stage explicit paths, especially
  when freshman might be mid-cycle with WIP in the working tree.
- **Push HEAD:main BEFORE pushing the tag**, always.
- **Never `Co-Authored-By`** on commits.
- **Use `git -C <abs-path>` not `cd <path> && git ...`** to avoid
  harness prompts.
- **Operator data off-limits** — `classe` + `lsi` + `cheshire` are PROD.
  Destructive tests use ephemeral test clusters only.
- **Verify outcome, not diff** — "tests pass" ≠ "user can do the thing".

## Files / paths to remember

| Path | What |
|---|---|
| `docs/adr/0009-capability-based-rbac.md` | The current architectural North Star |
| `internal/auth/capabilities.go` | Single source of truth for capabilities (Go) |
| `internal/auth/active_role.go:68-72` | The super-admin branch to delete in Phase C |
| `frontend/src/shared/auth/ProtectedRoute.tsx` | Pathname-switch to replace in Phase D |
| `prompts/v2.0.0-beta.NN_*.md` | All freshman prompts live here, NEVER `/tmp/` |
| `/tmp/basement/freshman_beta*.log` | Freshman cycle logs |
| `~/.claude/projects/-Users-mjackson-Developer-basement-ui/memory/MEMORY.md` | Memory index — start here when reading durable knowledge |

## Memory entries to read first

```
~/.claude/projects/-Users-mjackson-Developer-basement-ui/memory/
├── MEMORY.md                                   ← index
├── roadmap_state.md                            ← what's shipped, what's queued
├── role_visibility_matrix.md                   ← the locked role split
├── capability_model_followup.md                ← ADR-0009 phase tracker
├── active_role_vs_legacy_mode.md               ← mode/activeRole/capabilities distinction
├── freshman_workflow.md                        ← how to dispatch freshman cycles
└── feedback_*.md                               ← durable operator preferences
```

## Test credentials

- Live: `matthew` / `password` against `https://basement.pq.io`
- Admin token PATCH for the operator's classe cluster is operator-driven —
  do not auto-PATCH.

## When in doubt

- Read `docs/adr/0009-capability-based-rbac.md` first.
- Then read the relevant memory file from MEMORY.md.
- Then ask the operator. Don't invent.
