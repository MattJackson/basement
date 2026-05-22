# ADR-0003: Sudo-style admin elevation

- **Status**: Accepted
- **Date**: 2026-05-21
- **Decision-maker**: Operator (product manager); senior wrote up
- **Triggered by**: Operator session on v1.1.0c — *"should matthew be allowed to be user and a cluster admin and ui admin? will it work? or should there be 3 users with 1 role?"* → after discussion of (A) one user N roles, (B) separate accounts per role, (C) one user N roles with sudo escalation, operator picked C: *"Option C YES LOVE IT"*
- **Builds on**: [ADR-0001](./0001-rbac-three-tier-creds.md) (three-tier roles), [ADR-0002](./0002-region-tier-user-model.md) (region tier)

## Context

Today (post-v1.1.0c), a single basement account can hold multiple roles: matthew has `host_admin@*`, `cluster_admin@cluster:*`, plus his UserRegion(s) for user-tier access. He flips between `/admin/*` and `/files/*` via the persona pill in the UserMenu. Both areas are accessible at any time during the session.

This matches how the human actually works — matthew is one person with multiple hats — but the blast radius is wide:
- "I thought I was in user view" → accidentally clicks Delete Cluster
- "I'm just browsing files" → admin destructive action one click away
- No friction between read-the-world and change-the-world

Three patterns considered:

| Option | Model | Verdict |
|---|---|---|
| A | One user, N roles, free movement | Current. Wide blast radius. |
| B | N accounts (matthew-host, matthew-cluster, matthew-user) | Friction (3 logins), OIDC awkward, cluster-admin sharing creates account explosion |
| C | One user, N roles, **escalation required** to enter admin mode | Lowest cognitive cost; matches Linux `sudo` |

## Decision

basement adopts **sudo-style admin elevation** on top of the existing one-user-N-roles model.

### State machine

Every session is in one of three modes:

| Mode | Capabilities exercised | Banner | TTL |
|---|---|---|---|
| **USER** | UserRegion access + read-only shares/syncs | Persona pill = USER | Session lifetime |
| **ADMIN** | All `host:*`, `cluster:*`, `bucket:*`, `key:*`, `policy:*` capabilities | Persona pill = ADMIN (orange tint) + countdown chip | 15 min idle, then auto-revert to USER |
| **ELEVATED** | Same as ADMIN but for destructive ops requiring fresh auth (delete cluster/bucket/key/user, edit policy matrix) | Persona pill = ADMIN + lightning bolt | 5 min idle, then back to ADMIN |

### Transitions

```
USER ──(re-enter password OR OIDC challenge)──▶ ADMIN
ADMIN ──(re-enter password)──▶ ELEVATED
USER ◀──(15 min idle in ADMIN)── ADMIN
USER ◀──(click "drop privileges" button)── ADMIN
ADMIN ◀──(5 min idle in ELEVATED)── ELEVATED
USER ◀──(logout)── any
```

### Capability gating

The enforcer (`internal/auth/policy`) stays the same. The NEW layer is at the handler level: every capability is annotated with a **minimum mode**:

| Capability domain | Min mode |
|---|---|
| `bucket:view`, `objects:*`, `share:*` | USER (always allowed) |
| `cluster:test`, `cluster:view_layout`, `bucket:edit_alias`, `key:edit_permissions` | ADMIN |
| `cluster:delete`, `bucket:delete`, `key:delete`, `host:manage_users`, `host:manage_policies`, `policy:edit_matrix`, `policy:assign_role`, `cluster:edit_layout` | ELEVATED |

The `requireCapability(w, r, capID, scope)` helper grows a check: if the user's CURRENT mode is below the capability's minimum, return 403 with `mode_required: "admin"` so the FE prompts for re-auth.

### Frontend behavior

- USER mode is default after login.
- "Switch to admin view" in UserMenu → prompts for password → on success, mode = ADMIN, persona pill flips ADMIN with countdown.
- Hovering or clicking any destructive button (Delete cluster, Delete bucket, etc.) → if mode = ADMIN, prompts for password → on success, mode = ELEVATED + the action proceeds (5 min window for subsequent destructives without re-auth).
- 15 min idle in ADMIN: persona pill flashes amber 10s before, then auto-flips to USER. Toast: "Admin session ended. Re-authenticate to elevate."
- "Drop privileges" button next to the countdown chip → instant flip to USER.

### Audit log

The existing `actorRole` field on audit Events gains values: `user` / `admin` / `elevated`. Every destructive action logged with `actorRole: "elevated"` for incident analysis.

### Cookie shape

The existing JWT in `__Host-session` cookie gains two claims:
- `mode`: "user" | "admin" | "elevated"
- `modeExpiresAt`: unix timestamp

On every request, the gate middleware checks `now() < modeExpiresAt`; if expired, downgrades mode in-memory (cookie not re-issued; just downgrade for the request). The next response writes a fresh cookie with the downgraded mode.

Re-auth issues a new cookie with bumped mode + new modeExpiresAt.

### OIDC integration

For OIDC users, re-auth means triggering a fresh OIDC challenge with `prompt=login&max_age=0`. Returns to basement via callback, server verifies the fresh login claim, issues elevated cookie. Operator-configured: `OIDC_ELEVATION_PROMPT` env var defaults to `login` (force re-entry).

### What stays unchanged

- Policy matrix (`/admin/policies`) — role definitions + assignments stay data-driven
- ADR-0001's three-tier role model
- ADR-0002's region abstraction
- All four drivers
- Two-phase delete confirm tokens — those stay AS WELL (defense in depth)

## Consequences

### Good

- Lowest cognitive cost: one identity, one login, one cookie
- Standard pattern (Linux sudo, GitHub sudo-mode, GCP IAM impersonation)
- Audit log gets richer (mode at action time)
- Blast radius for accidents drops dramatically — no destructive op without recent re-auth
- Composable with future SSO patterns (step-up auth via OIDC ACR claim)

### Painful

- Each destructive op may prompt for password — friction. Mitigated by the 5-min ELEVATED window so multi-step admin workflows don't re-prompt every click.
- OIDC re-auth UX depends on IDP — some IDPs cache the auth and skip the prompt, some don't. Operator-configurable.
- Idle timeout = clock-watching. Configurable (`BASEMENT_ADMIN_TTL_SEC`, `BASEMENT_ELEVATED_TTL_SEC`).

### Open questions

- Should ELEVATED be REQUIRED for `policy:edit_matrix`? It's destructive (changes who can do what). Default: yes.
- Should the API return a structured "elevation required" response so the FE can pop a modal in-line, rather than navigating to a re-auth page? Yes — `{error: {code: "ELEVATION_REQUIRED", mode_required: "admin", endpoint: "/api/v1/auth/elevate"}}`.

## Implementation plan

Three steps. After v1.1.0 ships clean.

1. **v1.2.0a** — Backend mode state machine: JWT claims (`mode`, `modeExpiresAt`), `POST /api/v1/auth/elevate` endpoint, middleware that downgrades expired modes per-request, capability-to-min-mode mapping in `internal/auth/policy/`. Tests cover mode transitions + downgrade + elevation expiry.
2. **v1.2.0b** — Frontend elevation prompt: modal that re-takes password, calls `/auth/elevate`, on 200 updates local mode state. Persona pill gains countdown + amber-pre-warning. "Drop privileges" button.
3. **v1.2.0c** — OIDC elevation: when current session is OIDC, "Elevate" triggers OIDC challenge with `prompt=login` instead of password modal. Callback bumps mode.

After all three: tag v1.2.0.

## Relation to existing memory

- `[[role_model_three_axes]]` — Unchanged. Role model stays. Mode is an orthogonal axis ON TOP.
- `[[feedback_basement_doesnt_invent_permissions]]` — Sudo-mode is enforced at the basement boundary, not invented as a backend permission. The backend still sees the user's S3 key signing requests.
- `[[persona_split_user_vs_admin]]` — Sharpened. USER mode literally can't reach admin destructive ops; the persona pill becomes load-bearing security UX.

## Tags

`rbac`, `auth`, `security`, `v1.2`, `elevation`, `sudo`

---

## Amendment: v1.3.0a.4 — Two-mode simplification + operator-configurable TTL

**Date**: 2026-05-22
**Triggered by**: Operator session post-v1.2.0 — three-mode model (USER/ADMIN/ELEVATED) proved over-complicated; persona-switch ergonomics were wrong (no auto-elevate); operator wanted return-to-user on timer expiry. Operator-confirmed quotes:
- *"id say elevate before you go to the admin console, now your an admin for x mins"*
- *"auto switch back to user after x"*
- *"ui admin configurable. maybe they allow 2hr or something"*

### Changes from the original ADR-0003

1. **Drop ELEVATED mode.** USER + ADMIN only. ADMIN can do every capability including destructives. The TTL is the safety, not a sub-mode. ELEVATED was over-engineering — adding cognitive overhead without buying real protection.
2. **Elevate BEFORE entering admin console**, not on first 403. Clicking "Switch to admin" in UserMenu opens the password modal up front. On success, mode = ADMIN, navigate to /admin. Cancel = stay where you are.
3. **TTL is operator-configurable via `/admin/system`**, not env-only. Host Admin picks 5 min / 15 min / 30 min / 1 hr / 2 hr / 8 hr / custom in the UI. Default 15 min. Range 60s – 24h. Stored in `org_capabilities.json`.
4. **Timer expiry drops privileges in place, doesn't yank user.** Banner replaces the page rather than navigating away. Operator can finish reading / save form / re-elevate without losing context. Next admin action gets the standard 403 ELEVATION_REQUIRED → modal.
5. **Ramp warnings**: persona pill amber at <2 min, red at <30s, toast at <30s with "Stay admin" extend button.
6. **Capability gating** simplifies: every admin capability requires `ADMIN` (no ELEVATED tier). USER capabilities (objects:*, share:*, bucket:view) remain unrestricted.

### Migration from v1.2.0 (ELEVATED → ADMIN collapse)

- Cookies issued with `mode="elevated"` are treated as `mode="admin"` on read (with the original ELEVATED TTL preserved). Silent migration; no logout needed.
- Code: `MinModeFor()` returns only `ModeUser` or `ModeAdmin`. The `ModeElevated` constant is kept as an alias for ModeAdmin for one cycle then removed.
- Audit log: `actorRole` values become `user` or `admin` only. Existing entries with `elevated` stay readable (they're historical records).

### Implementation in v1.3.0a.4

- Backend: drop ELEVATED from `internal/auth/policy/min_mode.go`; add `OrgCapabilities.AdminSessionTTLSec` (default 900, range 60-86400); `/auth/elevate` reads TTL from org config; `requireCapability` gate compares mode (admin-required) without sub-mode logic.
- Frontend: `UserMenu` "Switch to admin" calls runWithElevation+navigate; `AuthModeHydrator` on expiry shows persistent banner instead of redirect; `/admin/system` adds the TTL setting card (dropdown + custom input).
- ADR-0003 main body above is the v1.2.0 reference; the model going forward is this amendment.
