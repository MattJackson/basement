# WebDAV Caddy Required Directives

## Overview

Basement's WebDAV gateway requires specific Caddy configuration to properly pass through non-standard HTTP verbs used by the WebDAV protocol (`PROPFIND`, `PROPPATCH`, `MKCOL`, `COPY`, `MOVE`, `LOCK`, `UNLOCK`).

**This is an operator infrastructure requirement, not a code change.**

## The Problem

Caddy versions ≤ v2.8 running on HTTP/2 filter unknown verbs at the proxy edge and return `HTTP/2 PROTOCOL_ERROR` or synthetic `405 Method Not Allowed` responses before requests reach basement's WebDAV gateway. This manifests as:

- **Sev-2: OPTIONS failures** - macOS Finder DAD discovery probes fail
- **Sev-2: PROPFIND failures** - bucket listing operations fail silently
- Clients report "connection refused" or "method not allowed" errors

## Required Caddyfile Directives

Add these directives to your Caddy configuration for the basement hostname:

```caddyfile
basement.example.com {
    # Whitelist all HTTP verbs basement implements, including WebDAV non-standard ones.
    # This is REQUIRED for Caddy ≤ v2.8 on HTTP/2 to pass PROPFIND/MKCOL/COPY/MOVE
    @basement-methods {
        method GET POST PUT DELETE PATCH OPTIONS HEAD PROPFIND PROPPATCH MKCOL COPY MOVE LOCK UNLOCK
    }
    
    handle @basement-methods {
        # Large request bodies for object uploads (5GB = AWS S3 single-PUT limit)
        request_body {
            max_size 5GB
        }

        reverse_proxy basement:8080 {
            # Preserve Host header for correct absolute URL generation
            header_up Host {http.request.host}
            header_up X-Forwarded-Proto {scheme}
            header_up X-Forwarded-For {remote_host}

            # Disable response buffering for streaming uploads/downloads
            flush_interval -1

            transport http {
                response_header_timeout 5m
                read_timeout 30m
                write_timeout 30m
            }
        }
    }

    # Explicit 405 for non-whitelisted methods
    handle {
        respond "Method not allowed" 405
    }

    encode zstd gzip
}
```

## Key Directives Explained

### `@basement-methods` + `handle @basement-methods`

**Purpose:** Workaround for Caddy HTTP/2 verb filtering.

Without this matcher, Caddy rejects WebDAV verbs at the edge before they reach basement. The explicit whitelist tells Caddy these are acceptable methods to pass through.

**Version note:** Caddy 2.9+ passes all methods by default. This directive is still recommended for forward compatibility as basement may add more verbs over time.

### `request_body { max_size 5GB }`

**Purpose:** Allow large object uploads through the WebDAV gateway.

Caddy's default body size limit is 10MB, which is sufficient for UI requests but catastrophic for object PUTs. Adjust upward to match your largest expected single upload.

### `flush_interval -1`

**Purpose:** Streaming mode for multi-GB transfers.

Disables Caddy's response buffering so uploads/downloads stream end-to-end instead of staging through Caddy's memory. Critical for large objects.

### Timeouts

```caddyfile
response_header_timeout 5m    # First byte timeout (slow PROPFIND on large buckets)
read_timeout 30m              # Upload duration allowance
write_timeout 30m             # Download duration allowance
```

WebDAV `PROPFIND` operations on large buckets can take several seconds. Set timeouts to at least 5 minutes for first listing of 10K+ object buckets.

## Verification

After updating your Caddyfile, verify WebDAV is working:

```bash
# Test OPTIONS (Finder discovery)
curl -I https://basement.example.com/webdav

# Expected response headers:
# DAV: 1, 3
# Allow: OPTIONS, GET, HEAD, POST, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, COPY, MOVE

# Test PROPFIND (bucket listing)
curl -X PROPFIND https://basement.example.com/webdav/ \
  -H "Depth: 0" \
  -u user:password
```

## Alternative Proxies

| Proxy | WebDAV Verbs | Fix Needed |
|-------|-------------|------------|
| Caddy ≤ v2.8 (HTTP/2) | Filtered | `@matcher method ...` whitelist (above) |
| Caddy 2.9+ | Passes | None needed |
| Nginx | Passes | Turn off `proxy_request_buffering` only |
| Traefik v2+v3 | Passes | Consider buffering middleware |

See [`docs/deployment/reverse-proxy.md`](../deployment/reverse-proxy.md) for full proxy configuration examples.

## Troubleshooting

### "Method Not Allowed" errors

**Cause:** Caddy filtering WebDAV verbs at the edge.

**Fix:** Ensure `@basement-methods` matcher includes all required verbs and wraps your `reverse_proxy` block.

### OPTIONS returning 401 instead of 200

**Cause:** Basement's pre-auth OPTIONS short-circuit not being reached.

**Fix:** Verify Caddy is passing OPTIONS to basement:8080 without authentication requirements.

### PROPFIND timing out after 60s

**Cause:** Proxy-level timeout shorter than bucket listing duration.

**Fix:** Increase `response_header_timeout` and `read_timeout` in the transport block.

## Operator Checklist

- [ ] Caddyfile includes `@basement-methods` method whitelist
- [ ] `request_body { max_size }` set to ≥ 5GB for object storage use case
- [ ] `flush_interval -1` configured for streaming mode
- [ ] Timeouts adequate for large bucket PROPFIND (≥ 5m)
- [ ] Host header preserved with `header_up Host {http.request.host}`
- [ ] WebDAV verified working via curl test after deployment
