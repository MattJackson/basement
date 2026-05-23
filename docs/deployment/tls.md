# TLS

basement requires HTTPS in production. The `__Host-basement_session`
cookie that carries the auth JWT has the `Secure` attribute set
unconditionally and will be rejected by every modern browser on a
plain-HTTP origin. **You will not be able to log in to a non-TLS
basement instance from a non-localhost browser.**

There are three common topologies for getting TLS in front of
basement. Pick the one that matches your environment.

## 1. Caddy auto-ACME (recommended for home labs and small ops)

The simplest path. Caddy fetches a Let's Encrypt cert at startup and
renews it automatically as long as DNS is pointing at your host and
ports 80 + 443 are reachable from the public internet.

See [`reverse-proxy.md`](reverse-proxy.md#caddy-canonical) for the
full Caddyfile. The relevant TLS bits:

- Caddy detects that `basement.example.com` is a hostname (not an
  IP) and enables auto-HTTPS on first start.
- The HTTP-01 challenge runs on port 80; the cert is served on port
  443. You don't write any TLS directives.
- Certs and account keys live in the `caddy-data` volume; mount
  this volume persistently so Caddy doesn't request a new cert on
  every restart and get rate-limited by Let's Encrypt.
- For DNS-01 (no inbound port 80 required, useful for non-public
  hosts), use a Caddy build with the appropriate DNS-provider
  plugin (cloudflare, route53, etc.) — see the Caddy docs.

This is the recommended topology for:

- Home labs with a public IP and a DNS A record
- Small VPS deployments
- Anyone who wants TLS to "just work" with no certbot cron

## 2. Behind Cloudflare

If you proxy basement through Cloudflare (the orange-cloud icon in
your DNS dashboard), Cloudflare terminates TLS at its edge and
either passes the request to your origin on plain HTTP (Flexible
mode) or re-encrypts to your origin (Full / Full Strict mode).

### Cloudflare Flexible SSL — not recommended

Cloudflare → origin runs on plain HTTP. The browser sees HTTPS, but
the path from Cloudflare to your basement instance is in the clear.
Use Flexible only if you cannot run a TLS listener on your origin
at all (rare).

The `__Host-` cookie works because the browser-facing connection is
HTTPS; basement sees the request via Cloudflare on HTTP but the
cookie attribute is honoured browser-side. **However**, basement
needs to know it's behind HTTPS to render correct redirect URLs:
set `BASEMENT_PUBLIC_URL=https://basement.example.com` so the
absolute-URL paths (share links, OIDC callbacks) use the public
HTTPS scheme.

### Cloudflare Full SSL (recommended for Cloudflare users)

Cloudflare → origin runs on HTTPS using a cert your origin presents.
Two sub-options:

- **Full (cert validation off):** origin can present any cert,
  including self-signed. Easy to set up; weaker than Full Strict
  because Cloudflare doesn't verify origin's identity.
- **Full Strict (cert validation on):** origin must present a cert
  Cloudflare trusts. Use [Cloudflare Origin
  CA](https://developers.cloudflare.com/ssl/origin-configuration/origin-ca/)
  to mint a free 15-year cert specifically for the
  Cloudflare→origin hop. This is the most secure Cloudflare
  topology.

In either Full mode, your origin reverse proxy (Caddy / Nginx /
Traefik) still terminates TLS on port 443 using a cert. If you use
Origin CA, Caddy can be told to use the static Origin CA cert
instead of auto-ACME:

```caddyfile
basement.example.com {
    tls /etc/caddy/origin-ca.crt /etc/caddy/origin-ca.key

    reverse_proxy basement:8080 {
        header_up Host {http.request.host}
    }
}
```

### Cloudflare and WebDAV

Cloudflare's free + Pro plans **strip non-standard HTTP verbs**
including `PROPFIND` / `MKCOL` / `MOVE` / `COPY` at the edge.
WebDAV mounts will fail with a 405 returned by Cloudflare before
basement sees the request. There is no Cloudflare dashboard toggle
to disable this on the free plan.

Workarounds:

1. **Use a [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)**
   (`cloudflared`). The tunnel terminates Cloudflare's edge logic
   and forwards the raw HTTP request to your origin. WebDAV verbs
   pass through.
2. **Disable Cloudflare proxy for the basement hostname** (grey
   cloud). You lose Cloudflare's edge protection but gain WebDAV.
3. **Use a separate hostname for WebDAV.** Keep
   `basement.example.com` Cloudflare-proxied for the UI, and point
   `webdav.example.com` straight at your origin (grey cloud or no
   Cloudflare at all). basement serves WebDAV on the same router;
   only the proxy posture differs.

For non-WebDAV use, Cloudflare's standard verb set covers
everything else basement does (UI + JSON API + bearer-auth M2M).

## 3. Reverse proxy terminates TLS, basement HTTP-only behind it (most common)

The shape the bundled `deploy/docker-compose.yml` implements: Caddy
(or Nginx, or Traefik) listens on `:443` with a cert, basement
listens on `:8080` HTTP-only on the internal Docker network, the
proxy forwards. basement never sees a TLS handshake — it talks plain
HTTP to a trusted upstream peer.

This is the recommended posture because:

- TLS cert renewal is the proxy's job (Caddy handles it; certbot
  handles it for Nginx)
- basement's binary stays small (no TLS code in the hot path; Go's
  `net/http` already handles it but with the proxy you avoid the
  attack surface)
- You can put multiple services behind one proxy with separate
  hostnames

basement requires no TLS-specific configuration in this mode. The
critical setting is:

```bash
BASEMENT_PUBLIC_URL=https://basement.example.com
```

This is the URL the proxy serves on. basement uses it when it needs
to construct absolute URLs (the OIDC redirect URI, the `/share/:token`
redirect target, the email-invite link). Without it, basement falls
back to the request `Host` header, which is usually right but not
always (e.g. if Cloudflare rewrites it).

## Mixed-mode caveats

- **Local development on `http://localhost:8080`** works without TLS
  because the `__Host-` cookie's `Secure` attribute is honoured by
  browsers for `http://localhost` as a special case. Production
  hostnames don't get this exemption.
- **Self-signed cert on the proxy** works for the basement UI but
  may confuse WebDAV clients (Finder, iOS Files) that don't expose
  a "trust this cert" prompt. Either trust the cert at the OS level
  or use Caddy's auto-ACME against a real domain.
- **HTTP-only basement on a private network** (e.g. internal-only
  install behind a VPN with no TLS at all) works only if you sign in
  from `http://localhost` because the `Secure` cookie attribute
  bounces every other origin. For a multi-user private deploy, run
  Caddy locally with a [DNS-01 cert](https://caddyserver.com/docs/automatic-https#dns-challenge)
  or use a internal CA.

## See also

- [`reverse-proxy.md`](reverse-proxy.md) — the proxy recipes that
  implement these topologies
- [`hardening.md`](hardening.md) — production-posture checklist
  including cookie + secret hygiene
- [Caddy docs: Automatic HTTPS](https://caddyserver.com/docs/automatic-https) —
  Caddy's auto-ACME reference
