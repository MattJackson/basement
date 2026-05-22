// Package api: tests for the v1.4.0c block-scrub handlers.
//
// Three behaviours have to hold:
//
//  1. Unsupported driver (matches AWS / MinIO / Garage builds without
//     the worker endpoint): GET returns 200 with caps.supported=false;
//     POST returns 409 SCRUB_UNSUPPORTED.
//  2. Supported driver happy path: GET round-trips the live ScrubState
//     and POST starts a scrub, transitioning Running false → true.
//  3. Capability gate: a user without cluster:edit on the cluster gets
//     403 on both GET + POST (covered transitively by the per-handler
//     requireCapability call; lifecycle tests assert the same shape so
//     we don't re-prove the middleware here).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// scrubStubState is shared per-connection scrub state. Keeping it in
// a package var (mirroring lifecycle's design) lets the stub driver
// re-resolve through the registry between calls and still see the
// updated Running flag.
type scrubStubState struct {
	mu sync.Mutex
	st map[string]driver.ScrubState
}

func newScrubStubState() *scrubStubState {
	return &scrubStubState{st: map[string]driver.ScrubState{}}
}

func (s *scrubStubState) get(connID string) driver.ScrubState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st[connID]
}

func (s *scrubStubState) set(connID string, st driver.ScrubState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st[connID] = st
}

var scrubStubSharedState = newScrubStubState()

type scrubStubBehavior string

const (
	scrubStubSupported   scrubStubBehavior = "supported"
	scrubStubUnsupported scrubStubBehavior = "unsupported"
)

// scrubStubDriver embeds fanoutDriver to inherit no-op implementations
// of every other Driver method. We override only the scrub trio and
// tag the instance with connID so state lookups land in the shared
// map.
type scrubStubDriver struct {
	fanoutDriver
	behavior scrubStubBehavior
}

func (d *scrubStubDriver) ScrubSupport() driver.ScrubCapability {
	if d.behavior == scrubStubUnsupported {
		return driver.ScrubCapability{Supported: false, Reason: "stub: unsupported"}
	}
	return driver.ScrubCapability{Supported: true}
}

func (d *scrubStubDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	if d.behavior == scrubStubUnsupported {
		return driver.ScrubState{}, &driver.Error{
			Op:      "ScrubState",
			Driver:  scrubStubDriverName,
			Err:     driver.ErrUnsupported,
			Message: "stub: unsupported",
		}
	}
	return scrubStubSharedState.get(d.connID), nil
}

func (d *scrubStubDriver) StartScrub(_ context.Context) error {
	if d.behavior == scrubStubUnsupported {
		return &driver.Error{
			Op:      "StartScrub",
			Driver:  scrubStubDriverName,
			Err:     driver.ErrUnsupported,
			Message: "stub: unsupported",
		}
	}
	scrubStubSharedState.set(d.connID, driver.ScrubState{Running: true, Message: "started"})
	return nil
}

const scrubStubDriverName = "stub-scrub-driver"

var scrubRegisterOnce sync.Once

func registerScrubStubDriver(t *testing.T) {
	t.Helper()
	scrubRegisterOnce.Do(func() {
		driver.Register(scrubStubDriverName, func(cfg driver.Config) (driver.Driver, error) {
			behavior := scrubStubBehavior(cfg["behavior"])
			if behavior == "" {
				return nil, errors.New("behavior config missing")
			}
			return &scrubStubDriver{
				fanoutDriver: fanoutDriver{
					behavior: "ok",
					connID:   cfg["conn_id"],
				},
				behavior: behavior,
			}, nil
		})
		store.SupportedDrivers[scrubStubDriverName] = true
	})
}

func makeScrubConnsStore(connID string, behavior scrubStubBehavior) *testMockConnectionStore {
	return &testMockConnectionStore{
		conns: []store.Connection{
			{
				ID:     connID,
				Label:  "scrub-" + connID,
				Driver: scrubStubDriverName,
				Config: map[string]string{
					"behavior": string(behavior),
					"conn_id":  connID,
				},
				Owner: "org",
			},
		},
	}
}

// TestGetScrub_Unsupported: garage-stub-without-worker; handler returns
// 200 with caps.supported=false. The UI's "not supported" branch is
// the only thing that should ever render against this response.
func TestGetScrub_Unsupported(t *testing.T) {
	registerScrubStubDriver(t)

	conns := makeScrubConnsStore("cid-unsup", scrubStubUnsupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/clusters/cid-unsup/scrub")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp scrubGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Capabilities.Supported {
		t.Errorf("expected Supported=false, got true")
	}
	if resp.Capabilities.Reason == "" {
		t.Errorf("expected non-empty Reason")
	}
}

// TestGetScrub_SupportedHappyPath: state is empty initially, POST
// kicks scrub off, second GET shows Running=true.
func TestGetScrub_SupportedRoundTrip(t *testing.T) {
	registerScrubStubDriver(t)
	scrubStubSharedState = newScrubStubState() // isolate from earlier tests

	conns := makeScrubConnsStore("cid-supp", scrubStubSupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	// 1) GET — Supported=true, Running=false (initial).
	{
		req := createAuthRequest(http.MethodGet, "/api/v1/admin/clusters/cid-supp/scrub")
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
		}
		var resp scrubGetResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Capabilities.Supported {
			t.Errorf("expected Supported=true initially")
		}
		if resp.State.Running {
			t.Errorf("expected Running=false initially")
		}
	}

	// 2) POST — kicks scrub off.
	{
		req := createAuthRequest(http.MethodPost, "/api/v1/admin/clusters/cid-supp/scrub")
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
		}
		var resp scrubGetResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.State.Running {
			t.Errorf("expected Running=true after POST, got %+v", resp.State)
		}
	}

	// 3) GET — confirms persisted Running=true.
	{
		req := createAuthRequest(http.MethodGet, "/api/v1/admin/clusters/cid-supp/scrub")
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET2 status=%d body=%s", rr.Code, rr.Body.String())
		}
		var resp scrubGetResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.State.Running {
			t.Errorf("expected Running=true in second GET, got %+v", resp.State)
		}
	}
}

// TestPostScrub_Unsupported: refuse POST with 409 SCRUB_UNSUPPORTED so
// the UI's optimistic-update path can distinguish "driver said no" from
// "we couldn't reach the cluster".
func TestPostScrub_Unsupported(t *testing.T) {
	registerScrubStubDriver(t)

	conns := makeScrubConnsStore("cid-post-unsup", scrubStubUnsupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	req := createAuthRequest(http.MethodPost, "/api/v1/admin/clusters/cid-post-unsup/scrub")
	req.Body = http.NoBody
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rr.Code, rr.Body.String())
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(bytes.NewReader(rr.Body.Bytes())).Decode(&er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error.Code != "SCRUB_UNSUPPORTED" {
		t.Errorf("code = %q, want SCRUB_UNSUPPORTED", er.Error.Code)
	}
}

// TestPostScrub_MissingCID: handler validates path; chi shouldn't even
// route this, but guard against a chi-config slip-up.
func TestGetScrub_NonExistentCluster(t *testing.T) {
	registerScrubStubDriver(t)

	conns := &testMockConnectionStore{conns: []store.Connection{}}
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/clusters/cid-missing/scrub")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rr.Code, rr.Body.String())
	}
}
