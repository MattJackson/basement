# Deployment guide

This is the operator path from "I just heard about basement" to a
production-grade install behind TLS with backups, hardening, and a
documented upgrade story.

basement ships as a single static Go binary embedded in a distroless
Docker image (`ghcr.io/mattjackson/basement`). The image runs as a
non-root user (UID 65532), reads its configuration from environment
variables, and persists its own state as JSON files under
`BASEMENT_DATA_DIR` (default `/var/lib/basement`). There is no
external database to manage.

Pick the topology that matches what you want to run.

## Supported topologies

### 1. Single-instance Docker behind a reverse proxy (recommended)

The canonical small-to-medium deploy: one `basement` container behind
Caddy (or Nginx, or Traefik) on a single host. TLS terminated at the
proxy; basement bound to localhost or to a private container network.
This is what the `deploy/docker-compose.yml` in the repo lays out and
what 90% of operators should reach for.

See:

- [`docker.md`](docker.md) — the annotated Compose file, secret
  generation, the first-run wizard, the volume layout
- [`reverse-proxy.md`](reverse-proxy.md) — Caddy / Nginx / Traefik
  examples, including the WebDAV verb-passthrough fix
- [`tls.md`](tls.md) — TLS topologies (auto-ACME, behind Cloudflare,
  proxy-terminated)
- [`hardening.md`](hardening.md) — production checklist
- [`backup-basement.md`](backup-basement.md) — backing up the basement
  data dir itself (separate from the bucket-to-bucket backup feature)
- [`upgrade.md`](upgrade.md) — pull-and-restart procedure + Watchtower
  setup

### 2. Kubernetes

basement runs cleanly as a single-replica StatefulSet with a
PersistentVolumeClaim mounted at `BASEMENT_DATA_DIR`. The image has
no privileged requirements; it runs as UID 65532 by default and
listens on a single TCP port. Ship a `Secret` for the JWT secret,
admin password hash, and any driver credentials; expose via Ingress
with TLS at the ingress.

A worked k8s manifest is not bundled in v1.11; the env-var contract
documented in [`docker.md`](docker.md#environment-variables) maps
directly into a `ConfigMap` + `Secret` split.

### 3. Bare metal (no container)

The Go binary is statically linked (`CGO_ENABLED=0`, distroless
runtime). You can pull it out of the image (`docker create
ghcr.io/mattjackson/basement:latest && docker cp ...:/basement
/usr/local/bin/`) and run it under systemd. Configuration is the same
env-var contract; `BASEMENT_DATA_DIR` must point at a writable
directory.

A bare-metal recipe is not bundled in v1.11. The Compose + env-var
docs cover the contract you need.

## What's in this directory

| File | What it covers |
| --- | --- |
| [`docker.md`](docker.md) | Annotated `docker-compose.yml`, env vars, secret generation, first-run wizard, volume layout |
| [`reverse-proxy.md`](reverse-proxy.md) | Caddy (canonical) + Nginx + Traefik examples with WebDAV verb passthrough, streaming, max body size |
| [`tls.md`](tls.md) | Auto-ACME (home labs), behind Cloudflare (Flexible / Full SSL), proxy-terminated TLS |
| [`hardening.md`](hardening.md) | Network binding, cookie posture, secrets management, data-dir perms, audit retention, container user |
| [`backup-basement.md`](backup-basement.md) | What's in `BASEMENT_DATA_DIR`, safe-copy procedure, restore steps, the "don't restore over a live writer" caveat |
| [`upgrade.md`](upgrade.md) | Tag pull + restart, Watchtower wiring, migration notes, the v1.x forward-compat contract |

## Pre-reqs

- A host that can run Docker (or Podman, or k8s)
- A domain you control, with an A / AAAA record pointing at the host
  (for TLS via Let's Encrypt)
- Outbound HTTPS from the host (for ACME challenges and pulling the
  basement image)
- Inbound TCP/443 (and TCP/80 if you want HTTP→HTTPS redirect at the
  proxy)
- At least one S3-compatible backend you want basement to manage —
  Garage, MinIO/OpenMaxIO, an AWS S3 account, or a mix

If you just want a 5-minute "does this run on my laptop" demo, the
top-level [`README.md` Quickstart](../../README.md#quickstart) skips
straight to `docker run` with no proxy and no TLS. Come back here when
you're ready to put it in front of real users.

## See also

- [`../configuration.md`](../configuration.md) — full environment
  variable reference
- [`../integrations/webdav.md`](../integrations/webdav.md) — WebDAV
  gateway-specific reverse-proxy notes
- [`../adr/`](../adr/) — architectural decision records that
  explain the security model (RBAC, encryption, sudo-elevation)
