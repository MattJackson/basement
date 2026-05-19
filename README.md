# basement

OSS-quality admin UI for [Garage](https://garagehq.deuxfleurs.fr), the self-hosted S3-compatible object storage system. Replaces the broken `khairul169/garage-webui` experience for Garage v1.x+ deployments with a modern, multi-arch Docker image and full coverage of the Garage v2 admin API.

**Status:** v0.1.0 admin read demo. Multi-arch image at `ghcr.io/mattjackson/basement:latest`.

---

## Quickstart

Run basement against a Garage cluster on the same host (replace `<GARAGE_HOST>` with your actual hostname):

```bash
docker run -d --name basement \
  -p 8080:8080 \
  -v basement-data:/var/lib/basement \
  -e BASEMENT_DRIVER=garage \
  -e BASEMENT_DRIVER_GARAGE_ADMIN_URL=http://<GARAGE_HOST>:3903 \
  -e BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN=<your-garage-admin-token> \
  -e BASEMENT_DRIVER_GARAGE_S3_URL=http://<GARAGE_HOST>:3900 \
  -e BASEMENT_DRIVER_GARAGE_S3_REGION=garage \
  -e BASEMENT_DRIVER_GARAGE_S3_ACCESS_KEY=<your-s3-access-key> \
  -e BASEMENT_DRIVER_GARAGE_S3_SECRET_KEY=<your-s3-secret-key> \
  -e BASEMENT_ADMIN_USER=admin \
  -e BASEMENT_ADMIN_PASSWORD_HASH=$(echo "your-password" | bcrypt -g 12) \
  -e BASEMENT_JWT_SECRET=$(openssl rand -base64 32) \
  ghcr.io/mattjackson/basement:latest
```

Visit `http://localhost:8080` and log in with the admin credentials you specified. See [`docs/configuration.md`](docs/configuration.md) for the complete environment variable reference.

---

## Configuration

See [`docs/configuration.md`](docs/configuration.md) for the full canonical reference of all `BASEMENT_*` environment variables. Top-3 required vars for impatient readers:

| Variable | Example | Description |
|----------|---------|-------------|
| `BASEMENT_DRIVER_GARAGE_ADMIN_URL` | `http://garage:3903` | Garage admin API v2 endpoint |
| `BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN` | `<token>` | Bearer token for admin API (never exposed to browser) |
| `BASEMENT_JWT_SECRET` | `<base64>` | HS256 signing secret, ≥32 bytes after decoding |

---

## Docker Compose (recommended)

Three-service reference deployment: basement + Garage + Caddy with TLS termination. See [`deploy/docker-compose.yml`](deploy/docker-compose.yml) for the full file with annotated comments.

```bash
# Copy example env file and fill in CHANGE_ME values
cp deploy/.env.example .env
nano .env  # edit secrets

# Start all three services
docker compose -f deploy/docker-compose.yml up -d
```

Access basement at `https://basement.example.com` (Caddy handles automatic Let's Encrypt certificates). The compose file includes:

- **garage** service (`dxflrs/garage:v1.0.1`) with internal-only ports 3900/3903
- **basement** service with Watchtower auto-update label
- **caddy** service terminating TLS for `basement.example.com` → basement:8080 and `s3.example.com` → garage:3900

---

## Reverse proxy (Caddy)

For production deployments behind Caddy, use the two-hostname pattern:

```caddyfile
basement.example.com {
    reverse_proxy to localhost:8080 {
        header_up Host {host}
    }
}

s3.example.com {
    # CRITICAL: S3 SigV4 signing requires Host header preservation
    # Without this, signature validation fails with SignatureDoesNotMatch errors
    reverse_proxy to localhost:3900 {
        header_up Host {host}
    }
}
```

The **Host header preservation** gotcha is the #1 footgun when proxying S3. Garage's SigV4 implementation validates that the Host header matches what was signed in the request. See [`deploy/Caddyfile`](deploy/Caddyfile) for a standalone example and [`docs/configuration.md`](docs/configuration.md) for full Caddy configuration notes.

---

## Backend support

| Backend | Status | Driver name |
|---------|--------|-------------|
| Garage v1.x+ (v2 admin API) | ✅ supported | `garage` |
| basement | 🚧 planned | `basement` |

Only the `garage` driver is implemented in v0.1.0–v1.0. The basement driver will be added in M7 when the backend stabilizes.

---

## Auth model (v1.0)

Single admin account from environment variables: `BASEMENT_ADMIN_USER` + `BASEMENT_ADMIN_PASSWORD_HASH`. Authentication uses bcrypt password verification with httpOnly JWT cookie sessions (24h sliding TTL, signed with HS256).

**Roadmap:**
- **v1.1**: Multi-user with basement-owned grants (users.json, grants.json)
- **v1.3**: OIDC integration (`coreos/go-oidc`) for SSO

---

## Architecture overview

basement sits between the browser and Garage's admin + S3 ports, terminating authentication and holding the Garage admin token server-side. The driver interface translates basement's API calls into backend-specific wire format, enabling future backends without frontend changes.

```
┌─────────────┐     ┌──────────────────┐     ┌─────────┐
│   Browser   │────▶│  basement     │────▶│ Garage  │
│             │     │  (Go backend)    │     │         │
│             │◀────│                  │◀────│ admin   │
│             │     │  /api/v1/*       │     │ + S3    │
└─────────────┘     └──────────────────┘     └─────────┘
                       │         │
                       ▼         ▼
                   JWT cookie  Admin token
                   (httpOnly)  (server-side)
```

See [`design.md`](../design.md) for the full architecture diagram and data flow.

---

## Building from source

Prerequisites: Go 1.25+, Node.js 22+, pnpm, Docker Buildx.

```bash
# Install frontend dependencies
pnpm install

# Build frontend + backend in one command (multi-arch)
docker buildx bake -f docker/docker-bake.hcl

# Single-arch local iteration
pnpm build          # builds frontend to internal/web/dist
go build -o basement ./cmd/basement-server
```

The Dockerfile embeds the built frontend into the Go binary. See [`docker/Dockerfile`](../docker/Dockerfile) for multi-stage build details.

---

## Contributing

Once v0.1 lands, contributions welcome via GitHub Issues and Discussions. For now, design discussion is the primary channel. See [GitHub issues](https://github.com/MattJackson/basement/issues) when opened.

---

## License

MIT. See [`LICENSE`](../LICENSE).
