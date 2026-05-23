// Package nfs is the NFS gateway stub (v1.9.0c).
//
// Why a stub: NFS v4 has decent pure-Go server libraries (e.g.
// go-nfs) but the integration with basement's identity surface
// (mapping NFSv4 user IDs to basement users) is the v1.10 cycle's
// work. Until then the gateway sits in the registry so the
// /admin/gateways UI can render it next to WebDAV with a "coming
// soon" badge.
//
// Why we care: NFS is the lingua franca of Linux + most NAS
// appliances; an NFS export of a basement bucket is the cleanest
// path to "mount this from a Linux host without webdav-fuse".

package nfs

import (
	"context"
	"net/http"

	"github.com/mattjackson/basement/internal/gateway"
)

// Gateway is the NFS stub. Implements gateway.Gateway with no-op
// lifecycle + nil HTTPHandler.
type Gateway struct{}

// New returns a fresh NFS gateway stub.
func New() *Gateway { return &Gateway{} }

func (g *Gateway) Name() string        { return "nfs" }
func (g *Gateway) DisplayName() string { return "NFS" }
func (g *Gateway) Description() string {
	return "Network File System exports for Linux + NAS clients. Pure-Go NFSv4 server integration lands in v1.10."
}

// Capabilities advertises what NFS would do when implemented.
// Notably NO Move (NFS RENAME maps onto our CopyObject + Delete,
// but the gateway is read+write+delete first).
func (g *Gateway) Capabilities() gateway.Capabilities {
	return gateway.Capabilities{
		Read:   true,
		Write:  true,
		Delete: true,
		// NFS auth is its own world (AUTH_SYS, RPCSEC_GSS); none of
		// our HTTP-tier auth modes apply directly. The UI renders
		// "—" for auth methods when nothing is set.
	}
}

func (g *Gateway) Status() gateway.Status         { return gateway.Status{Running: false} }
func (g *Gateway) Implemented() bool              { return false }
func (g *Gateway) Start(_ context.Context) error  { return nil }
func (g *Gateway) Stop(_ context.Context) error   { return nil }
func (g *Gateway) HTTPHandler() http.Handler      { return nil }
func (g *Gateway) ListenAddress() string          { return "" }
