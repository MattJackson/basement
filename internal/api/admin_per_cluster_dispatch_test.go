// Package api: v1.11.0.3 regression tests for the per-cluster-handler
// bug class fixed in v1.11.0.2 (buckets) + v1.11.0.3 (keys).
//
// The bug: handlers mounted at /admin/clusters/{cid}/... called the
// global s.drv default driver instead of resolving the per-cluster
// driver via s.reg.For(ctx, cid). In multi-cluster deployments, every
// per-cluster write silently landed on whichever cluster s.drv pointed
// at — caught when matthew added 10 Garage v2 clusters and CreateBucket
// reported success while Garage v2 never saw the request.
//
// Test strategy: register a spy driver factory whose instances record
// which connID served each method call. Build two connections (two
// distinct cids) pointing at the spy. Drive the live router at
// /admin/clusters/{cid}/... for each cid and assert each call landed
// on the spy bound to that cid — NOT a single shared instance.
//
// One dispatch test per affected file is enough — the per-driver
// counter is the canary. A regression that flips back to s.drv would
// show one of the two cids with a zero counter (s.drv == nil in tests
// so it would also nil-deref, but the assertion catches it cleanly).
package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"context"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// dispatchSpy records (connID, op) tuples for cross-instance
// assertions. Spy driver instances share one global counter; tests
// distinguish themselves via unique connIDs.
type dispatchSpy struct {
	mu    sync.Mutex
	calls map[string]int // key: "<connID>|<op>"
}

func (s *dispatchSpy) record(connID, op string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = make(map[string]int)
	}
	s.calls[connID+"|"+op]++
}

func (s *dispatchSpy) count(connID, op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[connID+"|"+op]
}

var spyState = &dispatchSpy{}

// spyDriver wraps fanoutDriver (full Driver surface) and overrides
// the bucket + key methods to record (connID, op) tuples before
// delegating. fanoutDriver's "ok" behavior gives the rest of the
// interface a happy-path default so handlers don't trip on side
// effects we aren't asserting.
type spyDriver struct {
	fanoutDriver
}

func (d *spyDriver) GetBucket(ctx context.Context, id string) (driver.Bucket, error) {
	spyState.record(d.connID, "GetBucket")
	return d.fanoutDriver.GetBucket(ctx, id)
}
func (d *spyDriver) CreateBucket(ctx context.Context, spec driver.BucketSpec) (driver.Bucket, error) {
	spyState.record(d.connID, "CreateBucket")
	// Echo the connID into the response so a wrong-driver routing
	// also surfaces visually in test failures, not just in counts.
	return driver.Bucket{ID: "b-" + d.connID + "-new", Aliases: []string{spec.Alias}}, nil
}
func (d *spyDriver) GetKey(ctx context.Context, id string) (driver.Key, error) {
	spyState.record(d.connID, "GetKey")
	return driver.Key{ID: id, Name: "k-" + d.connID}, nil
}

const spyDriverName = "stub-spy-driver"

var spyRegisterOnce sync.Once

func registerSpyDriver(t *testing.T) {
	t.Helper()
	spyRegisterOnce.Do(func() {
		driver.Register(spyDriverName, func(cfg driver.Config) (driver.Driver, error) {
			return &spyDriver{
				fanoutDriver: fanoutDriver{
					behavior: "ok",
					connID:   cfg["conn_id"],
				},
			}, nil
		})
		store.SupportedDrivers[spyDriverName] = true
	})
}

func makeSpyConnsStore(ids ...string) *testMockConnectionStore {
	conns := make([]store.Connection, 0, len(ids))
	for _, id := range ids {
		conns = append(conns, store.Connection{
			ID:     id,
			Label:  "spy-" + id,
			Driver: spyDriverName,
			Config: map[string]string{"conn_id": id},
			Owner:  "org",
		})
	}
	return &testMockConnectionStore{conns: conns}
}

// TestPerClusterDispatch_Buckets pins the v1.11.0.2 fix: GET
// /admin/clusters/{cid}/buckets/{id} routes to the cid-specific
// driver. The pre-fix bug hit s.drv (a single shared instance) so
// both cids would have recorded on one driver — here we assert each
// cid's spy was hit exactly once.
func TestPerClusterDispatch_Buckets(t *testing.T) {
	registerSpyDriver(t)
	conns := makeSpyConnsStore("bktA", "bktB")
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	beforeA := spyState.count("bktA", "GetBucket")
	beforeB := spyState.count("bktB", "GetBucket")

	for _, cid := range []string{"bktA", "bktB"} {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/admin/clusters/"+cid+"/buckets/some-bucket-id", nil)
		req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("cid=%s status=%d body=%s", cid, rr.Code, rr.Body.String())
		}
	}

	if got := spyState.count("bktA", "GetBucket") - beforeA; got != 1 {
		t.Errorf("GetBucket count for bktA: got delta=%d, want 1", got)
	}
	if got := spyState.count("bktB", "GetBucket") - beforeB; got != 1 {
		t.Errorf("GetBucket count for bktB: got delta=%d, want 1 — handler likely used s.drv instead of cid-specific driver", got)
	}
}

// TestPerClusterDispatch_Keys pins the v1.11.0.3 fix: GET
// /admin/clusters/{cid}/keys/{id} routes to the cid-specific driver.
// Same canary pattern as the buckets test above.
func TestPerClusterDispatch_Keys(t *testing.T) {
	registerSpyDriver(t)
	conns := makeSpyConnsStore("keyA", "keyB")
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	beforeA := spyState.count("keyA", "GetKey")
	beforeB := spyState.count("keyB", "GetKey")

	for _, cid := range []string{"keyA", "keyB"} {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/admin/clusters/"+cid+"/keys/some-key-id", nil)
		req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("cid=%s status=%d body=%s", cid, rr.Code, rr.Body.String())
		}
	}

	if got := spyState.count("keyA", "GetKey") - beforeA; got != 1 {
		t.Errorf("GetKey count for keyA: got delta=%d, want 1", got)
	}
	if got := spyState.count("keyB", "GetKey") - beforeB; got != 1 {
		t.Errorf("GetKey count for keyB: got delta=%d, want 1 — handler likely used s.drv instead of cid-specific driver", got)
	}
}
