# basement — design

> Status: design locked, pre-code. Drives the freshman backlog at
> `memory/freshman_backlog.md`. Update this doc + the ADR folder before
> deviating; do not deviate inside a freshman prompt.

## 1. What this is

basement is a UI for self-hosted S3-compatible object storage. Today
it manages Garage. Future versions also manage **basement** (the
operator's own storage system) and any successor. The UI is **one
codebase, one binary, multiple drivers, two personas**:

- **Admin** (v1.0 focus) — cluster, layout, buckets, keys, diagnostics
- **End user** (v1.1+) — non-technical: "give my wife a URL to download
  files." Browse buckets they have grants for, upload, download,
  preview, share signed links

The shape that follows is what falls out of those two requirements
combined. None of it is speculative; every layer earns its keep on at
least one named feature.

## 2. Hard constraints

1. **Single static binary**, ~15MB, scratch/distroless image
2. **Multi-arch** linux/amd64 + linux/arm64, published to GHCR
3. **No backend lock-in** — adding a driver = one Go package, no
   frontend changes
4. **Frontend never sees a driver-specific wire format**
5. **End user never gets a long-lived S3 credential** — share links
   route through basement so they can be revoked + audited
6. **OSS-quality** — readable Go stdlib-heavy backend, boring frontend
   stack, low contributor friction
7. **Polish bar:** Linear/Vercel-tier visual quality, dark-mode default

## 3. Stack

**Backend:** Go 1.22+, stdlib-first
- `net/http` + `httputil.ReverseProxy` (no framework)
- `github.com/go-chi/chi/v5` — thin router for middleware chains and
  sub-routes; no transitive deps
- `embed.FS` for SPA assets
- `encoding/json` + atomic write-to-tmp + rename for persistence
  (users, grants, shares); append-only `*.jsonl` for audit log
- `golang.org/x/crypto/bcrypt` for password hashing
- `github.com/golang-jwt/jwt/v5` for session cookies
- `github.com/coreos/go-oidc/v3` for OIDC (v1.3)
- `github.com/aws/aws-sdk-go-v2` for S3 presign (used inside drivers
  that wrap S3-compatible backends)

**Frontend:** single Vite SPA, two route trees
- React 18, TypeScript strict
- Vite 5
- Tailwind + shadcn/ui (components owned in-repo, not a dep)
- TanStack Router (file-based, type-safe, fits SPA + auth-guarded
  routes)
- TanStack Query (server state, optimistic mutations for the layout
  editor's stage/apply/revert)
- React Hook Form + Zod (forms, schema reused for client validation)
- `openapi-typescript` + `openapi-fetch` — generates a fully-typed
  client from basement's own OpenAPI spec

**Build:** pnpm + Vite for frontend, Go for backend, multi-stage
Dockerfile, `docker buildx bake` for multi-arch.

**Why Go + stdlib-first:** the backend is glue. Embedded assets, JWT
auth, reverse proxy, SQLite reads/writes, driver dispatch. Total backend
surface at v1.0 is ~2-3k LOC of Go. A framework would add more than it
removes.

## 4. Architecture

```
┌────────────────────────────────────────────────────────────────┐
│ basement (one Go binary)                                    │
│                                                                 │
│  /*         ┐  (end-user routes; admin redirects to /admin)     │
│  /admin/*   ├─→ embed.FS → React SPA (one bundle, two trees)   │
│  /assets/*  ┘                                                   │
│                                                                 │
│  /api/v1/*  ─→ HTTP handlers (chi)                              │
│                  │                                              │
│                  ├─ Auth middleware (JWT cookie → user ctx)     │
│                  ├─ RBAC middleware (grants check)              │
│                  ├─ Audit middleware (state changes → audit.jsonl)│
│                  │                                              │
│                  ▼                                              │
│              Driver interface ──────────┐                       │
│                  │                       │                       │
│        ┌─────────┼─────────┐             │                       │
│        ▼         ▼         ▼             ▼                       │
│   garage     basement   <future>    JSON store                   │
│   driver     driver                 (users.json, grants.json,    │
│        │         │                   shares.json, audit/*.jsonl) │
│        ▼         ▼                                               │
│   Garage      Basement                                           │
│   admin +     native +                                           │
│   S3 API      S3 API                                             │
└────────────────────────────────────────────────────────────────┘
                  ▲                          ▲
                  │                          │
              admin user                end user
              (cluster +                (signed-URL
               buckets +                 downloads,
               keys + S3)                uploads,
                                         shares)
```

**The contract surface is the OpenAPI spec at `openapi/basement.yaml`.**
Frontend codegen, backend handlers, and freshman prompts all reference
the same spec. Drift between spec and implementation is a freshman-fail
condition that typecheck or `oapi-codegen` will catch.

## 5. The driver interface

```go
type Driver interface {
    // Identity
    Capabilities(ctx context.Context) (Caps, error)
    HealthCheck(ctx context.Context) (HealthReport, error)

    // Cluster
    ListNodes(ctx context.Context) ([]Node, error)
    GetLayout(ctx context.Context) (Layout, error)
    StageLayout(ctx context.Context, change LayoutChange) (LayoutDiff, error)
    ApplyLayout(ctx context.Context) error
    RevertLayout(ctx context.Context) error

    // Buckets
    ListBuckets(ctx context.Context) ([]Bucket, error)
    GetBucket(ctx context.Context, id string) (Bucket, error)
    CreateBucket(ctx context.Context, spec BucketSpec) (Bucket, error)
    UpdateBucket(ctx context.Context, id string, update BucketUpdate) (Bucket, error)
    DeleteBucket(ctx context.Context, id string) error

    // Keys
    ListKeys(ctx context.Context) ([]Key, error)
    GetKey(ctx context.Context, id string) (Key, error)
    CreateKey(ctx context.Context, spec KeySpec) (Key, error)
    UpdateKeyPermissions(ctx context.Context, keyID string, perms []BucketPermission) error
    DeleteKey(ctx context.Context, id string) error

    // S3 data plane (used by both admin object browser and end-user UI)
    ListObjects(ctx context.Context, bucket, prefix, continuation string, limit int) (ObjectPage, error)
    StatObject(ctx context.Context, bucket, key string) (ObjectInfo, error)
    PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (PresignedURL, error)
    PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (PresignedURL, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    CreateMultipart(ctx context.Context, bucket, key, contentType string) (MultipartUpload, error)
    PresignUploadPart(ctx context.Context, upload MultipartUpload, partNum int) (PresignedURL, error)
    CompleteMultipart(ctx context.Context, upload MultipartUpload, parts []CompletedPart) error
    AbortMultipart(ctx context.Context, upload MultipartUpload) error
}

type Caps struct {
    Layout        LayoutCapability   // "stage-apply-revert" | "atomic" | "readonly"
    Quotas        bool
    BucketAliases bool
    KeyModel      KeyModel           // "garage" | "iam" | "none"
    Presign       bool
    Multipart     bool
    Versioning    bool
    Driver        string             // human-readable: "Garage 1.0.1"
}
```

**Unimplemented operations return `driver.ErrUnsupported`**, which the
API layer translates to HTTP 501 + a capability hint. The frontend
queries `/api/v1/capabilities` once at boot and feature-flags
accordingly — buttons aren't disabled, they aren't rendered.

**Driver registry:**

```go
// internal/drivers/registry.go
var drivers = map[string]func(config.DriverConfig) (Driver, error){}

func Register(name string, factory func(config.DriverConfig) (Driver, error)) {
    drivers[name] = factory
}
```

Drivers self-register in their package init. `cmd/basement-server/main.go`
imports `_ "internal/drivers/garage"` etc. Adding a backend = one
package + one blank import.

## 6. The basement API

**Versioning:** `/api/v1/*`. Treated as a public stable contract from
v1.0 onward (operator may want to script against it; third parties may
fork the frontend). Breaking changes go to `/api/v2/`.

**Routes (abridged — see `openapi/basement.yaml` for full):**

```
# Public
GET    /api/v1/health
GET    /api/v1/capabilities       (auth required, but no specific grant)

# Auth
POST   /api/v1/auth/login         { username, password }
POST   /api/v1/auth/logout
GET    /api/v1/auth/me
POST   /api/v1/auth/oidc/start    (v1.3)
GET    /api/v1/auth/oidc/callback (v1.3)

# Admin — cluster
GET    /api/v1/admin/nodes
GET    /api/v1/admin/layout
POST   /api/v1/admin/layout/stage
POST   /api/v1/admin/layout/apply
POST   /api/v1/admin/layout/revert

# Admin — buckets/keys
GET    /api/v1/admin/buckets
POST   /api/v1/admin/buckets
GET    /api/v1/admin/buckets/:id
PATCH  /api/v1/admin/buckets/:id
DELETE /api/v1/admin/buckets/:id
GET    /api/v1/admin/keys
POST   /api/v1/admin/keys
GET    /api/v1/admin/keys/:id
PATCH  /api/v1/admin/keys/:id
DELETE /api/v1/admin/keys/:id

# Admin — users + grants (v1.1+)
GET    /api/v1/admin/users
POST   /api/v1/admin/users
PATCH  /api/v1/admin/users/:id
DELETE /api/v1/admin/users/:id
GET    /api/v1/admin/users/:id/grants
PUT    /api/v1/admin/users/:id/grants

# S3 data plane (RBAC-checked per user grant)
GET    /api/v1/buckets/:bucket/objects?prefix=&continuation=&limit=
GET    /api/v1/buckets/:bucket/objects/{key+}/stat
POST   /api/v1/buckets/:bucket/objects/{key+}/presign-get
POST   /api/v1/buckets/:bucket/objects/{key+}/presign-put
DELETE /api/v1/buckets/:bucket/objects/{key+}
POST   /api/v1/buckets/:bucket/multipart                  { key, contentType }
POST   /api/v1/buckets/:bucket/multipart/:upload/part     { partNumber }
POST   /api/v1/buckets/:bucket/multipart/:upload/complete { parts }
DELETE /api/v1/buckets/:bucket/multipart/:upload

# Sharing (v1.1+)
POST   /api/v1/shares                  { bucket, key, ttl, password? }
GET    /api/v1/shares                  (user's own shares)
DELETE /api/v1/shares/:token           (revoke)
GET    /share/:token                   (public; 302 → presigned URL after check)
```

**Error shape (uniform):**
```json
{ "error": { "code": "DRIVER_UNSUPPORTED", "message": "Layout staging not supported by this driver", "details": {} } }
```

## 7. Auth & RBAC

### v1.0 — single admin

- One admin account; username + bcrypt hash stored in config (env
  `BASEMENT_ADMIN_USER`, `BASEMENT_ADMIN_PASSWORD_HASH`)
- POST `/api/v1/auth/login` issues a JWT, set as
  `__Host-basement_session` cookie: HttpOnly, Secure, SameSite=Strict,
  Path=/
- 24h sliding TTL, refreshed on activity
- Admin has implicit grant on all buckets

### v1.1 — multi-user with grants

- `users.json`: array of `{id, username, password_hash, role,
  oidc_subject?, created_at}`
- `role`: `admin | user`
- `grants.json`: array of `{user_id, bucket, prefix, permissions[]}`
  where `permissions ⊆ {list, read, write, delete, share}`
- Grants are matched longest-prefix-wins on a per-request basis
- Admin manages users + grants in the admin UI; nothing about
  users/grants is delegated to the driver (drivers don't know users
  exist)
- Both files loaded into memory on startup; mutations go through the
  `Store` interface (atomic write-to-tmp + rename under a `sync.RWMutex`)

### v1.3 — OIDC

- `coreos/go-oidc` validates ID tokens
- Identity → local user mapped by email claim (auto-create on first
  login if `BASEMENT_OIDC_AUTO_PROVISION=true`)
- Local password auth remains available as fallback

### Sharing model

- Share creation: user posts `{bucket, key, ttl, password?}` → backend
  generates a random 128-bit token, stores
  `{token, user_id, bucket, key, expires_at, password_hash?,
  revoked_at, last_accessed_at}` in `shares.json`
- Recipient hits `/share/:token` (no auth) → backend checks not-expired
  + not-revoked + password if set → presigns a short-TTL URL from the
  driver → 302 redirect
- Every `/share/:token` access appended to `audit/YYYY-MM-DD.jsonl`
- Owner can revoke from "My Shares" screen; revocation is instant
  (presigned URL is regenerated per access, never handed to recipient)

## 8. Frontend layout

Single SPA, two route trees. End-user experience lives at `/` (root);
admin lives at `/admin`. After login, redirect by role: `admin` →
`/admin`, `user` → `/`. In v1.0 (admin-only), `/` redirects to
`/admin` for all authenticated users. Admin users can navigate to `/`
to preview the end-user experience.

```
frontend/
├── src/
│   ├── shared/
│   │   ├── ui/                     ← shadcn primitives (Button, Input, Dialog, ...)
│   │   ├── api/
│   │   │   ├── client.ts           ← openapi-fetch instance
│   │   │   └── types.gen.ts        ← openapi-typescript output (gitignored)
│   │   ├── auth/
│   │   │   ├── useUser.ts
│   │   │   ├── LoginForm.tsx
│   │   │   └── ProtectedRoute.tsx
│   │   ├── theme/
│   │   │   └── tailwind.css        ← dark default, light opt-in
│   │   └── hooks/
│   ├── admin/                      ← mounted at /admin/*
│   │   ├── routes/
│   │   │   ├── __root.tsx
│   │   │   ├── index.tsx           ← Dashboard
│   │   │   ├── cluster/
│   │   │   │   ├── nodes.tsx
│   │   │   │   └── layout.tsx      ← drag-and-drop editor
│   │   │   ├── buckets/
│   │   │   │   ├── index.tsx
│   │   │   │   └── $id.tsx
│   │   │   ├── keys/
│   │   │   │   ├── index.tsx
│   │   │   │   └── $id.tsx
│   │   │   ├── users/              (v1.1+)
│   │   │   └── diagnostics.tsx
│   │   └── components/
│   ├── app/                        ← mounted at / (v1.1+)
│   │   ├── routes/
│   │   │   ├── __root.tsx
│   │   │   ├── index.tsx           ← My Buckets
│   │   │   ├── browse/
│   │   │   │   └── $bucket/$.tsx   ← splat for nested prefix
│   │   │   ├── shares.tsx
│   │   │   └── account.tsx
│   │   └── components/
│   └── main.tsx                    ← route-tree selector by path prefix
├── vite.config.ts
├── tailwind.config.ts
├── tsconfig.json
└── package.json
```

## 9. Backend layout

```
basement/
├── cmd/basement-server/main.go
├── internal/
│   ├── api/
│   │   ├── server.go               ← route registration
│   │   ├── auth.go                 ← /auth/* handlers
│   │   ├── admin_cluster.go
│   │   ├── admin_buckets.go
│   │   ├── admin_keys.go
│   │   ├── admin_users.go
│   │   ├── data_plane.go           ← /buckets/:bucket/objects/*
│   │   ├── shares.go
│   │   └── errors.go               ← uniform error shape
│   ├── auth/
│   │   ├── bcrypt.go
│   │   ├── jwt.go
│   │   ├── session.go
│   │   ├── rbac.go                 ← grant matching
│   │   └── middleware.go
│   ├── audit/
│   │   └── audit.go                ← write-through to SQLite
│   ├── config/
│   │   └── config.go               ← env + optional toml
│   ├── driver/
│   │   ├── driver.go               ← interface + types
│   │   ├── errors.go               ← ErrUnsupported, ErrNotFound, ...
│   │   └── registry.go
│   ├── drivers/
│   │   ├── garage/
│   │   │   ├── driver.go
│   │   │   ├── cluster.go
│   │   │   ├── buckets.go
│   │   │   ├── keys.go
│   │   │   └── s3.go               ← presign via aws-sdk-go-v2
│   │   └── basement/               (M7)
│   ├── store/
│   │   ├── store.go                ← Store interface + file paths + RWMutex
│   │   ├── json.go                 ← atomic write-to-tmp + rename helper
│   │   ├── users.go                ← load/save users.json
│   │   ├── grants.go               ← load/save grants.json
│   │   ├── shares.go               ← load/save shares.json
│   │   └── audit.go                ← append to audit/YYYY-MM-DD.jsonl
│   └── web/
│       ├── embed.go                ← //go:embed dist
│       └── dist/                   ← built SPA (gitignored)
├── frontend/                       ← (above)
├── openapi/basement.yaml
├── docker/
│   ├── Dockerfile
│   └── docker-bake.hcl
├── docs/
│   ├── DESIGN.md                   ← this doc
│   └── ADR/
├── memory/                         ← freshman workflow
│   ├── freshman_backlog.md
│   └── freshman_grades.md
├── prompts/                        ← freshman prompts (per session)
├── .github/workflows/
│   ├── ci.yml
│   └── release.yml
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

## 10. Configuration

Two layers — env vars for deploy-time, SQLite for runtime mutable
state. No TOML config file at v1.0; revisit at v1.x if env-only
becomes painful for OIDC issuer URLs etc.

```
# Required
BASEMENT_DRIVER=garage
BASEMENT_DRIVER_GARAGE_ADMIN_URL=http://garage:3903
BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN=...
BASEMENT_DRIVER_GARAGE_S3_URL=http://garage:3900
BASEMENT_DRIVER_GARAGE_S3_REGION=garage
BASEMENT_DRIVER_GARAGE_S3_ACCESS_KEY=...
BASEMENT_DRIVER_GARAGE_S3_SECRET_KEY=...
BASEMENT_ADMIN_USER=admin
BASEMENT_ADMIN_PASSWORD_HASH=$2a$12$...
BASEMENT_JWT_SECRET=<32+ random bytes, base64>

# Optional
BASEMENT_LISTEN=:8080
BASEMENT_DATA_DIR=/var/lib/basement     (JSON store + audit logs)
BASEMENT_PUBLIC_URL=https://admin.example.com   (used in share links)
BASEMENT_LOG_LEVEL=info
BASEMENT_SESSION_TTL=24h
BASEMENT_OIDC_ISSUER=...                   (v1.3)
BASEMENT_OIDC_CLIENT_ID=...
BASEMENT_OIDC_CLIENT_SECRET=...
BASEMENT_OIDC_AUTO_PROVISION=false
```

## 11. Build & ship

**Dockerfile (multi-stage):**

```dockerfile
# Stage 1 — frontend
FROM node:22-alpine AS frontend
WORKDIR /src
RUN corepack enable
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# Stage 2 — backend (with embedded frontend)
FROM golang:1.22-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/dist ./internal/web/dist
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -trimpath -o /out/basement ./cmd/basement-server

# Stage 3 — runtime
FROM gcr.io/distroless/static:nonroot
COPY --from=backend /out/basement /basement
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/basement"]
```

**CI (GitHub Actions):**
- `ci.yml` on PR: golangci-lint, go test, eslint, tsc, vitest
- `release.yml` on tag `v*`: `docker buildx bake` for amd64+arm64, push
  to `ghcr.io/mattjackson/basement:vX.Y.Z` and `:latest`

## 12. Milestones

| Tag  | Theme               | Scope                                                                                       |
|------|---------------------|---------------------------------------------------------------------------------------------|
| M0   | Foundations         | OpenAPI v1 draft, driver iface, Go server skeleton + embed, frontend scaffold, Docker, CI   |
| v0.1 | Admin read          | Garage driver: read paths. Login, Dashboard, Nodes, Buckets list, Keys list                 |
| v0.2 | Admin write         | CRUD for buckets/keys, layout stage/apply/revert with drag-and-drop + diff preview          |
| v1.0 | Admin polish        | Diagnostics screen, bucket object browser, empty states, a11y, GHCR release                 |
| v1.1 | End-user scaffold   | Users + grants tables. Admin users CRUD. End-user SPA: My Buckets, browse, download         |
| v1.2 | End-user write      | Upload (presigned PUT + multipart >100MB). Share links (server-routed, revocable, audited)  |
| v1.3 | OIDC                | `coreos/go-oidc`, OIDC config screen, identity → user mapping                               |
| M7   | Basement driver     | Parallelizable from M0. Implements interface against basement's API; caps declared honestly |

## 13. Decisions worth calling out (mini-ADRs)

1. **Single SPA over two SPAs.** End-user bundle is only ~30% of admin
   anyway after tree-shaking; tooling complexity of two Vite entries
   isn't earned.
2. **JSON files, not SQLite.** Persisted state is small (<1k users,
   <1k grants, low-thousands shares); audit is append-only JSONL.
   `encoding/json` + atomic write-to-tmp + rename + in-memory cache
   covers it. Operator can `cat`/`jq`/hand-edit. Reconsider only when
   audit queries get painful or a feature actually needs relational
   reads — `Store` is an interface so the swap is local.
3. **No backend framework.** stdlib `net/http` + a thin router (likely
   `chi`) is plenty for ~30 routes.
4. **Shares route through `/share/:token`, not raw presigned URLs.**
   Buys revocation, audit, optional passwords. Cost: backend in the
   request path (but not the data path — bytes still go user↔Garage
   direct via 302).
5. **OpenAPI is the source of truth.** Backend handlers and frontend
   types are both generated/checked against it. Fabricated routes
   fail typecheck.
6. **Capabilities returned, not assumed.** Every driver declares what
   it can do; frontend renders accordingly. Adding a backend with a
   subset of features doesn't require frontend conditionals beyond the
   cap check.
7. **No object browsing parity with S3 console for v1.0.** Admin
   object browser exists (it has to, for diagnostics + bucket detail),
   but the polished end-user browse experience is v1.1.

## 14. Risks & open questions

- **Q1 — Garage admin API surface.** First freshman task is a Tier-1
  RE writeup: every Garage v1 admin endpoint, request/response shape,
  cross-referenced to driver methods. This is the doc that the spec
  and the driver impl both reference. Without it, the driver risks
  fabrication.
- **Q2 — OpenAPI churn during v0.x.** Lock v1 contract only at v1.0
  tag. Pre-1.0 the spec is allowed to break; codegen catches it on
  both sides.
- **Q3 — Layout editor UX.** Garage's layout model (zone/capacity/tags
  per node) needs a UI that's honest about staging semantics. Likely
  needs a designer pass before M2.
- **Q3b — Audit log retention.** Daily JSONL rotation; default retain
  90 days, configurable via `BASEMENT_AUDIT_RETENTION_DAYS`. Cleanup
  runs at startup + once/day. Reconsider at v1.x if operators want
  long-tail queries (would justify SQLite for the audit table only).
- **Q4 — Object listing for huge prefixes.** Keyset pagination via
  TanStack Query infinite query + react-virtual for the row list. Tested
  against a bucket with 100k objects before v1.0.
- **Q5 — Share-link abuse.** Rate-limit `/share/:token` and `/api/v1/auth/login`
  with a token bucket per IP. Configurable. Default: 60/min.
- **Q6 — basement-internal sibling repo.** Sibling repo
  `~/Developer/basement-internal` exists; purpose TBD. If it
  contains private drivers or auth config, design here must stay
  decoupled. Confirm with operator before referencing.

## 15. Senior vs. freshman split

**Senior (operator/me) owns:**
- This design doc + ADRs
- OpenAPI v1 spec final review
- Driver interface signature
- RBAC + sharing model
- Cross-cutting refactors
- Strategic scope / sequencing calls

**Freshman owns:**
- Tier-1: Garage admin API RE writeup; OpenAPI v1 draft from this doc;
  Garage layout-editor UX scoping
- Tier-2: every driver method against the API spec; every API handler
  against the OpenAPI; every React screen against a per-screen spec;
  every unit test; Docker + CI YAML; README updates

Per the freshman guide: senior writes the prompt + audits (~5-15 min
total), freshman runs in background. Target: keep the freshman queue at
zero idle wall-time. Initial backlog at `memory/freshman_backlog.md`.

## 16. Deployment methods

Single binary inside a single image, but consumable several ways.
Operator audience: home-server / self-hoster running Garage + a NAS
OS. Target methods, in priority order:

1. **Docker image (canonical).** `docker run` with env vars + a
   single volume for `BASEMENT_DATA_DIR`. Published to GHCR
   multi-arch. See `docker/Dockerfile`.

2. **docker-compose.** `deploy/docker-compose.yml` — annotated
   example showing env vars, named volume, restart policy, and
   network attachment to a Garage container. Most homelab users
   start here.

3. **Unraid Community Apps template.** XML template at
   `deploy/unraid/basement.xml` conforming to the CA spec
   (https://github.com/Squidly271/AppFeed). Container path mappings:
   data dir → `/mnt/user/appdata/basement`, port → user-mapped.
   Each `BASEMENT_*` env var declared with description + default +
   "advanced" flag for the seldom-changed ones. Icon at
   `deploy/unraid/icon.png` (64×64 PNG). Submission to the CA store
   is a v1.x follow-up (PR to a community feed repo, out of scope
   for v1.0).

4. **Portainer stack template.** `deploy/portainer/template.json`
   conforming to Portainer's App Templates v3 spec. Lets users with
   Portainer (the operator already runs it per global CLAUDE.md)
   deploy with a form UI.

5. **Bare Linux + systemd.** `deploy/systemd/basement.service`
   unit file with `User=basement`, `StateDirectory=basement`
   (which becomes `/var/lib/basement`), env file at
   `/etc/basement/basement.env`. Install doc:
   `docs/install-systemd.md`.

Lower-priority methods (v1.x+ if asked): Synology Container Manager,
TrueNAS SCALE app, CasaOS template, k8s Helm chart. None planned for
v1.0; design must not preclude them, which it doesn't (one binary +
env vars + one volume is universally deployable).

**Contract for all methods:** identical env var names, identical
data-dir layout, identical port. The only thing that differs
between methods is the wrapping (compose YAML vs Unraid XML vs
systemd unit). One canonical config table at `docs/configuration.md`
is the source of truth that all method docs reference.

## 17. Anti-fabrication discipline for this project

Beyond the standard anti-fab block in every prompt, this project has
two specific traps:

1. **Garage API drift.** Don't assume any endpoint exists without
   citing it in the Garage v1 source (cloned at
   `~/Developer/garage/`). Pre-1.0 versions of the community admin UI
   used paths that no longer exist — that's the whole reason this
   project exists.
2. **basement API doesn't exist yet.** The basement driver in M7 is
   placeholder until basement itself stabilizes. Don't let a freshman
   "implement" it from imagined endpoints. Declare the package, leave
   methods returning `driver.ErrUnsupported`, ship.
