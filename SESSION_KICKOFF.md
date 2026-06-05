# Session Kickoff — basement v2.0.0 drive

> Resume script for the next session. Re-read this before doing anything.
> Last updated: 2026-06-04 (after v2.0.0-rc.3 shipped).

## TL;DR — where we are right now

- **Live deploy:** `v2.0.0-rc.3` (Watchtower auto-rolls from latest git tag)
- **Latest tag:** `v2.0.0-rc.3` (annotated; commit `76d2a57`, == `origin/main`)
- **GA tag:** NOT yet (`v2.0.0` final awaits explicit operator OK)
- **Two parallel tracks have been running** — read both:
  1. **Security audit drive — ✅ COMPLETE.** 3-model ensemble (Opus +
     Sonnet + Qwen) sweep of the whole codebase, rounds 1–13 + a
     lower-severity leftovers pass, shipped as rc.1 → rc.2 → rc.3 on
     branch `security/audit-fixes` (now merged to main).
  2. **ADR-0009 capability RBAC migration — ⏸ PARKED at Phase C.**
     Phases A/B/C shipped (beta.37/39). **Phase D + E were never done**
     — the audit drive took priority. The FE prompts are still queued.

- **⚠️ KNOWN LIVE ROUGH EDGE (still present, from Phase C):** the FE
  cluster detail page (`$cid/index.tsx`) fetches wiring AND contents from
  one screen. The backend enforces the split, so a **UI Admin opening a
  cluster detail page gets 403s** on contents calls (buckets/keys/admins/
  lock-status). This is the thing **Phase D (beta.40)** was meant to fix.
  It is STILL LIVE on rc.3. Decide with operator whether to resume the
  migration or ship GA with this known edge.

## First commands when resuming

```bash
# 1) Get oriented
git -C /Users/mjackson/Developer/basement-ui pull --ff-only origin main
git -C /Users/mjackson/Developer/basement-ui log --oneline -10
git -C /Users/mjackson/Developer/basement-ui tag -l 'v2.0.0-rc*'

# 2) Check live + version (live tracks the latest tag = rc.3)
curl -s https://basement.example.com/api/v1/health
curl -s https://basement.example.com/api/v1/version

# 3) Confirm the two-track state for yourself
grep -rl "useCan" frontend/src           # empty => Phase D still not done
ls prompts/v2.0.0-beta.4{0,1}_*.md        # the queued Phase D/E prompts
git -C /Users/mjackson/Developer/basement-ui status --short
```

## What's the next action — ASK THE OPERATOR FIRST

The audit drive is done. The open decision is **which track to resume**,
and that's the operator's call. Lay out the choice:

| Option | What it means |
|---|---|
| **A — Resume ADR-0009 Phase D** | Dispatch `prompts/v2.0.0-beta.40_capability_frontend_useCan_2026-05-28.md`. Adds `useCan` + rewrites `ProtectedRoute`, fixing the live UI-Admin 403-on-detail-page edge. Then Phase E (beta.41) sweeps components. NOTE: renumber — these are post-rc.3 now, so they'd ship as `rc.4`/`rc.5` (or beta-on-a-branch), not beta.40/41. Confirm numbering with operator. |
| **B — Human-review the flagged audit items** | The audit FLAGGED (did not auto-fix) several real-but-complex issues needing design decisions — see below. These are senior + operator work, not freshman. |
| **C — Cut v2.0.0 GA** | If the operator accepts the known FE 403 edge, tag `v2.0.0`. Run a full smoke + a11y pass first. |

Do not pick for them. Surface A/B/C, recommend based on their priority.

## ⚑ FLAGGED for human design review (audit found, did NOT auto-fix)

These are REAL but need design decisions — do not "fix" blind:

- **federation engine** (commit `838ca73` body): auto-failover split-brain
  with no fencing; never-synced-replica promotion; delete-advances-LastSync
  data loss; engineCtx/cancel lock-discipline races. Treat as
  "audited, partial fix, needs a focused human pass."
- **`store/crypto.go` at-rest KDF** = `sha256(jwtSecret)` — no salt / domain
  separation. Fixing it requires a **versioned-ciphertext migration**
  (changing the KDF breaks decryption of every existing stored secret).
  Decide: migration cycle vs accept.
- **`/admins` gating** — operator said leave as-is; revisit if Phase D/E resume.

## What the audit drive actually shipped (rc.1 → rc.3)

Full 3-model sweep + verify + fix across every security-critical surface:
auth/RBAC, API enforcement, user-tier ownership, crypto, drivers, job
engines, store internals, OIDC, gateways, bootstrap, federation, and the
FE security core (skin renderer XSS, API client, route guards, auth/mode).

**rc.3 (commit `76d2a57`) — the lower-severity leftovers pass:**
- public_shares CommonPrefixes prefix-filter; user_syncs goroutine ID
  snapshot; stateless `ValidateSchedule()` (killed the `__dryrun__` race);
  admin_buckets/keys cid guards inverted to fail-closed (empty cid → 400).
- webdav gateway: drain guard (503 once Stopped), real audit result via
  wrapped ResponseWriter, IPv6 clientIP via `net.SplitHostPort`.
- config: reject non-positive SESSION_TTL/AUDIT_RETENTION; bcrypt-validate
  ADMIN_PASSWORD_HASH; `writeSecret` fsync; DataDir `0700`.
- `User.LogValue()` redacts PasswordHash from slog; skin upload server-side
  validation mirroring the r13 FE guards (HSL palette / font / CSS-length).
- FE: useUser `/auth/me` staleTime 5m→30s; AuthModeHydrator re-applies the
  expiry downgrade to the server payload (no stale-elevated re-promotion flap).

Earlier rounds' real bugs (already fixed): pause-sync cross-tenant IDOR,
webhook SSRF, gateway tenant-isolation, `matthew` backdoor seed, pre-v1.2
perpetual admin grace. ~8 P0 false-positives were verified-and-retired
(cross-cluster IDORs were route-mw cid-scoped; "block internal endpoints"
would break self-hosted; transport-error mapping is intentional).

Audit tracker: `scratch/audit/GOAL.md`. Portable reusable goal for
auditing another app: `/Users/mjackson/Developer/audit-goal.md`.

## ADR-0009 migration arc (for when Phase D/E resume)

Read `docs/adr/0009-capability-based-rbac.md`. Source of truth for caps:
`internal/auth/capabilities.go`.

```
Phase A ✓ ADR + operations matrix (commit 09514ab)
Phase B ✓ capabilities.go + UserResponse.Capabilities (commit 54f33f7, beta.37)
Phase C ✓ backend RequireCapability middleware + full sweep + DELETED
          the "UI Admin is super-admin" branch in active_role.go (beta.39,
          commit 9e05797 — do-not-reintroduce guard comment in place)
Phase D ⏸ FE useCan + ProtectedRoute rewrite  (prompt queued: beta.40)
Phase E ⏸ sweep activeRole.kind===… sites in components (prompt queued: beta.41)
```

Phase C decisions that D/E must mirror (operator-approved):
- `cluster.wiring.read` is the ONE wiring cap held by BOTH roles (ui-admin
  any cluster, cluster-admin their cluster) — GET /admin/clusters/{cid} + driver-info.
- New caps beyond the ADR matrix that the FE `capabilities.ts` mirror MUST
  include: `cluster.wiring.read`, `cluster.layout.write`, `cluster.scrub.write`,
  `cluster.keys.update`, `platform.{system,oidc,skins,onboarding}.read`.
- Bucket lifecycle re-homed ui-admin → cluster-admin contents.

## The locked role visibility split (don't violate)

| Layer | Owner |
|---|---|
| Cluster connection (admin_url, admin_token, driver, label) | UI Admin |
| Cluster contents (buckets, keys, encryption admins, lifecycle, grants) | Cluster Admin |
| Platform (users, policies, audit, system, skins) | UI Admin |
| Cross-cluster aggregates (`/admin/buckets`, `/admin/usage`) | UI Admin |
| Own resources (`/files`, `/keys`, …) | User |

UI Admin is NOT a super-admin. One person needing both holds two roles and
switches via the persona pill.

## Working agreements (durable, don't violate)

- **Tags trigger LIVE Watchtower deploy** — only tag when the operator
  explicitly asks. Push `HEAD:main` BEFORE pushing the tag, always.
- **rc tags are dot-separated:** `v2.0.0-rc.1/.2/.3`. Betas: `v2.0.0-beta.NN`.
  Operator's "rc3" → normalize to `rc.3`.
- **Never `Co-Authored-By`** on commits — Matthew's name only.
- **No `git add -A`/`git add .`** — stage explicit paths. `operator-log.md`
  and `scratch/` are untracked and must STAY unstaged.
- **`git -C <abs-path>`, never `cd <path> && git …`** (avoids harness prompt).
- **Never write important files to `/tmp`** — use `<repo>/scratch/`.
- **Operator data off-limits** — `classe` + `lsi` + `cheshire` are PROD.
- **Verify outcome, not diff** — "tests pass" ≠ "user can do the thing".
- **Freshman default for delegatable work; senior for speed / synthesis /
  corrective fixes.** ONE freshman at a time (operator-machine constraint).

## Files / paths to remember

| Path | What |
|---|---|
| `docs/adr/0009-capability-based-rbac.md` | Architectural North Star |
| `internal/auth/capabilities.go` | Single source of truth for caps (Go) |
| `frontend/src/shared/auth/ProtectedRoute.tsx` | Still pathname-switch — Phase D rewrites it |
| `scratch/audit/GOAL.md` | 3-model audit tracker (round state table) |
| `/Users/mjackson/Developer/audit-goal.md` | Portable reusable audit goal |
| `prompts/v2.0.0-beta.40_*.md`, `…beta.41_*.md` | Queued Phase D/E freshman prompts |
| `prompts/` | All freshman prompts live here, NEVER `/tmp/` |

## Test credentials

- Live: `matthew` / `password` against `https://basement.example.com`
- Admin-token PATCH for the operator's `classe` cluster is operator-driven
  — do not auto-PATCH.

## When in doubt

- The audit drive is DONE — don't restart it unless asked.
- For RBAC questions read `docs/adr/0009-capability-based-rbac.md` first.
- Hindsight memory holds the durable lore (loads automatically). The most
  recent entry is "basement v2.0.0-rc.3 shipped — audit leftovers swept".
- Then ask the operator. Don't invent.
