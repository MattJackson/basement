// Package ftp is the FTP / FTPS / SFTP gateway stub (v1.9.0c).
//
// Why a stub: FTP has decent pure-Go libraries (jlaffaye/ftp,
// goftp/server) but the wire is barely-encrypted and the v1.9.0c
// scope is about the abstraction layer, not new protocols. Stub
// registration keeps the UI honest about what's coming.
//
// Why we'd ever ship it: there's a long tail of legacy clients
// (media-server boxes, embedded devices) that only speak FTP.
// SFTP via golang.org/x/crypto/ssh is the more interesting target;
// we'd lump it under this gateway with a "preferred" badge for the
// SSH-tunneled variant.

package ftp

import (
	"context"
	"net/http"

	"github.com/mattjackson/basement/internal/gateway"
)

// Gateway is the FTP stub. Implements gateway.Gateway with no-op
// lifecycle + nil HTTPHandler.
type Gateway struct{}

// New returns a fresh FTP gateway stub.
func New() *Gateway { return &Gateway{} }

func (g *Gateway) Name() string        { return "ftp" }
func (g *Gateway) DisplayName() string { return "FTP / SFTP" }
func (g *Gateway) Description() string {
	return "File Transfer Protocol — FTP, FTPS, and SFTP variants for legacy + embedded clients."
}

func (g *Gateway) Capabilities() gateway.Capabilities {
	return gateway.Capabilities{
		Read:      true,
		Write:     true,
		Delete:    true,
		BasicAuth: true,
	}
}

func (g *Gateway) Status() gateway.Status         { return gateway.Status{Running: false} }
func (g *Gateway) Implemented() bool              { return false }
func (g *Gateway) Start(_ context.Context) error  { return nil }
func (g *Gateway) Stop(_ context.Context) error   { return nil }
func (g *Gateway) HTTPHandler() http.Handler      { return nil }
func (g *Gateway) ListenAddress() string          { return "" }
