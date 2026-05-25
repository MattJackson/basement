// Package s3 is the S3-API gateway stub (v1.9.0c).
//
// Why a stub: basement currently routes S3 traffic via the user's
// region keychain (the user supplies an S3 endpoint + key per
// region). The v2.0 S3 GATEWAY inverts the relationship: basement
// itself terminates an S3 endpoint, verifies SigV4 against
// service-account credentials, and proxies the request to the
// underlying driver. This is the substrate the SDK story rides on
// — once basement speaks S3 natively, every existing S3 SDK in any
// language is a first-class basement client.
//
// v1.9.0c registers the stub so the wire shape stays stable and so
// /admin/gateways can render "S3 (coming soon)" alongside WebDAV.
// The actual SigV4 verification + request proxy lands in v2.0.

package s3

import (
	"context"
	"net/http"

	"github.com/mattjackson/basement/internal/gateway"
)

// Gateway is the S3 stub. Implements gateway.Gateway with no-op
// lifecycle + nil HTTPHandler. v2.0 will return an http.Handler
// from HTTPHandler() that mounts under /s3/ on the basement chi
// router (same pattern WebDAV uses today).
type Gateway struct{}

// New returns a fresh S3 gateway stub.
func New() *Gateway { return &Gateway{} }

func (g *Gateway) Name() string        { return "s3" }
func (g *Gateway) DisplayName() string { return "S3 API" }
func (g *Gateway) Description() string {
	return "Native S3 endpoint with SigV4 — every S3 SDK becomes a first-class basement client."
}

// Capabilities advertises the S3 surface. SigV4 is the headline
// auth method; Basic + Bearer don't apply at the S3 wire (though
// the FE may surface "use a service account" as the workflow).
func (g *Gateway) Capabilities() gateway.Capabilities {
	return gateway.Capabilities{
		Read:      true,
		Write:     true,
		Delete:    true,
		Move:      true,
		SigV4Auth: true,
	}
}

func (g *Gateway) Status() gateway.Status         { return gateway.Status{Running: false} }
func (g *Gateway) Implemented() bool              { return false }
func (g *Gateway) Start(_ context.Context) error  { return nil }
func (g *Gateway) Stop(_ context.Context) error   { return nil }
func (g *Gateway) HTTPHandler() http.Handler      { return nil }
func (g *Gateway) ListenAddress() string          { return "" }
