# Docker single-instance deployment

The canonical recipe: one `basement` container, persistent data on a
named volume, configured by environment variables, fronted by a
reverse proxy that terminates TLS. This page walks the Compose file
line by line so you can adapt it without guessing.

For TLS topologies, see [`tls.md`](tls.md). For the reverse-proxy
Caddyfile / Nginx / Traefik recipes, see
[`reverse-proxy.md`](reverse-proxy.md).

## 5-minute evaluation (v1.11.0c auto-bootstrap)

The shortest path to a working basement. No env vars, no bcrypt CLI,
no JWT secret to generate up front:

```bash
docker run -d --name basement -p 8080:8080 \
  -v basement-data:/var/lib/basement \
  ghcr.io/mattjackson/basement:latest

# Wait ~5 seconds, then read the auto-generated admin password:
docker logs basement 2>&1 | grep "INITIAL ADMIN PASSWORD"
# INITIAL ADMIN PASSWORD: <24-char string>

# Open http://localhost:8080 and log in as admin / <password>.
```

### What auto-bootstrap does on first boot

When `BASEMENT_JWT_SECRET`, `BASEMENT_ADMIN_PASSWORD_HASH`, and
`BASEMENT_ADMIN_PASSWORD` are all unset, basement fills in defaults
under the data directory (`/var/lib/basement` inside the container,
backed by the `basement-data` volume above):

| File | Mode | Purpose |
|------|------|---------|
| `.jwt-secret` | 0600 | 32 random bytes (hex-encoded) used to sign JWT cookies. Reused on every restart so existing sessions survive. |
| `.initial-admin-password` | 0600 | The plaintext of the password printed on first boot. Lets you recover it after the log line scrolls off; safe to delete once you change the password via `/admin/users`. |

`BASEMENT_ADMIN_USER` defaults to `admin` when bootstrap fires.
Bootstrap is fully idempotent — restarting the container reuses the
same JWT secret and the same admin password (so sessions survive and
the password you wrote down still works).

### One-liner installer

`scripts/install.sh` wraps the `docker run` above with Docker
detection, image pull, compose-file generation, log tailing, and a
final banner that prints the auto-generated password:

```bash
curl -sSL https://raw.githubusercontent.com/MattJackson/basement/main/scripts/install.sh | bash
```

For review-before-run:

```bash
curl -sSLo install.sh https://raw.githubusercontent.com/MattJackson/basement/main/scripts/install.sh
less install.sh
bash install.sh
```

### Convenience: supply a plaintext password without bcrypt

If you'd rather pick the admin password than read one out of the logs,
set `BASEMENT_ADMIN_PASSWORD` (plaintext). basement bcrypt-hashes it
at boot and never persists the plaintext to disk:

```bash
docker run -d --name basement -p 8080:8080 \
  -v basement-data:/var/lib/basement \
  -e BASEMENT_ADMIN_PASSWORD=changeme \
  ghcr.io/mattjackson/basement:latest
```

The JWT secret still auto-generates in this posture; supply
`BASEMENT_JWT_SECRET` explicitly to take it over for production.

### When to move past auto-bootstrap

Auto-bootstrap is fine for evaluation and small single-operator
installs. For production posture (explicit secrets, no plaintext on
disk, reverse-proxied TLS, backed-up data dir, image-tag pinned) skip
to the [Annotated docker-compose.yml](#annotated-docker-composeyml)
section below and the
[`hardening.md`](hardening.md) checklist.

## Image

```
ghcr.io/mattjackson/basement:latest
```

Pin a release tag in production (e.g. `:v1.11.0`) instead of
`:latest`. See [`upgrade.md`](upgrade.md) for the pull-and-restart
procedure.

The image is built `FROM scratch` with the binary, the CA
certificates bundle, and a passwd file with UID/GID 65532. There is
no shell, no package manager, no init system. The entrypoint is the
single static `basement` binary listening on port 8080.

## Annotated docker-compose.yml

The Compose file below is the production-shape baseline. It is what
ships in [`deploy/docker-compose.yml`](../../deploy/docker-compose.yml)
with the Caddy reverse proxy bundled. Reading top to bottom:

```yaml
services:
  basement:
    # Pin a release tag in production. ":latest" is fine for
    # Watchtower-managed installs (see upgrade.md).
    image: ghcr.io/mattjackson/basement:v1.11.0

    container_name: basement

    # restart: unless-stopped survives daemon restarts and reboots
    # without coming back from a `docker stop`. Better than `always`,
    # which fights with manual `docker stop`.
    restart: unless-stopped

    # Don't publish 8080 to the host. The reverse proxy reaches
    # basement over the internal Docker network using `basement:8080`.
    # This is the primary network-hardening step — see hardening.md.
    # If you must expose basement directly (no proxy), bind to
    # 127.0.0.1 only: `- "127.0.0.1:8080:8080"`.

    # Pull config from a file outside the Compose file. Keep
    # docker-compose.yml in version control, keep .env out (it has
    # the JWT secret + admin password hash + driver creds).
    env_file:
      - .env

    volumes:
      # Named volume for basement's own state. See "Volume layout"
      # below for what lives in here, and backup-basement.md for how
      # to back it up.
      - basement-data:/var/lib/basement

    # Run as the image's default UID 65532. Don't override unless
    # you have a specific host-uid mapping requirement; see
    # hardening.md for the trade-off.

  caddy:
    image: caddy:2
    container_name: basement-caddy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    depends_on:
      - basement

volumes:
  basement-data:
  caddy-data:
  caddy-config:
```

The Caddy service is optional — drop it if you already have a
reverse proxy on the host and want to point it at the `basement`
container over the Docker network or a published port.

## Environment variables

All configuration is environment-driven. The full reference is in
[`../configuration.md`](../configuration.md). The minimum required
for a production deploy:

```bash
# --- Required ---
BASEMENT_ADMIN_USER=admin
BASEMENT_ADMIN_PASSWORD_HASH='$2y$12$...'   # see "Generating secrets"
BASEMENT_JWT_SECRET='base64-string-32-bytes-or-more'

# --- Strongly recommended ---
BASEMENT_PUBLIC_URL=https://basement.example.com
BASEMENT_LOG_LEVEL=info
BASEMENT_SESSION_TTL=24h
BASEMENT_AUDIT_RETENTION_DAYS=90

# --- Optional: seed a default cluster at boot ---
# Most operators add clusters via the UI; these env vars are for
# zero-touch first-run automation (CI / k8s bootstrap).
# BASEMENT_DRIVER_GARAGE_ADMIN_URL=http://garage:3903
# BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN=...
```

Put these in a `.env` file alongside `docker-compose.yml`. Set the
file mode to `600` (owner-readable only) and keep it out of version
control.

## Generating secrets

### `BASEMENT_ADMIN_PASSWORD_HASH`

basement stores the admin password as a bcrypt hash. Never put a
plaintext password in the env. Use any of the following to generate
the hash; all produce the same `$2y$12$...` shape that basement
accepts.

**Option A — `htpasswd` (Apache tools, available everywhere):**

```bash
htpasswd -bnBC 12 "" 'your-password-here' | tr -d ':\n'
```

This prints the bare bcrypt hash with no username prefix and no
trailing newline. Paste it into `BASEMENT_ADMIN_PASSWORD_HASH=`
exactly as printed.

**Option B — `python3 -c` (Python with `bcrypt` installed):**

```bash
python3 -c 'import bcrypt; print(bcrypt.hashpw(b"your-password-here", bcrypt.gensalt(12)).decode())'
```

**Option C — Go one-liner (if you have the Go toolchain):**

```bash
go run - <<'EOF'
package main
import ("fmt"; "golang.org/x/crypto/bcrypt")
func main() { h, _ := bcrypt.GenerateFromPassword([]byte("your-password-here"), 12); fmt.Println(string(h)) }
EOF
```

Cost-12 is the basement default. Higher cost (13, 14) is fine; lower
than 10 is rejected.

> **Important: shell quoting.** The hash starts with `$2y$` (or
> `$2a$`, `$2b$` — all valid bcrypt). In a `.env` file or a YAML
> environment block, wrap the value in single quotes so `$2y` is not
> interpreted as a shell variable: `BASEMENT_ADMIN_PASSWORD_HASH='$2y$12$...'`.

### `BASEMENT_JWT_SECRET`

This is the HS256 signing secret for session JWTs and the AES-GCM
key derivation source for at-rest encryption of stored driver
credentials. It must decode to at least 32 bytes.

```bash
openssl rand -base64 32
```

Paste the output directly into `BASEMENT_JWT_SECRET=`. The string is
~44 characters; basement base64-decodes it to 32 raw bytes.

### Rotating `BASEMENT_JWT_SECRET`

The JWT secret is **load-bearing for two things**:

1. **Active session JWTs.** Rotating the secret invalidates every
   logged-in session immediately. Users will be bounced to the
   login page on their next request. This is the intended behaviour
   for "I think a secret leaked, kick everyone out."

2. **At-rest encryption of stored driver credentials.** The
   per-user S3 credentials in `user_regions.json`, the per-cluster
   admin tokens in `connections.json`, and the service-account
   secrets in `service_accounts.json` are AES-GCM-encrypted with a
   key derived from `BASEMENT_JWT_SECRET`. **Rotating the secret
   without a migration step renders these unreadable.**

The safe rotation procedure:

1. Sign in as admin.
2. Note every cluster connection (the admin token can be re-pasted)
   and every service account (re-mint after rotation; secrets are
   shown-once and not recoverable).
3. Stop basement.
4. Back up `BASEMENT_DATA_DIR` (see
   [`backup-basement.md`](backup-basement.md)).
5. Replace the secret in `.env`.
6. Start basement.
7. Re-paste each cluster's admin token in the UI (this re-encrypts
   under the new key).
8. Re-mint any service accounts your integrations use; update the
   downstream config files.

> A future basement release may ship an in-place rekey command that
> reads the old + new secret and re-encrypts on disk. As of v1.11
> the procedure above is the supported path.

## Volume layout

basement persists everything it owns under `BASEMENT_DATA_DIR`
(default `/var/lib/basement` inside the container, mapped to the
`basement-data` Docker volume).

```
/var/lib/basement/
  users.json                 # local accounts, OIDC-provisioned users
  user_regions.json          # per-user S3 credentials (AES-GCM encrypted)
  connections.json           # per-cluster admin connections (admin tokens encrypted)
  bucket_grants.json         # per-bucket grants (legacy, retained for migration)
  invites.json               # pending invite tokens
  shares.json                # public share tokens
  oidc_group_mappings.json   # OIDC group -> role auto-mappings
  org_capabilities.json      # org-wide settings (elevation TTL, gateway toggles)
  service_accounts.json      # M2M bearer credentials (secrets hashed; AKID visible)
  webhooks.json              # bucket-event webhook subscriptions + secrets
  federated_buckets.json     # multi-backend mirrored bucket records
  backups.json               # scheduled bucket-to-bucket backup jobs
  audit/
    2026-05-20.log           # one JSONL file per day; append-only
    2026-05-21.log
    ...
```

This is the entire state surface. Backing up the directory atomically
captures everything basement knows about; restoring it brings the
instance back to that point in time. See
[`backup-basement.md`](backup-basement.md) for the recommended
procedure.

> **What's NOT in here.** Bucket contents live in the backend
> (Garage / MinIO / AWS S3), not in basement. basement is a control
> plane; the data plane is the S3 backend. Backing up basement does
> NOT back up your objects — for that, use the bucket-to-bucket
> backup feature at `/files/backups` (v1.5+), which writes
> snapshots to another bucket on a schedule.

## First run

After `docker compose up -d`:

1. Wait ~5s for the container to come up. Check `docker compose logs
   basement` for the `serving on :8080` line.
2. Open the public URL in a browser (e.g. `https://basement.example.com`).
3. Sign in with the admin username + password you set in
   `BASEMENT_ADMIN_USER` + the plaintext password whose bcrypt hash
   you set in `BASEMENT_ADMIN_PASSWORD_HASH`.
4. You land on `/admin/clusters` (empty list).
5. Click **Add cluster** to register your first backend. Pick a
   driver (Garage v1, Garage v2, MinIO/OpenMaxIO, or AWS S3), paste
   the admin endpoint + token, and submit.
6. Once at least one cluster is registered, the user persona
   (`/files`) becomes useful: invite users via `/admin/users`, hand
   them an access key for each cluster they should reach, and they
   can browse buckets at `/files/{region}/b/{bucket}`.

There is no separate setup wizard; the empty admin list IS the
"first run" surface. Every action from here is reachable from
`/admin/*`.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Container exits immediately with `BASEMENT_JWT_SECRET must be at least 32 bytes after base64 decoding` | Secret too short, or not base64 | Regenerate with `openssl rand -base64 32` |
| Container exits with `BASEMENT_ADMIN_PASSWORD_HASH: bcrypt hash invalid` | Hash got shell-mangled (the `$2y` was interpolated as `$2` + `y`) | Wrap value in single quotes in `.env` |
| Browser shows "your connection is not private" on first load | TLS not yet configured | See [`tls.md`](tls.md) |
| Admin login succeeds but a freshly added cluster shows "Connection failed" | basement container cannot reach the backend on the URL you supplied | Confirm Docker networking — `basement` and the backend must be on the same network, or you must use a host-reachable URL |
| WebDAV mount in Finder shows "the server is not responding" | Reverse proxy is stripping PROPFIND | See [`reverse-proxy.md`](reverse-proxy.md) for the per-proxy fix |
| `/admin/audit` is empty after a few weeks | Audit retention deleted older logs | Increase `BASEMENT_AUDIT_RETENTION_DAYS`, or offload daily; see [`hardening.md`](hardening.md#audit-log-retention) |

## See also

- [`../configuration.md`](../configuration.md) — full env-var reference
- [`reverse-proxy.md`](reverse-proxy.md) — proxy recipes
- [`tls.md`](tls.md) — TLS topologies
- [`hardening.md`](hardening.md) — production posture
- [`backup-basement.md`](backup-basement.md) — backing up the data dir
- [`upgrade.md`](upgrade.md) — tag-and-restart, Watchtower
