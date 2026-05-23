# Hardening checklist

A production-grade basement install. Each item below is a knob the
operator owns; basement ships sane defaults but the trust boundary
is your environment.

## Network

### Bind basement to localhost or to a private network only

Do not publish the basement container's port 8080 to the public
internet. The reverse proxy is the public surface; basement is the
upstream the proxy talks to over a private network.

In Docker Compose, this looks like:

```yaml
services:
  basement:
    # No `ports:` block — basement is reachable only on the internal
    # Docker network as `basement:8080`. The caddy service is the
    # only one with a `ports:` block (80 + 443).
    networks:
      - default
```

If you need to debug from the host directly, bind to localhost only:

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

Never `- "8080:8080"` in production — that publishes basement to
every interface on the host, bypassing your proxy and any
proxy-side rate-limit or WAF.

### Trust the reverse proxy's headers

basement reads `X-Forwarded-Proto`, `X-Forwarded-For`, and `Host` to
attribute audit-log entries to the correct origin and to render
correct absolute URLs. The reverse-proxy recipes in
[`reverse-proxy.md`](reverse-proxy.md) set these headers; if you
roll your own proxy, do the same.

## Cookies

### `__Host-basement_session` is HTTPS-only by design

basement's auth cookie uses the `__Host-` prefix, which the browser
enforces:

- `Secure` (HTTPS only)
- `Path=/` (not scoped to a subpath)
- No `Domain` attribute (host-only, not shareable with subdomains)

This is the right shape. **Do not** put basement on a plain-HTTP
hostname and try to work around the cookie — every modern browser
will refuse to set it. See [`tls.md`](tls.md).

`SameSite=Strict` is also set, which means a click from another
origin (e.g. a Slack link to a basement page) will land logged-out
on first request and then logged-in after a fresh same-origin
navigation. This is intentional — it removes the CSRF vector for
mutating endpoints.

### OIDC state cookie

The OIDC flow uses a short-lived `__Host-basement_oidc_state` cookie
to bind the authorisation request to the eventual callback. Same
attributes as the session cookie. No operator action required.

## Secrets management

### Never put secrets in `docker-compose.yml`

Keep `docker-compose.yml` in version control. Keep secrets in a
separate file pulled in via `env_file:`:

```yaml
services:
  basement:
    env_file:
      - .env
    # No `environment:` block with secrets inline.
```

Then `.env` lives outside version control (`echo .env >> .gitignore`):

```bash
chmod 600 .env
chown root:root .env       # or the user running docker compose
```

If you use a secrets manager (Vault, AWS Secrets Manager, Doppler,
1Password), template the `.env` from there at deploy time. The
basement runtime doesn't reach back into a secret store — it reads
its env at startup.

### Rotate `BASEMENT_JWT_SECRET` periodically

A leaked JWT secret means:

1. An attacker can mint valid session JWTs for any user.
2. An attacker can decrypt the AES-GCM-encrypted driver credentials
   in `BASEMENT_DATA_DIR`.

Rotate on a schedule (annually at minimum) and immediately on
suspicion of leak. The rotation procedure (because the secret is
also the encryption key derivation source) is documented in
[`docker.md`](docker.md#rotating-basement_jwt_secret).

### OIDC client secret

If you use OIDC (`BASEMENT_OIDC_*`), the client secret is also a
high-value credential. Rotate it at your IdP and update the env
together with restart.

## Data directory

### File permissions

`BASEMENT_DATA_DIR` (default `/var/lib/basement` inside the
container) contains the AES-GCM-encrypted secrets store + the audit
log. The container runs as UID 65532 and writes files owned by
65532. On the host:

- The Docker named volume (`basement-data:` in the Compose file) is
  owned by `root:root` at the volume root and the entries inside
  are `65532:65532`. Don't `chmod -R 777` the volume.
- If you mount a host directory instead of a named volume (`-v
  /srv/basement:/var/lib/basement`), you must `chown -R 65532:65532
  /srv/basement` first, or basement will fail to write on first
  boot.

### Backups of the data dir

See [`backup-basement.md`](backup-basement.md). The encrypted
secrets are useless without `BASEMENT_JWT_SECRET`; back up the env
file (or the secret store entry) alongside the data dir, or you'll
have an encrypted-blob restore with no key.

## Audit log retention

basement writes one JSONL file per UTC day to
`{BASEMENT_DATA_DIR}/audit/YYYY-MM-DD.log`. The retention default
is 90 days (`BASEMENT_AUDIT_RETENTION_DAYS=90`); files older than
that are deleted on a daily sweep.

For compliance environments that need longer retention, you have
two options:

1. **Raise the env var** — `BASEMENT_AUDIT_RETENTION_DAYS=365` (or
   longer). basement keeps everything inside the data dir; disk
   usage grows linearly with traffic.

2. **Offload to external storage** — read-only copy the daily logs
   out of `{BASEMENT_DATA_DIR}/audit/` to S3 / a SIEM / a log
   pipeline. Use the dated filenames; only the current day's file
   is actively appended (so yesterday-and-older are safe to copy at
   any time). This is the operator's responsibility; basement does
   not ship an audit-log shipper.

The retention sweep is idempotent — running with a longer retention
value never deletes anything; running with a shorter value deletes
files older than the new value at next sweep.

## Container hardening

### Run as the default non-root user

The image runs as UID 65532 (the distroless "nonroot" convention).
**Don't override.** Specifically, don't add `user: root` or
`user: 0:0` to the Compose service — that defeats the
container-hardening posture, gives the process write access to the
whole rootfs, and is the kind of thing CIS Benchmarks flag.

basement does not need root for anything. It binds to port 8080
(non-privileged), reads its env, and writes to its data volume.

### Read-only root filesystem

The image's rootfs has nothing writable in it (it's `FROM scratch`
with only the binary, CA certs, and `/etc/passwd`). You can enable
Docker's `read_only: true` for an extra belt-and-braces guarantee:

```yaml
services:
  basement:
    read_only: true
    tmpfs:
      - /tmp   # Go uses /tmp for some HTTP file uploads; tmpfs it
    volumes:
      - basement-data:/var/lib/basement   # writable, where state lives
```

### Drop capabilities

basement needs zero Linux capabilities. Drop them all:

```yaml
services:
  basement:
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
```

### Pin a specific image tag

Don't run `:latest` in production unless Watchtower is wired up and
you've thought about the upgrade story (see
[`upgrade.md`](upgrade.md)). Pin a release tag like `:v1.11.0` and
upgrade explicitly. This also means you know exactly what's running
when you go to debug.

## OIDC

If you're using OIDC for sign-in:

- Use `BASEMENT_OIDC_CLIENT_SECRET` from a secret store (see
  "Secrets management" above)
- Set `BASEMENT_PUBLIC_URL` so basement constructs the correct
  redirect URI; the registered `redirect_uri` at your IdP must
  match exactly (including the trailing slash, if any)
- For local-password break-glass when OIDC is down: leave
  `BASEMENT_ADMIN_USER` + `BASEMENT_ADMIN_PASSWORD_HASH` set. The
  `/login` route always offers the local-password path; you can
  sign in as admin to revoke OIDC mappings or to fix a borked
  config

## Service accounts (M2M)

Service accounts (v1.7+) mint long-lived bearer credentials. Treat
the secret like any other API key:

- Mint scoped per-capability (don't mint `host_admin:*` for a
  script that only needs read access to one bucket)
- Rotate on a schedule
- Audit `/admin/service-accounts` for accounts you don't recognise
- Revoke immediately on suspicion of leak

The secret is shown exactly once at mint time. basement stores only
the hash — there's no recovery path.

## Production checklist

| Item | Done? |
| --- | --- |
| `BASEMENT_JWT_SECRET` ≥ 32 bytes after base64 decode, generated from `openssl rand -base64 32` | ☐ |
| `BASEMENT_ADMIN_PASSWORD_HASH` generated via `htpasswd -bnBC 12 "" PASSWORD | tr -d ':\n'` (cost ≥ 12) | ☐ |
| `BASEMENT_PUBLIC_URL` set to the HTTPS hostname the proxy serves | ☐ |
| basement container is **not** published to `0.0.0.0:8080` (only reachable via the proxy) | ☐ |
| Reverse proxy terminates TLS with a real cert (auto-ACME or BYO) | ☐ |
| WebDAV verbs pass through the proxy (see [`reverse-proxy.md`](reverse-proxy.md)) | ☐ |
| `.env` file is `chmod 600` and not in git | ☐ |
| Docker named volume `basement-data` is on the same backup schedule as the rest of the host | ☐ |
| `BASEMENT_AUDIT_RETENTION_DAYS` matches your compliance requirement | ☐ |
| Specific image tag pinned (not `:latest`) unless Watchtower is wired | ☐ |
| Container runs as the default UID 65532 (no `user:` override) | ☐ |
| OIDC client secret (if used) is in a secret store, not inline | ☐ |
| You know where the JWT secret backup lives — without it, a data-dir restore is useless | ☐ |

## See also

- [`docker.md`](docker.md) — the Compose file this hardens
- [`tls.md`](tls.md) — TLS topologies
- [`backup-basement.md`](backup-basement.md) — data-dir backup
- [`../adr/0001-rbac-three-tier-creds.md`](../adr/0001-rbac-three-tier-creds.md) —
  RBAC model that the policy gates enforce
