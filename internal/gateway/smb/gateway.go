// Package smb is the SMB / CIFS gateway stub (v1.9.0c).
//
// Why a stub: native SMB on Linux currently requires either a Samba
// sidecar (configuration + privileges + a separate process to keep
// in sync with basement's identity surface) or a pure-Go SMB
// implementation (none ship today with read+write CIFS dialects
// that match macOS Finder's expectations). Until one of those paths
// lands, the gateway sits in the registry as a "coming soon"
// placeholder so the /admin/gateways UI shows it next to WebDAV.
//
// The doctrine on basement (memory: feedback_driver_parity): SMB
// parity with WebDAV is the v1.10 cycle. This stub keeps the wire
// shape stable so the UI doesn't need to know which gateways exist
// — it just renders whatever the registry returns.

package smb

import (
	"context"
	"net/http"

	"github.com/mattjackson/basement/internal/gateway"
)

// Gateway is the SMB stub. Implements gateway.Gateway with
// no-op lifecycle + nil HTTPHandler.
type Gateway struct{}

// New returns a fresh SMB gateway stub.
func New() *Gateway { return &Gateway{} }

func (g *Gateway) Name() string        { return "smb" }
func (g *Gateway) DisplayName() string { return "SMB / CIFS" }
func (g *Gateway) Description() string {
	return "Native Windows + macOS file shares (Time Machine, network drives). Requires a Samba sidecar or pure-Go SMB server — not yet implemented in basement."
}

// Capabilities advertises what SMB would do when implemented. UI
// renders the capability chips greyed-out next to a "coming soon"
// badge driven by Implemented().
func (g *Gateway) Capabilities() gateway.Capabilities {
	return gateway.Capabilities{
		Read:      true,
		Write:     true,
		Delete:    true,
		Move:      true,
		BasicAuth: true,
	}
}

// Status returns a zero-value Status — stub gateway is never running.
func (g *Gateway) Status() gateway.Status {
	return gateway.Status{Running: false}
}

// Implemented returns false — this is a stub registration.
func (g *Gateway) Implemented() bool { return false }

// Start is a no-op for stub gateways.
func (g *Gateway) Start(_ context.Context) error { return nil }

// Stop is a no-op for stub gateways.
func (g *Gateway) Stop(_ context.Context) error { return nil }

// HTTPHandler returns nil — SMB is port-bound, not HTTP-mounted.
func (g *Gateway) HTTPHandler() http.Handler { return nil }

// ListenAddress returns the would-be listen address. Empty for now
// since the gateway isn't implemented; v1.10 will read this from
// org capabilities.
func (g *Gateway) ListenAddress() string { return "" }
