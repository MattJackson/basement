package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// bridgeTestEnv stands up an API server with the user-region
// keychain wired plus a stubbed driver registry so a Garage admin
// Connection at a given endpoint is reachable via reg.For without
// depending on the real driver factories (which live in
// cmd/basement-server's blank-imports and aren't compiled into this
// package's test binary).
type bridgeTestEnv struct {
	srv       *Server
	conns     *testMockConnectionStore
	adminDrv  *testMockDriver
	regionDrv *regionMockDriver
	cleanup   func()
}

// bridgeFactoryCurrent is the driver instance the test-only
// store.DriverGarageV1 / store.DriverGarage factories hand back to
// the registry. The bridge's driver-type gate checks for
// store.DriverGarageV1 / store.DriverGarage literally, so registering
// stubs under those names lets the bridge pick up the test driver
// without us extending the production allowlist for tests.
//
// The cmd/basement-server main package registers the real factories at
// the same names via blank-import; in the internal/api test binary
// those imports don't fire, so the registrations below win
// uncontested.
var bridgeFactoryCurrent driver.Driver

func init() {
	driver.Register(store.DriverGarageV1, func(_ driver.Config) (driver.Driver, error) {
		if bridgeFactoryCurrent == nil {
			return &testMockDriver{}, nil
		}
		return bridgeFactoryCurrent, nil
	})
	driver.Register(store.DriverGarage, func(_ driver.Config) (driver.Driver, error) {
		if bridgeFactoryCurrent == nil {
			return &testMockDriver{}, nil
		}
		return bridgeFactoryCurrent, nil
	})
	driver.Register(store.DriverAWSS3, func(_ driver.Config) (driver.Driver, error) {
		return &testMockDriver{}, nil
	})
	driver.Register(store.DriverMinio, func(_ driver.Config) (driver.Driver, error) {
		return &testMockDriver{}, nil
	})
}

func newBridgeTestEnv(t *testing.T) *bridgeTestEnv {
	t.Helper()

	tmp, err := os.MkdirTemp("", "bridge-test-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		cleanup()
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireUserRegions(cfg.JWT.Secret); err != nil {
		cleanup()
		t.Fatalf("WireUserRegions: %v", err)
	}

	conns := &testMockConnectionStore{}
	adminDrv := &testMockDriver{}
	regionDrv := newRegionMockDriver()

	reg := driver.NewRegistry(conns)
	reg.SetUserRegionsStore(st.UserRegions())
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		return regionDrv, nil
	})

	srv := New(cfg, st, conns, nil, reg)
	srv.SetAuditLogger(&memAuditLogger{})

	return &bridgeTestEnv{
		srv:       srv,
		conns:     conns,
		adminDrv:  adminDrv,
		regionDrv: regionDrv,
		cleanup:   cleanup,
	}
}

func (e *bridgeTestEnv) bindAdminDriver() {
	bridgeFactoryCurrent = e.adminDrv
}

func (e *bridgeTestEnv) seedAdminConnection(id, driverName, endpointKey, endpoint string) {
	e.conns.conns = append(e.conns.conns, store.Connection{
		ID:        id,
		Label:     "admin-" + id,
		Driver:    driverName,
		Config:    map[string]string{endpointKey: endpoint},
		Owner:     "org",
		CreatedAt: time.Now().UTC(),
	})
}

func (e *bridgeTestEnv) createRegion(t *testing.T, endpoint string) string {
	t.Helper()
	body := map[string]string{
		"alias":       "home",
		"endpoint":    endpoint,
		"accessKeyId": "GK_user",
		"secretKey":   "user-secret",
		"region":      "garage",
	}
	rr := httptest.NewRecorder()
	e.srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create region: %d (%s)", rr.Code, rr.Body.String())
	}
	var created userRegionResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)
	return created.ID
}

// TestRegionBuckets_GarageBridge_ReturnsAdminList covers the
// happy-path bridge: a Garage admin Connection at the same endpoint
// exists, the admin's ListBuckets returns [lsi, cheshire], the
// user's S3 key can reach both — the user sees both.
func TestRegionBuckets_GarageBridge_ReturnsAdminList(t *testing.T) {
	env := newBridgeTestEnv(t)
	defer env.cleanup()
	env.bindAdminDriver()
	defer func() { bridgeFactoryCurrent = nil }()

	endpoint := "https://s3.basement.pq.io"
	env.seedAdminConnection("conn-garage-prod", store.DriverGarageV1, "s3_endpoint", endpoint)

	env.adminDrv.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{
			{ID: "lsi-id", Aliases: []string{"lsi"}},
			{ID: "cheshire-id", Aliases: []string{"cheshire"}},
		}, nil
	}
	// User key can reach every bucket — ListObjects(limit=1) succeeds.
	env.regionDrv.testMockDriver.listObjectsFunc = func(_ context.Context, _, _, _ string, _ int) (driver.ObjectPage, error) {
		return driver.ObjectPage{}, nil
	}

	regionID := env.createRegion(t, endpoint)
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil))
	req.Header.Set("Content-Type", "application/json")
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("buckets: expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var got []driver.Bucket
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 buckets from admin bridge, got %d (%+v)", len(got), got)
	}
}

// TestRegionBuckets_GarageBridge_FiltersByUserKey verifies the
// intersection step: admin returns [lsi, cheshire], user's key can
// reach only lsi (cheshire ListObjects → 403); user sees only [lsi].
func TestRegionBuckets_GarageBridge_FiltersByUserKey(t *testing.T) {
	env := newBridgeTestEnv(t)
	defer env.cleanup()
	env.bindAdminDriver()
	defer func() { bridgeFactoryCurrent = nil }()

	endpoint := "https://s3.basement.pq.io"
	env.seedAdminConnection("conn-garage-prod", store.DriverGarageV1, "s3_endpoint", endpoint)

	env.adminDrv.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{
			{ID: "lsi-id", Aliases: []string{"lsi"}},
			{ID: "cheshire-id", Aliases: []string{"cheshire"}},
		}, nil
	}
	env.regionDrv.testMockDriver.listObjectsFunc = func(_ context.Context, bucket, _, _ string, _ int) (driver.ObjectPage, error) {
		if bucket == "cheshire" {
			return driver.ObjectPage{}, &driver.Error{Op: "ListObjects", Err: driver.ErrPermissionDenied}
		}
		return driver.ObjectPage{}, nil
	}

	regionID := env.createRegion(t, endpoint)
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil))
	req.Header.Set("Content-Type", "application/json")
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("buckets: %d (%s)", rr.Code, rr.Body.String())
	}
	var got []driver.Bucket
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].Aliases[0] != "lsi" {
		t.Errorf("expected only [lsi] after user-key intersection, got %+v", got)
	}
}

// TestRegionBuckets_NoAdminBridge_FallsThroughToUserDriver verifies
// the fallback: no matching admin Connection exists, so the user-tier
// driver's ListBuckets is used directly.
func TestRegionBuckets_NoAdminBridge_FallsThroughToUserDriver(t *testing.T) {
	env := newBridgeTestEnv(t)
	defer env.cleanup()

	env.regionDrv.testMockDriver.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{{ID: "user-side-only", Aliases: []string{"user-side-only"}}}, nil
	}

	regionID := env.createRegion(t, "https://s3.aws-style.example.com")
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil))
	req.Header.Set("Content-Type", "application/json")
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("buckets: %d (%s)", rr.Code, rr.Body.String())
	}
	var got []driver.Bucket
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].ID != "user-side-only" {
		t.Errorf("expected user-driver fallback list, got %+v", got)
	}
}

// TestRegionBuckets_NonGarageAdmin_SkipsBridge — an AWS S3 admin
// Connection at the same endpoint must NOT be bridged through (the
// user's own ListBuckets works on AWS S3 already).
func TestRegionBuckets_NonGarageAdmin_SkipsBridge(t *testing.T) {
	env := newBridgeTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.aws-style.example.com"
	env.seedAdminConnection("conn-aws", store.DriverAWSS3, "endpoint", endpoint)

	// If the bridge fired, this admin driver would be invoked — make
	// it fail loudly to catch regression.
	env.adminDrv.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		t.Errorf("admin bridge fired for AWS driver — should have been skipped")
		return nil, nil
	}
	env.regionDrv.testMockDriver.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{{ID: "user-aws", Aliases: []string{"user-aws"}}}, nil
	}

	regionID := env.createRegion(t, endpoint)
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil))
	req.Header.Set("Content-Type", "application/json")
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("buckets: %d (%s)", rr.Code, rr.Body.String())
	}
	var got []driver.Bucket
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].ID != "user-aws" {
		t.Errorf("expected user-driver path for AWS, got %+v", got)
	}
}

// TestRegionBuckets_BridgeEndpointCanonicalization — an admin
// Connection registered with a styled URL ("HTTPS://S3.PQ.IO:443/")
// matches a UserRegion whose endpoint was input differently
// ("https://s3.pq.io") because NormalizeEndpoint folds both to the
// same canonical form.
func TestRegionBuckets_BridgeEndpointCanonicalization(t *testing.T) {
	env := newBridgeTestEnv(t)
	defer env.cleanup()
	env.bindAdminDriver()
	defer func() { bridgeFactoryCurrent = nil }()

	env.seedAdminConnection("conn-garage-prod", store.DriverGarageV1, "s3_endpoint", "HTTPS://S3.PQ.IO:443/")

	env.adminDrv.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{{ID: "canonical", Aliases: []string{"canonical"}}}, nil
	}
	env.regionDrv.testMockDriver.listObjectsFunc = func(_ context.Context, _, _, _ string, _ int) (driver.ObjectPage, error) {
		return driver.ObjectPage{}, nil
	}

	regionID := env.createRegion(t, "https://s3.pq.io")
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil))
	req.Header.Set("Content-Type", "application/json")
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("buckets: %d (%s)", rr.Code, rr.Body.String())
	}
	var got []driver.Bucket
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].Aliases[0] != "canonical" {
		t.Errorf("expected canonical-form bridge to match, got %+v", got)
	}
}
