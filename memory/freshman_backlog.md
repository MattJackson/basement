# basement — freshman backlog

> Source of truth: `DESIGN.md` at repo root. Don't deviate inside a
> prompt — if a task needs a design change, file an OPEN question and
> escalate.

Pop from the top. Mark `[done] @<sha>` when shipped + audited. Mark
`[wip] @<task-id>` when dispatched. Strikethrough if descoped.

---

## M0 — Foundations

### Tier-1 (docs / RE)

- [ ] **T1.01** RE writeup: Garage v1 admin API surface
  - Source: `~/Developer/garage/` (cloned source)
  - Deliverable: `docs/garage-admin-api.md` — every endpoint, method,
    request shape, response shape, status codes, with file:line cites
    into the Garage source
  - Used by: T2.04 (driver stubs), T1.02 (OpenAPI draft)
- [ ] **T1.02** OpenAPI v1 draft: `openapi/basement.yaml`
  - Source: `DESIGN.md` §6 + T1.01 output
  - Deliverable: full spec for v1.0 admin scope (auth, capabilities,
    nodes, layout, buckets, keys, diagnostics, S3 data plane)
  - Validate: `swagger-cli validate openapi/basement.yaml`
- [ ] **T1.03** Scoping doc: Garage layout editor UX
  - Source: Garage's zone/capacity/tag model + community UI screenshots
  - Deliverable: `docs/layout-editor-ux.md` — wireframes (ASCII OK),
    interaction model, staging/diff semantics, error cases

### Tier-2 (implementation)

- [ ] **T2.01** Repo scaffold
  - `go.mod` (module `github.com/mattjackson/basement`, Go 1.22+)
  - Empty packages per `DESIGN.md` §9
  - `.gitignore` additions: `internal/web/dist/`, `frontend/node_modules/`,
    `frontend/dist/`, `*.db`, `*.db-journal`
- [ ] **T2.02** Config loader: `internal/config/config.go`
  - Reads env per `DESIGN.md` §10
  - Validates required vars, returns typed `Config` struct
  - Unit test: missing required → error; full env → populated struct
- [ ] **T2.03** Driver interface: `internal/driver/driver.go`
  - Exact signatures per `DESIGN.md` §5
  - `internal/driver/errors.go`: `ErrUnsupported`, `ErrNotFound`,
    `ErrPermissionDenied`, etc.
  - `internal/driver/registry.go`: register / lookup
- [ ] **T2.04** Garage driver stub
  - All methods return `driver.ErrUnsupported`
  - Self-registers as `"garage"` in `init()`
  - Compiles + passes a "registry returns garage" unit test
- [ ] **T2.05** JSON store skeleton
  - `internal/store/store.go`: `Store` interface (Users, Grants,
    Shares, Audit) + opens data dir + sync.RWMutex per file
  - `internal/store/json.go`: atomic write-to-tmp + fsync + rename
    helper; load-or-init-empty on startup
  - `internal/store/users.go`, `grants.go`, `shares.go`: load/save
    `users.json`, `grants.json`, `shares.json` per `DESIGN.md` §7
  - `internal/store/audit.go`: append-only to
    `audit/YYYY-MM-DD.jsonl`, daily rotation, retention cleanup
  - Unit tests: concurrent writes don't corrupt; load → mutate → save
    → reload round-trips; audit retention removes old files
- [ ] **T2.06** HTTP server skeleton: `internal/api/server.go`
  - `chi` router, `/api/v1/*` group, middleware chain (logger,
    recoverer, content-type)
  - `cmd/basement-server/main.go`: load config, open store, instantiate
    driver, start server
  - Smoke test: `GET /api/v1/health` returns `{ "status": "ok" }`
- [ ] **T2.07** Auth: bcrypt + JWT
  - `internal/auth/bcrypt.go`: hash + verify
  - `internal/auth/jwt.go`: issue + parse, 32-byte secret from config
  - `internal/auth/middleware.go`: cookie → user context, 401 on fail
  - Unit tests for both
- [ ] **T2.08** Login handler: `POST /api/v1/auth/login`
  - Verifies against config admin user; sets `__Host-basement_session`
    cookie with proper flags
  - Unit + integration test
- [ ] **T2.09** Capabilities endpoint: `GET /api/v1/capabilities`
  - Calls `driver.Capabilities()`, returns JSON
  - Auth-required but no specific grant needed
- [ ] **T2.10** SPA asset embed: `internal/web/embed.go`
  - `//go:embed dist`
  - Server route `GET /` serves index.html, `/assets/*` serves bundled
    assets, fallthrough to index.html for SPA routes
  - Stub dist with a placeholder index.html until T2.11+
- [ ] **T2.11** Frontend scaffold: Vite + React + TS + Tailwind
  - `pnpm create vite frontend -- --template react-ts`
  - Tailwind + shadcn init (dark mode default in `tailwind.config.ts`)
  - TanStack Router file-based setup
  - Build outputs to `internal/web/dist`
- [ ] **T2.12** API codegen pipeline
  - `openapi-typescript` generates `src/shared/api/types.gen.ts`
  - `openapi-fetch` client at `src/shared/api/client.ts`
  - npm script `gen:api` runs codegen; `prebuild` hook auto-runs it
- [ ] **T2.13** Login screen
  - `src/admin/routes/login.tsx`: React Hook Form + Zod, calls login
    mutation, redirects to `/admin` on success
  - `useUser` hook: queries `/auth/me`, stores in TanStack Query cache
  - `ProtectedRoute` wrapper for authenticated routes
- [ ] **T2.14** Multi-stage Dockerfile + bake
  - `docker/Dockerfile` per `DESIGN.md` §11
  - `docker/docker-bake.hcl` for amd64 + arm64
  - README section: "Build locally"
- [ ] **T2.15** CI: `.github/workflows/ci.yml`
  - golangci-lint, go test, eslint, tsc, vitest
  - Caches Go modules + pnpm store
- [ ] **T2.16** Release CI: `.github/workflows/release.yml`
  - Triggered on tag `v*`
  - Builds + pushes `ghcr.io/mattjackson/basement:vX.Y.Z` + `:latest`

---

## v0.1 — Admin read

- [ ] **T1.04** Per-screen spec: Dashboard
- [ ] **T1.05** Per-screen spec: Nodes
- [ ] **T1.06** Per-screen spec: Buckets list
- [ ] **T1.07** Per-screen spec: Keys list
- [ ] **T2.17** Garage driver: `Capabilities()` (real)
- [ ] **T2.18** Garage driver: `HealthCheck()` against `/v1/health`
- [ ] **T2.19** Garage driver: `ListNodes()` against `/v1/cluster/status`
- [ ] **T2.20** Garage driver: `GetLayout()` against `/v1/cluster/layout`
- [ ] **T2.21** Garage driver: `ListBuckets()` against
      `/v1/cluster/admin/bucket`
- [ ] **T2.22** Garage driver: `ListKeys()` against
      `/v1/cluster/admin/key`
- [ ] **T2.23** API handler: `GET /admin/nodes`
- [ ] **T2.24** API handler: `GET /admin/layout`
- [ ] **T2.25** API handler: `GET /admin/buckets`
- [ ] **T2.26** API handler: `GET /admin/keys`
- [ ] **T2.27** Frontend: Dashboard screen
- [ ] **T2.28** Frontend: Nodes screen (table + status badges + refresh)
- [ ] **T2.29** Frontend: Buckets list
- [ ] **T2.30** Frontend: Keys list
- [ ] **T2.31** Frontend: app shell (sidebar nav, topbar, user menu,
      breadcrumbs)
- [ ] **T2.32** Tag + release v0.1

---

## v0.2 — Admin write

(Filled in after v0.1 audit. Stub names below for reference.)

- T2.33+ Garage driver: bucket CRUD
- T2.34+ Garage driver: key CRUD + permission update
- T2.35+ Garage driver: layout stage / apply / revert
- T2.36+ Bucket detail screen with key-permission grid
- T2.37+ Key detail screen with bucket-permission grid (transposed)
- T2.38+ Layout editor: drag-and-drop, diff preview, stage/apply/revert
        buttons with confirmation modals
- T2.39+ Optimistic mutations for layout staging via TanStack Query

---

## v1.0 — Admin polish

- Diagnostics screen (every admin endpoint as a low-level form)
- Object browser inside bucket detail
- Empty states for every list
- Loading skeletons
- Keyboard shortcuts (cmd+k command palette)
- Accessibility audit (axe-core)
- README + screenshots
- **Deployment artifacts (per `DESIGN.md` §16):**
  - `docs/configuration.md` — canonical env var table
  - `deploy/docker-compose.yml` — annotated, includes Garage container
    for one-stop demo
  - `deploy/unraid/basement.xml` — CA template
  - `deploy/unraid/icon.png` — 64×64 icon
  - `deploy/portainer/template.json` — App Templates v3 spec
  - `deploy/systemd/basement.service` + `docs/install-systemd.md`
- GHCR release

---

## v1.1 — End-user scaffold

- Users + grants tables exposed via admin API
- Admin: Users CRUD screen
- End-user SPA at `/app/*`
- My Buckets screen
- Bucket browser (folder tree, breadcrumbs, search)
- Download (presigned GET via backend)

---

## v1.2 — End-user write

- Upload (presigned PUT, multipart for >100MB, drag-and-drop, progress)
- Share links (creation, listing, revocation, audit)
- `/share/:token` public handler
- File preview (text, image, audio, video where applicable)

---

## v1.3 — OIDC

- `coreos/go-oidc` wiring
- Admin: OIDC config screen
- Identity → local user mapping
- Login screen: "Sign in with SSO" button

---

## M7 — basement driver (parallelizable)

Blocked on basement itself stabilizing. Stub package exists from
**T2.04** equivalent; real impl filed when basement's API exists.

---

## House rules for this backlog

1. Always pop the top unmarked task. If blocked, file an OPEN question
   and pop the next non-blocked one.
2. Tier-1 tasks tend to unblock several Tier-2 tasks. Run them in
   parallel where possible (e.g. T1.01 + T1.03 + T2.01 + T2.05 can all
   dispatch simultaneously — no shared files).
3. Each Tier-2 prompt MUST cite `DESIGN.md` section + relevant T1.*
   output + the anti-fab block from `working_with_freshman.md`.
4. Test coverage is part of the task, not a follow-up.
5. If freshman flags an OPEN question, senior resolves in `DESIGN.md`
   first, then re-dispatches.
