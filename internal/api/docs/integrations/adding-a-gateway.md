# Adding a new gateway

basement's protocol surface is pluggable. Each native-protocol mount
(WebDAV today; SMB, NFS, FTP, S3 are wired as stubs in v1.9.0c and
will land as full implementations across v1.10+) lives behind a single
interface — `internal/gateway.Gateway` — and reaches the storage
layer through a single contract — `internal/gateway.Backend`.

This document walks through the recipe for adding a brand-new gateway:
the two interfaces you have to implement, where to register it, how
to surface it in the operator UI, and the testing pattern that
existing gateways follow.

## Architecture in one paragraph

Two axes are pluggable in basement. The **Driver** axis (`internal/driver`)
controls _how basement talks to storage_ — S3, Garage, MinIO, AWS, and
whatever drivers ship next. The **Gateway** axis (`internal/gateway`)
controls _how clients talk to basement_ — the HTTP+JSON UI, WebDAV,
and the SMB / NFS / FTP / S3-API gateways that lie ahead. The two
axes are orthogonal: adding a new driver doesn't change the gateway
surface, and adding a new gateway doesn't change drivers. They meet
at the `Backend` interface — every gateway calls `Backend`, every
production `Backend` impl composes drivers + stores.

## Step 1: implement the Gateway interface

Create a new package under `internal/gateway/{name}/`. The package
holds one type that satisfies `gateway.Gateway`. Use the WebDAV
implementation (`internal/gateway/webdav/gateway.go`) as the
canonical reference; the stub gateways
(`internal/gateway/smb/gateway.go`, `nfs`, `ftp`, `s3`) are minimal
"return false / no-op" templates suitable for cribbing the boilerplate.

```go
package myproto

import (
    "context"
    "net/http"

    "github.com/mattjackson/basement/internal/gateway"
)

type Gateway struct {
    backend gateway.Backend
    // ...protocol-specific fields...
}

func New(deps Deps) *Gateway {
    return &Gateway{backend: deps.Backend}
}

// --- gateway.Gateway interface -----------------------------------

func (g *Gateway) Name() string        { return "myproto" }
func (g *Gateway) DisplayName() string { return "My Protocol" }
func (g *Gateway) Description() string {
    return "What clients can do with this gateway."
}

func (g *Gateway) Capabilities() gateway.Capabilities {
    return gateway.Capabilities{
        Read:   true,
        Write:  true,
        Delete: true,
        // Set the auth method(s) the protocol carries on the wire.
        BasicAuth: true,
    }
}

func (g *Gateway) Status() gateway.Status {
    return gateway.Status{
        Running: g.running.Load(),
        // ...optional counters...
    }
}

func (g *Gateway) Implemented() bool { return true }

func (g *Gateway) Start(ctx context.Context) error {
    // HTTP-mounted gateway: no-op; the mount happens via HTTPHandler.
    // Port-bound gateway: bind the listen socket here.
    return nil
}

func (g *Gateway) Stop(ctx context.Context) error {
    return nil
}

func (g *Gateway) HTTPHandler() http.Handler {
    // Return your http.Handler for HTTP-mounted gateways; nil for
    // port-bound ones (SMB, NFS, FTP).
    return g
}

func (g *Gateway) ListenAddress() string {
    // "0.0.0.0:445" for SMB, "0.0.0.0:2049" for NFS, etc.
    // "" for HTTP-mounted gateways — the basement HTTP server already
    // binds, and the gateway just mounts a handler on it.
    return ""
}
```

The five accessor methods (`Name`, `DisplayName`, `Description`,
`Capabilities`, `Implemented`) feed the operator UI verbatim. The
`/admin/gateways` endpoint reads them on every request — keep them
cheap (no I/O, no lock acquisition).

Lifecycle methods (`Start`, `Stop`) MUST be idempotent: `StartAll`
is called once at boot, but tests register and start gateways in
loops, so a second `Start` on a running gateway should return `nil`.

## Step 2: use the Backend interface for storage

`gateway.Backend` wraps basement's existing primitives (Auth,
UserRegions, ServiceAccounts, Driver Registry) behind a single
contract so:

- your protocol code never reaches into `internal/driver` or
  `internal/store` directly,
- the gateway tests can pass a small mock instead of a full driver
  registry + UserRegions store,
- adding a new gateway in v1.10+ doesn't require new wiring between
  protocol code and storage code.

The data-plane methods you'll call from your protocol handler:

```go
type Backend interface {
    // Auth — pick the one your protocol speaks.
    AuthBasic(ctx, user, pass) (*UserContext, error)
    AuthBearer(ctx, akidSecret) (*UserContext, error)
    AuthSigV4(ctx, signedRequest) (*UserContext, error)

    // Data plane — S3-shaped verbs that every backend implements.
    ListRegions(ctx, uctx) ([]Region, error)
    ListBuckets(ctx, uctx, regionID) ([]Bucket, error)
    ListObjects(ctx, uctx, regionID, bucket, prefix, delimiter, token, limit)
    HeadObject(ctx, uctx, regionID, bucket, key) (ObjectMeta, error)
    GetObject(ctx, uctx, regionID, bucket, key) (ReadCloser, ObjectMeta, error)
    PutObject(ctx, uctx, regionID, bucket, key, body, size, contentType) error
    DeleteObject(ctx, uctx, regionID, bucket, key) error
    CopyObject(ctx, uctx, srcRegion, srcBucket, srcKey, dstRegion, dstBucket, dstKey)

    // Bucket lifecycle — for protocols that surface bucket-create
    // (WebDAV does this via MKCOL at /{alias}/{bucket}).
    CreateBucket(ctx, uctx, regionID, bucket) error
    DeleteBucket(ctx, uctx, regionID, bucket) error
}
```

`UserContext` is the resolved identity after the auth call. Thread
it into every data-plane call so per-user filtering stays in the
Backend impl instead of leaking into protocol code.

## Step 3: register at boot in `cmd/basement-server/main.go`

Look for the `v1.9.0c GATEWAY registry` block; add your gateway
alongside the existing registrations:

```go
gwRegistry := gateway.New()

myproto := myproto.New(myproto.Deps{
    Backend: gwBackend,
    OrgCaps: myprotoOrgCapsBridge{caps: st.OrgCapabilities()},
    Audit:   auditLogger,
    Logger:  slog.Default(),
})
if err := gwRegistry.Register(myproto); err != nil {
    slog.Error("failed to register myproto gateway", "error", err)
    os.Exit(1)
}
```

If your gateway is HTTP-mounted, wire its handler into the chi
router; if it's port-bound (binds its own socket), `StartAll` is
the only call you need — the gateway owns its own bind inside
`Start(ctx)`.

## Step 4: surface a per-protocol Enable toggle

basement's operator UI reads `OrgCapabilities.Gateways.Protocols`
to decide which gateways are enabled. The v1.9.0d shape is a
name-keyed map, so adding a new gateway requires no Go field
changes:

```go
caps.Gateways.Protocols["myproto"] = store.GatewayConfig{
    Enabled: true,
}
```

In your gateway's request handler, gate on the toggle via the
`OrgCaps` adapter your `Deps` carries:

```go
if !g.orgCaps.IsEnabled() {
    writeError(w, http.StatusForbidden, "GATEWAY_DISABLED", ...)
    return
}
```

The bridge from `*store.OrgCapabilitiesStore` to your narrow
`IsEnabled()` interface is a four-line adapter in `main.go`; see
`webdavOrgCapsBridge` for the pattern.

If your gateway needs a few per-protocol settings beyond `Enabled`
+ `BaseURL` (e.g. a share-name prefix, an export root, a custom
auth realm), use the `Options map[string]string` field on
`GatewayConfig`. v1.10+ may evolve the shape; for now `Options`
is the escape hatch.

## Step 5: docs + UI integration

Add `docs/integrations/{name}.md` — the operator UI links to it
from the per-row docs pointer. The page should cover:

1. What the protocol does + who it's for.
2. Per-client connect instructions (macOS / Windows / Linux /
   mobile, whichever apply).
3. Authentication: which BMNT shape goes where.
4. Known limitations versus the protocol spec.
5. Troubleshooting common errors.

The WebDAV doc (`docs/integrations/webdav.md`) is the reference
shape; the stub gateway docs (`smb.md`, `nfs.md`, `ftp.md`,
`s3.md`) are templates for "coming soon" implementation tracking
that the UI links to until the gateway ships.

## Step 6: testing pattern

Two layers of tests:

**Unit tests for your `Gateway` impl** under
`internal/gateway/{name}/gateway_test.go`. Use a fake `Backend`
(small struct with the data-plane methods you need); the registry
tests in `internal/gateway/registry_test.go` show the `fakeGateway`
pattern. Cover auth, the verb mapping, the protocol-specific quirks
(folder markers for WebDAV; rename semantics for NFS).

**Wire-shape tests in the API package** at
`internal/api/admin_gateways_test.go`. The existing test asserts
the registry roster returns the five gateways with the right
`Implemented` + `Enabled` flags. Add a row for your gateway when
it lands; the tie-in is automatic — once you `Register` it in
`main.go`, the `/admin/gateways` endpoint surfaces it without any
handler changes.

The WebDAV gateway also ships an end-to-end test
(`internal/gateway/webdav/gateway_test.go`) that exercises real
HTTP verbs against an in-process server with a mock Backend. Crib
from that pattern for protocols where the end-to-end behaviour is
the integration risk.

## Reference implementation

`internal/gateway/webdav/` is the canonical reference. Notable
files:

- `gateway.go` — the `Gateway` interface impl, `Status()` counters,
  the `OrgCapsLookup` adapter pattern.
- `handler.go` — the HTTP request handler with auth + verb
  dispatch.
- `fs.go` — the `webdav.FileSystem` shim that translates WebDAV
  verbs into Backend calls.
- `auth.go` — Basic + Bearer/BMNT credential parsing.
- `gateway_test.go` — end-to-end verb tests against a fake Backend.

When in doubt, start with the WebDAV layout and trim the bits your
protocol doesn't need; consistency across gateways pays off when
the operator (or future-you) reads two gateway packages back-to-back
during an incident.
