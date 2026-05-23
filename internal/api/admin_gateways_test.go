// Package api: tests for GET /api/v1/admin/gateways (v1.9.0c).
//
// Coverage: returns the registry roster in alphabetical order, marks
// the WebDAV row enabled when org caps say so, marks stubs with
// Implemented=false, and returns 503 GATEWAYS_NOT_WIRED when the
// registry hasn't been wired.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/gateway"
	"github.com/mattjackson/basement/internal/gateway/ftp"
	"github.com/mattjackson/basement/internal/gateway/nfs"
	"github.com/mattjackson/basement/internal/gateway/s3"
	"github.com/mattjackson/basement/internal/gateway/smb"
	"github.com/mattjackson/basement/internal/store"
)

// fakeGatewayForAPI is a minimal Gateway used to control the wire
// shape this test asserts. Lives in this package so the admin
// handler test can swap in a known impl without depending on the
// real WebDAV gateway (which would pull a Backend mock through it).
type fakeGatewayForAPI struct {
	name string
}

func (f *fakeGatewayForAPI) Name() string        { return f.name }
func (f *fakeGatewayForAPI) DisplayName() string { return "Fake " + f.name }
func (f *fakeGatewayForAPI) Description() string { return "fake gateway for tests" }
func (f *fakeGatewayForAPI) Capabilities() gateway.Capabilities {
	return gateway.Capabilities{Read: true}
}
func (f *fakeGatewayForAPI) Status() gateway.Status              { return gateway.Status{Running: true} }
func (f *fakeGatewayForAPI) Implemented() bool                   { return true }
func (f *fakeGatewayForAPI) Start(_ context.Context) error       { return nil }
func (f *fakeGatewayForAPI) Stop(_ context.Context) error        { return nil }
func (f *fakeGatewayForAPI) HTTPHandler() http.Handler           { return nil }
func (f *fakeGatewayForAPI) ListenAddress() string               { return "" }

func TestListGatewaysHandler_ReturnsRoster(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)

	reg := gateway.New()
	if err := reg.Register(&fakeGatewayForAPI{name: "webdav"}); err != nil {
		t.Fatalf("register webdav: %v", err)
	}
	for _, g := range []gateway.Gateway{smb.New(), nfs.New(), ftp.New(), s3.New()} {
		if err := reg.Register(g); err != nil {
			t.Fatalf("register %s: %v", g.Name(), err)
		}
	}
	srv.SetGatewayRegistry(reg)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/gateways")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out []gatewayResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if len(out) != 5 {
		t.Fatalf("want 5 gateways, got %d: %+v", len(out), out)
	}
	// All five registered, sorted alphabetically by Name(): ftp, nfs, s3, smb, webdav.
	wantNames := []string{"ftp", "nfs", "s3", "smb", "webdav"}
	for i, want := range wantNames {
		if out[i].Name != want {
			t.Errorf("out[%d].Name: want %q got %q", i, want, out[i].Name)
		}
	}
	// Stubs report Implemented=false; the fake webdav reports true.
	for _, row := range out {
		switch row.Name {
		case "webdav":
			if !row.Implemented {
				t.Errorf("webdav: want Implemented=true")
			}
			// fresh store: default OrgCapabilities has WebDAV enabled.
			if !row.Enabled {
				t.Errorf("webdav: want Enabled=true on a fresh org_capabilities default")
			}
		case "smb", "nfs", "ftp", "s3":
			if row.Implemented {
				t.Errorf("%s: want Implemented=false (stub)", row.Name)
			}
			if row.Enabled {
				t.Errorf("%s: want Enabled=false (stub has no org caps toggle today)", row.Name)
			}
		}
	}
}

func TestListGatewaysHandler_NoRegistry_503(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)
	// SetGatewayRegistry deliberately NOT called.

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/gateways")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error.Code != "GATEWAYS_NOT_WIRED" {
		t.Errorf("error code: want GATEWAYS_NOT_WIRED, got %s", er.Error.Code)
	}
}

func TestListGatewaysHandler_RequiresUIAdmin(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)
	srv.SetGatewayRegistry(gateway.New())

	// Non-admin token must be rejected by the uiAdminG middleware
	// before the handler runs.
	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/gateways")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Errorf("status=%d body=%s; want non-200 (uiAdmin gated)", rr.Code, rr.Body.String())
	}
}
