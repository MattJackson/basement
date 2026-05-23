// Package gateway is the protocol-surface abstraction (v1.9.0c). Each
// Gateway exposes basement's storage to clients through one protocol
// (WebDAV today; SMB / NFS / FTP / S3 land in v1.10+). All gateways
// share a single Backend interface for data access, which lets the
// protocol code stay narrow + testable and lets the Backend stay shared
// across protocols.
//
// Two axes are pluggable in basement:
//
//   - Driver: HOW we talk to storage (S3, Garage, MinIO, AWS, ...).
//     internal/driver carries that registry.
//   - Gateway: HOW clients talk to us (HTTP+JSON UI, WebDAV, future
//     SMB / NFS / FTP / S3). This package carries that registry.
//
// The two axes are orthogonal — adding a new driver doesn't change the
// gateway surface; adding a new gateway doesn't change drivers. They
// meet at the Backend interface: every gateway calls Backend, every
// Backend impl composes drivers + stores.
package gateway

import (
	"context"
	"net/http"
	"time"
)

// Gateway is the interface every protocol-handler implements. The same
// shape covers HTTP-mounted gateways (WebDAV, future S3 gateway) and
// port-bound ones (SMB, NFS, FTP) — the lifecycle calls let each kind
// own its own bind path.
//
// Gateways are registered at boot via Registry.Register, then started
// via Registry.StartAll once. The Registry holds them for the life of
// the process; Stop is called on graceful shutdown.
type Gateway interface {
	// Name is the stable identifier, lowercase, used as the registry
	// key and as the on-disk config nest ("webdav", "smb", "nfs",
	// "ftp", "s3"). Must be unique across registered gateways — the
	// Registry returns an error on duplicate Register.
	Name() string

	// DisplayName is the human-friendly label shown in the operator
	// UI ("WebDAV", "SMB / CIFS", "NFS v4", "FTP / SFTP", "S3").
	DisplayName() string

	// Description is the one-sentence what-it-is rendered under the
	// gateway card in /admin/system. Keep tight; the link out to docs
	// carries the longer form.
	Description() string

	// Capabilities advertises the protocol's read/write surface so
	// the UI can render accurate capability badges. The mix of
	// supported auth methods rides in here too.
	Capabilities() Capabilities

	// Status returns runtime stats — populated on a best-effort basis
	// by each gateway implementation. Empty fields are fine; the UI
	// renders "—" for absent values.
	Status() Status

	// Implemented reports whether this gateway has a real protocol
	// implementation behind it. Stub registrations (the future SMB /
	// NFS / FTP / S3 gateways in v1.9.0c) return false; the UI uses
	// the flag to render a "coming soon" badge instead of an enable
	// toggle.
	Implemented() bool

	// Start brings the gateway online. For HTTP-mounted gateways this
	// is typically a no-op — the actual mount happens in the chi
	// router using HTTPHandler. For port-bound gateways (SMB, NFS,
	// FTP) Start binds the listen socket.
	//
	// Idempotent: a second Start on a running gateway returns nil.
	Start(ctx context.Context) error

	// Stop tears the gateway down. Errors during shutdown are
	// surfaced so the operator can see "graceful shutdown of SMB
	// failed: ..." in the boot log; the registry continues stopping
	// other gateways regardless.
	Stop(ctx context.Context) error

	// HTTPHandler returns the http.Handler the main chi router should
	// mount under the gateway's protocol-specific path (today only
	// /webdav/; v2.0 adds /s3/ for the S3-shaped gateway). Returns
	// nil for non-HTTP gateways (SMB, NFS, FTP), which bind their own
	// listen sockets inside Start.
	HTTPHandler() http.Handler

	// ListenAddress returns the configured listen address for
	// port-bound gateways ("0.0.0.0:445" for SMB, "0.0.0.0:21" for
	// FTP, etc.). Returns "" for HTTP-mounted gateways — the basement
	// HTTP server already binds, and the gateway just mounts a
	// handler on it.
	ListenAddress() string
}

// Capabilities describes the protocol surface of a gateway. The UI
// renders these as per-gateway capability chips ("Read", "Write",
// "Delete", "Move", "Lock") and as per-auth-method badges. Backend
// drivers may further constrain what's possible at runtime (e.g. a
// read-only S3 backend silently disables Write at the data layer);
// this struct is the protocol's advertised surface.
type Capabilities struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	Delete bool `json:"delete"`
	Move   bool `json:"move"`
	Lock   bool `json:"lock"`

	// Auth methods supported by the protocol. A gateway may support
	// several at once (e.g. WebDAV today accepts both BasicAuth +
	// BearerAuth via the BMNT-prefixed Basic shape).
	BasicAuth  bool `json:"basicAuth"`
	BearerAuth bool `json:"bearerAuth"`
	SigV4Auth  bool `json:"sigV4Auth"`
}

// Status carries the runtime stats the operator dashboard surfaces.
// Every field is optional; gateways that don't track a counter leave
// it at the zero value and the UI renders "—". Running flips true on
// Start, false on Stop; for stub gateways it stays false forever.
type Status struct {
	Running           bool       `json:"running"`
	ActiveConnections int        `json:"activeConnections,omitempty"`
	LastActivity      *time.Time `json:"lastActivity,omitempty"`
	TotalRequests     int64      `json:"totalRequests,omitempty"`
	LastError         string     `json:"lastError,omitempty"`
}
