# Reverse proxy

basement listens on plain HTTP on port 8080. In production, you put a
reverse proxy in front of it that:

1. Terminates TLS for `https://basement.example.com`
2. Forwards every verb (including the non-standard WebDAV ones —
   `PROPFIND`, `PROPPATCH`, `MKCOL`, `COPY`, `MOVE`) to basement
3. Streams request and response bodies without buffering (multi-GB
   PUTs are a normal workload)
4. Allows large request bodies for object uploads
5. Preserves the `Host` header so basement renders correct absolute
   URLs and the `__Host-basement_session` cookie is accepted by the
   browser

This page covers Caddy (recommended for home labs and small ops),
Nginx (most common in the wider world), and Traefik (most common in
k8s and Docker-native shops).

> **WebDAV gotcha.** basement's WebDAV gateway lives at `/webdav/*`
> on the same router as `/api/v1/*`. WebDAV uses non-standard HTTP
> verbs that some proxy stacks filter at the edge. The Caddy
> example below includes the **`methods`** workaround that lets
> Caddy ≤ v2.8 pass `PROPFIND` / `MKCOL` / `MOVE` / `COPY` through
> on HTTP/2. Skip it if you don't use the WebDAV gateway; include
> it (it's free) if you do or might.

## Caddy (canonical)

Caddy is the recommended choice for self-hosters: zero-config
auto-ACME, sensible defaults for streaming and body sizes, and a
two-line config for the common case.

### Caddyfile

```caddyfile
# basement.example.com — the basement UI + API + WebDAV gateway.
basement.example.com {
    # Pass through every HTTP verb basement implements, including
    # the WebDAV non-standard ones. Caddy ≤ v2.8 on HTTP/2 filters
    # unknown verbs at the edge and returns 405 before the request
    # reaches the upstream; this directive whitelists every verb
    # basement uses. Keep this even if you're not on WebDAV today —
    # it's free, and basement may add more verbs over time.
    @basement-methods {
        method GET POST PUT DELETE PATCH OPTIONS HEAD PROPFIND PROPPATCH MKCOL COPY MOVE LOCK UNLOCK
    }
    handle @basement-methods {
        # Large request bodies for object uploads. Adjust to match
        # what your users will PUT. 5GB matches AWS S3's per-PUT
        # ceiling for a single (non-multipart) upload.
        request_body {
            max_size 5GB
        }

        reverse_proxy basement:8080 {
            # Preserve the Host header so basement renders correct
            # absolute URLs (BASEMENT_PUBLIC_URL is the override).
            header_up Host {http.request.host}
            header_up X-Forwarded-Proto {scheme}
            header_up X-Forwarded-For {remote_host}

            # Long upload + download streams. Disable buffering so
            # PUTs/GETs stream end-to-end instead of being staged
            # through Caddy's memory.
            flush_interval -1

            transport http {
                # Time the upstream is allowed before responding
                # with the first byte. Backends signing multi-GB
                # uploads can take a while.
                response_header_timeout 5m
                # Time per stream. 30m allows slow uploaders on
                # large objects without dropping the connection.
                read_timeout 30m
                write_timeout 30m
            }
        }
    }

    # Anything outside the whitelist (rare): explicit 405.
    handle {
        respond "Method not allowed" 405
    }

    encode zstd gzip
}
```

### What each directive does

- **`@basement-methods` + `handle @basement-methods`** — the
  workaround. Without this, Caddy on HTTP/2 will reject `PROPFIND`
  with `HTTP/2 PROTOCOL_ERROR` or return a synthetic 405 before the
  request reaches basement. Listing the verbs explicitly tells Caddy
  these are acceptable. The smoke probe in
  `scripts/postdeploy-ui-smoke.ts` (`[v1.9c]`) exists specifically
  to catch this kind of proxy-side filtering.

- **`request_body { max_size 5GB }`** — Caddy's default is 10MB,
  which is fine for the UI but catastrophic for object PUTs. Adjust
  upward to match the largest object your users will upload through
  the WebDAV gateway or the bucket browser.

- **`header_up Host {http.request.host}`** — preserves the original
  hostname. basement uses the request host to gate `__Host-`
  cookies; if Caddy rewrites it, the browser refuses the session
  cookie. (Caddy preserves Host by default; this is here for
  clarity in code review.)

- **`flush_interval -1`** — disables Caddy's response buffering. For
  multi-GB GETs (object downloads), buffering would either stage
  everything in memory or stall waiting for `Content-Length`. `-1`
  flushes after every Write.

- **`response_header_timeout 5m / read_timeout 30m / write_timeout
  30m`** — generous timeouts for long uploads. Tune downward only
  if you don't expect large objects.

- **`encode zstd gzip`** — compresses the HTML / JS / JSON of the
  UI. Compression is content-type-aware in Caddy, so it won't try
  to gzip an already-compressed object PUT.

### TLS

Auto-ACME (Let's Encrypt) is on by default. Caddy will fetch and
renew a cert as long as:

- A and/or AAAA DNS records for `basement.example.com` point at the
  host running Caddy
- TCP 80 and 443 are reachable from the public internet (for the
  HTTP-01 challenge and for serving the cert)
- The Caddy container has a writable `/data` volume (so it can
  persist account keys + certs across restarts)

For other TLS topologies (DNS-01 challenge, behind Cloudflare,
proxy-terminated already), see [`tls.md`](tls.md).

## Nginx

Nginx config for the same shape:

```nginx
upstream basement {
    server basement:8080;
    keepalive 32;
}

# Increase max body size globally or per-server.
client_max_body_size 5G;

# Pass WebDAV verbs through. Nginx forwards arbitrary methods by
# default — there is no allowlist to add — but you DO need to
# explicitly proxy_pass for paths that use them, which means a
# single `location /` block is enough.

server {
    listen 443 ssl http2;
    server_name basement.example.com;

    # TLS — bring your own cert + key, or use certbot / acme.sh.
    ssl_certificate     /etc/nginx/certs/basement.example.com.crt;
    ssl_certificate_key /etc/nginx/certs/basement.example.com.key;
    ssl_protocols       TLSv1.2 TLSv1.3;

    # Long timeouts for streaming uploads and downloads.
    proxy_connect_timeout    60s;
    proxy_send_timeout       30m;
    proxy_read_timeout       30m;

    # Disable buffering for streaming bodies in both directions.
    proxy_request_buffering  off;
    proxy_buffering          off;
    proxy_http_version       1.1;

    location / {
        proxy_pass http://basement;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket / streaming support.
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        $http_connection;
    }
}

# HTTP -> HTTPS redirect.
server {
    listen 80;
    server_name basement.example.com;
    return 301 https://$host$request_uri;
}
```

Nginx forwards `PROPFIND`/`MKCOL`/`MOVE`/`COPY` without any
allowlist; the **most common Nginx gotcha for WebDAV is body
buffering**, which the `proxy_request_buffering off` directive
above fixes. If you have a corporate Nginx with a WAF (ModSecurity
or NAXSI) in front, that WAF may filter non-standard verbs — check
its ruleset.

## Traefik

Traefik (v2 + v3) handles WebDAV verbs out of the box. The shape is
labels-on-the-basement-container rather than a config file.

```yaml
services:
  basement:
    image: ghcr.io/mattjackson/basement:v1.11.0
    restart: unless-stopped
    env_file: .env
    volumes:
      - basement-data:/var/lib/basement
    networks:
      - traefik
    labels:
      # Enable Traefik routing for this service.
      - "traefik.enable=true"

      # Router: match the hostname, use the HTTPS entrypoint, attach
      # the auto-ACME certresolver.
      - "traefik.http.routers.basement.rule=Host(`basement.example.com`)"
      - "traefik.http.routers.basement.entrypoints=websecure"
      - "traefik.http.routers.basement.tls.certresolver=letsencrypt"

      # Service: point at basement's HTTP port.
      - "traefik.http.services.basement.loadbalancer.server.port=8080"

      # Large body support (default is 4MB in some Traefik builds).
      # buffering middleware caps max body — set it generously.
      - "traefik.http.middlewares.basement-body.buffering.maxRequestBodyBytes=5368709120"
      - "traefik.http.middlewares.basement-body.buffering.memRequestBodyBytes=1048576"
      - "traefik.http.middlewares.basement-body.buffering.maxResponseBodyBytes=5368709120"
      - "traefik.http.middlewares.basement-body.buffering.memResponseBodyBytes=1048576"
      - "traefik.http.routers.basement.middlewares=basement-body"

networks:
  traefik:
    external: true
```

> **Traefik buffering caveat.** The `buffering` middleware above
> *enables* buffering up to the configured limits, which works for
> small-to-medium uploads but stages the body in memory or on disk.
> For pure streaming (no body cap), omit the buffering middleware
> entirely — Traefik streams by default, with no max body. The
> trade-off: no body cap = a misbehaving client can stream forever.

## Notes on streaming + max body size

- **Object PUTs from the bucket browser** flow `browser →
  reverse-proxy → basement → backend`. basement does not buffer the
  body; it streams to the backend as bytes arrive. Your proxy must
  do the same, or memory will balloon on multi-GB uploads.

- **Object GETs (downloads)** flow `backend → basement → proxy →
  browser`. Same shape, same buffering concern. Caddy's
  `flush_interval -1` and Nginx's `proxy_buffering off` are the
  controls.

- **AWS S3 PUT object limit** (without multipart) is 5GB. basement
  does not yet do multipart uploads from the browser (that's on the
  backlog); for objects > 5GB, use the WebDAV gateway with a client
  that chunks (rclone), or use the AWS CLI directly against the
  backend.

- **WebDAV PROPFIND on large buckets is slow** (v1.9 limitation —
  see [`../integrations/webdav.md`](../integrations/webdav.md)).
  Set your proxy's `proxy_read_timeout` / `read_timeout` to at
  least 5m to avoid premature disconnects on first listing of a
  10K+ object bucket.

## Per-proxy verb passthrough matrix

| Proxy | `GET`/`POST`/`PUT`/`DELETE` | `PROPFIND` / `MKCOL` / `MOVE` / `COPY` | Fix |
| --- | --- | --- | --- |
| Caddy 2.9+ | passes | passes | none needed |
| Caddy ≤ 2.8 on HTTP/2 | passes | **filtered** | `@matcher method ...` whitelist (above) |
| Nginx | passes | passes | none for verbs; turn off `proxy_request_buffering` |
| Traefik v2 + v3 | passes | passes | none for verbs; consider buffering middleware |
| Apache `mod_proxy` | passes | passes if `mod_dav` not blocking | confirm `mod_security` ruleset |
| HAProxy | passes | passes | none |
| Cloudflare (Free / Pro) | passes | **filtered by default** | enterprise plan or `cloudflared` tunnel; see [`tls.md`](tls.md#behind-cloudflare) |

## See also

- [`docker.md`](docker.md) — Compose file the proxy sits in front of
- [`tls.md`](tls.md) — TLS topologies
- [`../integrations/webdav.md`](../integrations/webdav.md) —
  WebDAV-specific reverse-proxy notes and the per-platform mount
  walkthrough
- [`../../deploy/Caddyfile`](../../deploy/Caddyfile) — the bundled
  reference Caddyfile (note: the v1.11.0b cycle generalises the
  Caddyfile here in the docs; the bundled file is a minimal
  starting point)
