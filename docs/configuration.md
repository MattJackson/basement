# Configuration Reference

Every environment variable basement reads at startup. All variables
are prefixed with `BASEMENT_`. basement has no runtime config —
changes require a process restart.

Source of truth: [`internal/config/config.go`](../internal/config/config.go).

---

## Server

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_LISTEN` | string | No | `:8080` | TCP address to listen on. Format: `[host]:port`. |
| `BASEMENT_DATA_DIR` | string | No | `/var/lib/basement` | Directory for JSON store + audit logs + auto-bootstrap files. |
| `BASEMENT_PUBLIC_URL` | string | No | *(empty)* | Absolute base URL the reverse proxy serves on. Used for share links, OIDC redirect URI, invite emails. Set this in production. |
| `BASEMENT_LOG_LEVEL` | string | No | `info` | One of `debug`, `info`, `warn`, `error`. |
| `BASEMENT_LOG_FORMAT` | string | No | `json` | `json` (one parseable line per event) or `text` (key=value for local dev). v1.11.0f. |
| `BASEMENT_SESSION_TTL` | duration | No | `24h` | Session sliding TTL as a Go duration string (`1h`, `30m`, etc.). |
| `BASEMENT_AUDIT_RETENTION_DAYS` | int | No | `90` | Days to retain audit-log JSONL files. Files older than this are deleted on the daily sweep. |
| `BASEMENT_METRICS_TOKEN` | string | No | *(empty)* | When set, gates `/metrics` behind bearer-token auth. Unset = unauthenticated (standard Prometheus convention; gate via network). v1.11.0f. |

---

## Admin auth (with v1.11.0c auto-bootstrap)

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_ADMIN_USER` | string | No | `admin` (when bootstrap fires) | Admin username. When neither `BASEMENT_ADMIN_PASSWORD_HASH` nor `BASEMENT_ADMIN_PASSWORD` is set, the v1.11.0c bootstrap path defaults the username to `admin`. |
| `BASEMENT_ADMIN_PASSWORD_HASH` | string | No (auto-bootstrap) | — | bcrypt hash of the admin password. Production posture. When unset (and `BASEMENT_ADMIN_PASSWORD` also unset), basement auto-generates a 24-char random password on first boot, prints it to stdout as `INITIAL ADMIN PASSWORD: <pw>`, and persists the plaintext to `{DATA_DIR}/.initial-admin-password` (0600). |
| `BASEMENT_ADMIN_PASSWORD` | string | No | — | Plaintext admin password (v1.11.0c convenience). When set, basement bcrypt-hashes it at boot and never persists the plaintext. Useful for `docker run -e BASEMENT_ADMIN_PASSWORD=...`. Production should use `BASEMENT_ADMIN_PASSWORD_HASH` so no plaintext sits in the env. |
| `BASEMENT_JWT_SECRET` | base64 bytes | No (auto-bootstrap) | — | HMAC-SHA256 secret for session JWTs **and** the AES-256-GCM key-derivation source for encrypting stored S3 credentials + admin tokens at rest. Must decode to ≥32 bytes. When unset, basement auto-generates 32 random bytes on first boot and persists them to `{DATA_DIR}/.jwt-secret` (0600, hex-encoded) so the same secret is reused across restarts. **Set explicitly for production** — a regenerated secret invalidates every session and renders every encrypted credential unreadable. |

See [`deployment/docker.md`](deployment/docker.md#rotating-basement_jwt_secret)
for the JWT-secret rotation procedure.

---

## Drivers

basement supports four drivers; you do not need to pick one at the
env layer. In v1.x, the recommended pattern is:

- **Run basement with auto-bootstrap** (no `BASEMENT_DRIVER`).
- **Add clusters at runtime** via `/admin/clusters` — each cluster
  picks its own driver (`garage` (v2 admin API), `garage-v1`,
  `aws-s3`, `minio`) and supplies its own admin endpoint + token.

The `BASEMENT_DRIVER` family of env vars (below) seeds *one* default
cluster at boot. It exists for zero-touch first-run automation (CI,
k8s bootstrap, immutable infra). Most operators do not need it.

### Driver name

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_DRIVER` | string | No | *(empty)* | Seed one cluster at boot. One of `garage` (v2 admin API), `garage-v1`, `aws-s3`, `minio`. Empty = no env-seeded cluster; add via `/admin/clusters`. |

### Garage v1 / v2 (`BASEMENT_DRIVER=garage` or `garage-v1`)

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_DRIVER_GARAGE_ADMIN_URL` | string | **Yes** (when DRIVER=garage*) | — | Admin API URL (e.g. `http://garage:3903`). |
| `BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN` | string | **Yes** (when DRIVER=garage*) | — | Bearer token for the Garage admin API. Stored AES-256-GCM at rest. |
| `BASEMENT_DRIVER_GARAGE_S3_URL` | string | No | *(empty)* | Optional S3 data-plane URL (e.g. `http://garage:3900`). |
| `BASEMENT_DRIVER_GARAGE_S3_REGION` | string | No | *(empty)* | S3 region name (used in SigV4 signing). |
| `BASEMENT_DRIVER_GARAGE_S3_ACCESS_KEY` | string | No | *(empty)* | Optional default S3 access key. |
| `BASEMENT_DRIVER_GARAGE_S3_SECRET_KEY` | string | No | *(empty)* | Optional default S3 secret key. |

### AWS S3 (`BASEMENT_DRIVER=aws-s3`)

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_DRIVER_AWS_S3_REGION` | string | **Yes** (when DRIVER=aws-s3) | — | AWS region (e.g. `us-east-1`). |
| `BASEMENT_DRIVER_AWS_S3_ACCESS_KEY` | string | **Yes** (when DRIVER=aws-s3) | — | IAM access key ID. |
| `BASEMENT_DRIVER_AWS_S3_SECRET_KEY` | string | **Yes** (when DRIVER=aws-s3) | — | IAM secret access key. Encrypted at rest. |
| `BASEMENT_DRIVER_AWS_S3_ENDPOINT` | string | No | *(empty)* | Override endpoint for S3-compatible non-AWS services. |

### MinIO (`BASEMENT_DRIVER=minio`)

MinIO clusters added via `/admin/clusters` are the typical path
(the wizard prompts for the admin endpoint + access key + secret).
The env-var family is the same shape as the AWS S3 driver.

---

## OIDC (optional, v1.3+)

OIDC is disabled when `BASEMENT_OIDC_ISSUER` is empty. Local-password
sign-in always remains available (for break-glass when OIDC is down).

| Variable | Type | Required? | Default | Description |
|----------|------|-----------|---------|-------------|
| `BASEMENT_OIDC_ISSUER` | string | No | *(empty)* | OIDC issuer URL (e.g. `https://auth.example.com`). Setting this enables OIDC. |
| `BASEMENT_OIDC_CLIENT_ID` | string | Required if Issuer set | *(empty)* | Client ID registered at your IdP. |
| `BASEMENT_OIDC_CLIENT_SECRET` | string | Required if Issuer set | *(empty)* | Client secret from your IdP. |
| `BASEMENT_OIDC_REDIRECT_URL` | string | No (derived from PublicURL) | *(empty)* | OAuth callback URL the IdP redirects to. Derives from `${PublicURL}/api/v1/auth/oidc/callback` when `BASEMENT_PUBLIC_URL` is set. |
| `BASEMENT_OIDC_AUTO_PROVISION` | bool | No | `false` | Auto-create a local user on first OIDC login if no matching username exists. |
| `BASEMENT_OIDC_ELEVATION_PROMPT` | string | No | `login` | The `prompt` parameter the elevation flow appends to the IdP authorize URL. Defaults to `login` so an OIDC user elevating via `/auth/elevate/oidc/start` gets an IdP-side re-auth prompt even if a session is cached. `consent` or `""` are also accepted. |

---

## Generating secrets

### `BASEMENT_ADMIN_PASSWORD_HASH`

Production posture is a bcrypt hash, not a plaintext password.

```bash
# Option A: htpasswd (Apache tools, available everywhere)
htpasswd -bnBC 12 "" 'your-password-here' | tr -d ':\n'

# Option B: Python with bcrypt
python3 -c 'import bcrypt; print(bcrypt.hashpw(b"your-password-here", bcrypt.gensalt(12)).decode())'

# Option C: Go
go run - <<'EOF'
package main
import ("fmt"; "golang.org/x/crypto/bcrypt")
func main() { h, _ := bcrypt.GenerateFromPassword([]byte("your-password-here"), 12); fmt.Println(string(h)) }
EOF
```

Cost-12 is the basement default; lower than 10 is rejected.

> **Shell quoting gotcha.** The hash starts with `$2y$` (or `$2a$`,
> `$2b$`). In `.env` or YAML, wrap the value in single quotes so
> the `$2y` is not interpreted as a shell variable:
> `BASEMENT_ADMIN_PASSWORD_HASH='$2y$12$...'`.

### `BASEMENT_JWT_SECRET`

32 random bytes, base64-encoded.

```bash
openssl rand -base64 32
# Output: <44-char string that decodes to 32 raw bytes>
```

---

## Minimum env for production

```bash
BASEMENT_PUBLIC_URL=https://basement.example.com
BASEMENT_ADMIN_USER=admin
BASEMENT_ADMIN_PASSWORD_HASH='$2y$12$...'
BASEMENT_JWT_SECRET='base64-string-32-bytes-or-more'

# Optional but recommended for production posture
BASEMENT_LOG_LEVEL=info
BASEMENT_LOG_FORMAT=json
BASEMENT_SESSION_TTL=24h
BASEMENT_AUDIT_RETENTION_DAYS=90
```

The driver settings are no longer required at the env layer — add
clusters via `/admin/clusters` after first login. See
[`deployment/docker.md`](deployment/docker.md) for the
production-shape Compose file.

---

## 5-minute evaluation (v1.11.0c, no env at all)

The auto-bootstrap path lets you run basement with zero secrets
configured — useful for kicking the tyres without writing a `.env`:

```bash
docker run -d --name basement -p 8080:8080 \
  -v basement-data:/var/lib/basement \
  ghcr.io/mattjackson/basement:latest

# Wait ~5 seconds, then:
docker logs basement 2>&1 | grep "INITIAL ADMIN PASSWORD"
# INITIAL ADMIN PASSWORD: <24-char string>
```

See [`deployment/docker.md`](deployment/docker.md#5-minute-evaluation-v1110c-auto-bootstrap)
for what auto-bootstrap does, where it persists state, and when to
move past it.

---

## Validation errors

On startup, `basement` aggregates every validation failure and exits
with a single combined error message. Common cases:

- `BASEMENT_DRIVER="foo": supported values are "garage" (v2 admin API), "garage-v1" (v1 admin API), "aws-s3", or "minio"` — unsupported driver name
- `BASEMENT_DRIVER_GARAGE_ADMIN_URL is required when DRIVER=garage` — driver-tier env incomplete
- `BASEMENT_DRIVER_AWS_S3_REGION is required when DRIVER=aws-s3` — same shape for AWS
- `BASEMENT_ADMIN_USER is required` / `BASEMENT_ADMIN_PASSWORD_HASH is required` — admin account incomplete (only when bootstrap is also off — i.e. `BASEMENT_JWT_SECRET` is set explicitly but no password path is configured)
- `BASEMENT_JWT_SECRET must be at least 32 bytes after base64 decoding (got <n>)` — secret too short
- `BASEMENT_LOG_LEVEL="foo": must be one of debug|info|warn|error` — invalid log level
- `BASEMENT_LOG_FORMAT="foo": must be one of json|text` — invalid log format
- `invalid BASEMENT_SESSION_TTL: ...` / `invalid BASEMENT_AUDIT_RETENTION_DAYS: ...` — parse failures

All errors print together so you can fix multiple issues at once.

## See also

- [`deployment/docker.md`](deployment/docker.md) — annotated Compose
  file + 5-minute auto-bootstrap walkthrough + secret generation
- [`deployment/hardening.md`](deployment/hardening.md) — production
  posture checklist
- [`observability/README.md`](observability/README.md) — `/metrics`
  + slog (`BASEMENT_LOG_FORMAT`, `BASEMENT_METRICS_TOKEN`) walkthrough
- [`../SECURITY.md`](../SECURITY.md) — threat model + what basement
  encrypts at rest
