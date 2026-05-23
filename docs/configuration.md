# Configuration Reference

This document lists every environment variable supported by `basement`. All variables are prefixed with `BASEMENT_` and read at startup. No runtime configuration — changes require a restart.

See [`internal/config/config.go`](../internal/config/config.go) for the source of truth (lines cited in each section).

---

## Server Configuration

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_LISTEN` | string | No | `:8080` | TCP address to listen on. Format: `[host]:port`. See [`config.go:80`](../internal/config/config.go) |
| `BASEMENT_DATA_DIR` | string | No | `/var/lib/basement` | Directory for JSON store (users, grants, shares) and audit logs. See [`config.go:16-17`](../internal/config/config.go) |
| `BASEMENT_PUBLIC_URL` | string | No | *(empty)* | Absolute base URL for share links. If set, used to generate absolute URLs in `/share/:token` redirects. See [`config.go:17-18`](../internal/config/config.go) |
| `BASEMENT_LOG_LEVEL` | string | No | `info` | One of `debug`, `info`, `warn`, `error`. Validates at load time. See [`config.go:18`](../internal/config/config.go), [`config.go:182-185`](../internal/config/config.go) |
| `BASEMENT_SESSION_TTL` | duration | No | `24h` | Session sliding TTL as Go duration string (e.g., `1h`, `30m`). Parsed at startup. See [`config.go:19`](../internal/config/config.go), [`config.go:86-93`](../internal/config/config.go) |
| `BASEMENT_AUDIT_RETENTION_DAYS` | int | No | `90` | Days to retain audit logs before cleanup. Parsed as integer at startup. See [`config.go:20`](../internal/config/config.go), [`config.go:96-103`](../internal/config/config.go) |

---

## Driver Configuration

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_DRIVER` | string | **Yes** | — | Backend driver name. Only `"garage"` supported in v1.0. See [`config.go:30`](../internal/config/config.go), [`config.go:106`](../internal/config/config.go), [`config.go:149-152`](../internal/config/config.go) |
| `BASEMENT_DRIVER_GARAGE_ADMIN_URL` | string | **Yes** (if driver=garage) | — | Admin API URL of Garage cluster (e.g., `http://garage:3903`). See [`config.go:37`](../internal/config/config.go), [`config.go:109`](../internal/config/config.go), [`config.go:156-158`](../internal/config/config.go) |
| `BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN` | string | **Yes** (if driver=garage) | — | Bearer token for Garage admin API. Never exposed to browser; stored server-side. See [`config.go:38`](../internal/config/config.go), [`config.go:110`](../internal/config/config.go), [`config.go:157-159`](../internal/config/config.go) |
| `BASEMENT_DRIVER_GARAGE_S3_URL` | string | No | *(empty)* | S3 data-plane URL (e.g., `http://garage:3900`). Optional if only admin operations needed. See [`config.go:39`](../internal/config/config.go), [`config.go:111`](../internal/config/config.go) |
| `BASEMENT_DRIVER_GARAGE_S3_REGION` | string | No | *(empty)* | S3 region name (e.g., `garage`). Used in SigV4 signing. See [`config.go:40`](../internal/config/config.go), [`config.go:112`](../internal/config/config.go) |
| `BASEMENT_DRIVER_GARAGE_S3_ACCESS_KEY` | string | No | *(empty)* | S3 access key for data-plane operations (buckets, objects). See [`config.go:41`](../internal/config/config.go), [`config.go:113`](../internal/config/config.go) |
| `BASEMENT_DRIVER_GARAGE_S3_SECRET_KEY` | string | No | *(empty)* | S3 secret key for data-plane operations. See [`config.go:42`](../internal/config/config.go), [`config.go:114`](../internal/config/config.go) |

---

## Admin Authentication (v1.0)

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_ADMIN_USER` | string | No (bootstrap defaults to `admin`) | `admin` (when bootstrap fires) | Admin username. Single admin account in v1.0; multi-user planned for v1.1+. When `BASEMENT_ADMIN_PASSWORD_HASH` is also unset, the v1.11.0c bootstrap path defaults the username to `admin`. |
| `BASEMENT_ADMIN_PASSWORD_HASH` | string | No (auto-bootstrap) | — | bcrypt hash of admin password. Generate with `bcrypt-cli` or Node's `bcrypt`. **v1.11.0c**: when unset (and `BASEMENT_ADMIN_PASSWORD` also unset), basement auto-generates a random password on first boot, prints it to stdout as `INITIAL ADMIN PASSWORD: <pw>`, and persists the plaintext to `{DATA_DIR}/.initial-admin-password` (0600). |
| `BASEMENT_ADMIN_PASSWORD` | string | No | — | **v1.11.0c convenience**: plaintext admin password. When set and `BASEMENT_ADMIN_PASSWORD_HASH` is unset, basement bcrypt-hashes it at boot and never persists the plaintext. Useful for `docker run -e BASEMENT_ADMIN_PASSWORD=...` first-boot. Production should set `BASEMENT_ADMIN_PASSWORD_HASH` directly so no plaintext sits in the env. |
| `BASEMENT_JWT_SECRET` | base64 bytes | No (auto-bootstrap) | — | Secret for signing HS256 JWTs. Must be ≥32 bytes after decoding (base64 in env). **v1.11.0c**: when unset, basement auto-generates 32 random bytes on first boot and persists them to `{DATA_DIR}/.jwt-secret` (0600, hex-encoded) so the same secret is reused across container restarts and existing sessions survive. Set explicitly for production so a fresh data volume keeps the same signing key. |

---

## OIDC Configuration (v1.3+)

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_OIDC_ISSUER` | string | No | *(empty)* | OIDC issuer URL (e.g., `https://auth.example.com`). Optional; only used if v1.3+ enabled. See [`config.go:58`](../internal/config/config.go), [`config.go:131`](../internal/config/config.go) |
| `BASEMENT_OIDC_CLIENT_ID` | string | No | *(empty)* | OIDC client ID from issuer registration. See [`config.go:59`](../internal/config/config.go), [`config.go:132`](../internal/config/config.go) |
| `BASEMENT_OIDC_CLIENT_SECRET` | string | No | *(empty)* | OIDC client secret from issuer registration. See [`config.go:60`](../internal/config/config.go), [`config.go:133`](../internal/config/config.go) |
| `BASEMENT_OIDC_AUTO_PROVISION` | bool | No | `false` | Auto-create local user on first OIDC login if no matching username exists. See [`config.go:61`](../internal/config/config.go), [`config.go:136-143`](../internal/config/config.go) |

---

## Generating bcrypt + JWT secret

### bcrypt password hash (admin account)

**Option A — `bcrypt-cli` CLI:**
```bash
# Generate 12-cost hash (default for basement)
echo "your-password" | bcrypt -g 12
# Output: $2a$12$...
```

**Option B — Node.js one-liner:**
```bash
node -e 'const bcrypt = require("bcrypt"); bcrypt.hashSync("your-password", 12)'
# Output: $2a$12$...
```

**Option C — Go (if you have Go toolchain):**
```bash
go run - <<'EOF'
package main
import ("fmt"; "golang.org/x/crypto/bcrypt")
func main() { h, _ := bcrypt.GenerateFromPassword([]byte("your-password"), bcrypt.DefaultCost); fmt.Println(string(h)) }
EOF
# Output: $2a$12$...
```

### JWT secret (base64-encoded)

**Option A — `openssl` (macOS / Linux):**
```bash
# Generate 32 random bytes, base64 encode for env var
openssl rand -base64 32
# Output: <96-char string>
```

**Option B — `node` one-liner:**
```bash
node -e 'console.log(require("crypto").randomBytes(32).toString("base64"))'
# Output: <96-char string>
```

**Option C — Python (if available):**
```bash
python3 -c 'import base64, os; print(base64.b64encode(os.urandom(32)).decode())'
# Output: <96-char string>
```

**Important:** The JWT secret must be ≥32 bytes **after** base64 decoding. A 96-character base64 string decodes to exactly 32 bytes (minimum). Use the full output — do not truncate.

---

## Quick reference (minimum for v1.0)

```bash
BASEMENT_DRIVER=garage
BASEMENT_DRIVER_GARAGE_ADMIN_URL=http://garage:3903
BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN=<your-garage-admin-token>
BASEMENT_ADMIN_USER=admin
BASEMENT_ADMIN_PASSWORD_HASH=$2a$12$...
BASEMENT_JWT_SECRET=$(openssl rand -base64 32)
```

## 5-minute evaluation (v1.11.0c, no env vars at all)

The auto-bootstrap path lets you run basement with **zero** secrets
configured — useful for kicking the tyres or smoke-testing a new
image tag without writing a `.env` first:

```bash
docker run -d --name basement -p 8080:8080 \
  -v basement-data:/var/lib/basement \
  ghcr.io/mattjackson/basement:latest

# Wait ~5 seconds, then:
docker logs basement 2>&1 | grep "INITIAL ADMIN PASSWORD"
```

The driver settings (`BASEMENT_DRIVER`, `BASEMENT_DRIVER_GARAGE_*` or
`BASEMENT_DRIVER_AWS_S3_*`) are still required when you want basement
to manage a specific backend at boot. Without them, basement comes up
with no default cluster and you add one via `/admin/clusters` after
first login. See
[`docs/deployment/docker.md`](deployment/docker.md) for the
auto-bootstrap behaviour and the production checklist.

---

## Validation errors

On startup, `basement` aggregates all validation failures and exits with a single error message. Common cases:

- `BASEMENT_DRIVER is required` — driver name must be set
- `BASEMENT_DRIVER="foo": only "garage" supported in v1.0` — unsupported driver value
- `BASEMENT_ADMIN_USER is required` / `BASEMENT_ADMIN_PASSWORD_HASH is required` — admin account incomplete
- `BASEMENT_JWT_SECRET is required` — missing secret entirely
- `BASEMENT_JWT_SECRET must be at least 32 bytes after base64 decoding (got <n>)` — secret too short
- `BASEMENT_LOG_LEVEL="foo": must be one of debug|info|warn|error` — invalid log level
- `invalid BASEMENT_SESSION_TTL: ...` / `invalid BASEMENT_AUDIT_RETENTION_DAYS: ...` — parse failures

All errors are printed together so you can fix multiple issues at once.
