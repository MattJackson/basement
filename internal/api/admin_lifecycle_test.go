// Package api: tests for the v0.9.0i bucket lifecycle handlers.
//
// Three behaviours have to hold:
//
//  1. Unsupported driver (matches Garage v1 stub): GET returns 200
//     with capabilities.supported=false + an empty rules array; the
//     UI gates the editor on that flag.
//  2. Supported driver happy path: GET round-trips an existing rule
//     set, PUT validates + writes + returns the persisted policy.
//  3. Capability validation: PUT rejects rules carrying fields the
//     driver doesn't advertise (e.g. TransitionDays on a Garage-v2
//     driver that doesn't support tier transitions).
//
// Tests share a freshly-registered stub driver via init-once so
// connection records can point at it; the registry resolves the
// driver and the test seeds per-bucket lifecycle state via the
// driver's overridable hooks.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// lifecycleStubBehavior is a per-config dial telling the stub how to
// respond. "supported" exposes the full capability surface; "unsupported"
// reports Supported=false (matches Garage v1).
type lifecycleStubBehavior string

const (
	lifecycleStubSupported   lifecycleStubBehavior = "supported"
	lifecycleStubUnsupported lifecycleStubBehavior = "unsupported"
)

// lifecycleStubState is the in-memory bucket lifecycle store shared
// across all instances of lifecycleStubDriver for a given connID.
// Keyed by (connID, bucketID); a missing entry means "no rules".
// Tests pre-seed via lifecycleStubSeed; PUT writes here.
type lifecycleStubState struct {
	mu    sync.Mutex
	rules map[string][]driver.LifecycleRule
}

func newLifecycleStubState() *lifecycleStubState {
	return &lifecycleStubState{rules: map[string][]driver.LifecycleRule{}}
}

func (s *lifecycleStubState) set(connID, bid string, rules []driver.LifecycleRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[connID+"|"+bid] = rules
}

func (s *lifecycleStubState) get(connID, bid string) []driver.LifecycleRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rules[connID+"|"+bid]
}

// Single shared state across all stub drivers — keyed by connID so
// different test cases stay isolated.
var lifecycleStubSharedState = newLifecycleStubState()

// lifecycleStubDriver embeds fanoutDriver so it inherits the no-op
// implementations of every other Driver method (ListBuckets, etc.).
// We override only the lifecycle trio + tag the instance with its
// connID so the state lookup works.
type lifecycleStubDriver struct {
	fanoutDriver
	behavior lifecycleStubBehavior
	caps     driver.LifecycleCapabilities
}

func (d *lifecycleStubDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return d.caps
}

func (d *lifecycleStubDriver) GetLifecycle(_ context.Context, bid string) ([]driver.LifecycleRule, error) {
	if d.behavior == lifecycleStubUnsupported {
		return nil, &driver.Error{
			Op:      "GetLifecycle",
			Driver:  lifecycleStubDriverName,
			Err:     driver.ErrUnsupported,
			Message: "not implemented",
		}
	}
	return lifecycleStubSharedState.get(d.connID, bid), nil
}

func (d *lifecycleStubDriver) PutLifecycle(_ context.Context, bid string, rules []driver.LifecycleRule) error {
	if d.behavior == lifecycleStubUnsupported {
		return &driver.Error{
			Op:      "PutLifecycle",
			Driver:  lifecycleStubDriverName,
			Err:     driver.ErrUnsupported,
			Message: "not implemented",
		}
	}
	lifecycleStubSharedState.set(d.connID, bid, rules)
	return nil
}

const lifecycleStubDriverName = "stub-lifecycle-driver"

var lifecycleRegisterOnce sync.Once

func registerLifecycleStubDriver(t *testing.T) {
	t.Helper()
	lifecycleRegisterOnce.Do(func() {
		driver.Register(lifecycleStubDriverName, func(cfg driver.Config) (driver.Driver, error) {
			behavior := lifecycleStubBehavior(cfg["behavior"])
			if behavior == "" {
				return nil, errors.New("behavior config missing")
			}
			d := &lifecycleStubDriver{
				fanoutDriver: fanoutDriver{
					behavior: "ok", // upstream methods (ListBuckets etc) stay happy
					connID:   cfg["conn_id"],
				},
				behavior: behavior,
			}
			switch behavior {
			case lifecycleStubSupported:
				d.caps = driver.LifecycleCapabilities{
					Supported:          true,
					Expiration:         true,
					Transition:         true,
					TransitionTiers:    []string{"STANDARD_IA", "GLACIER"},
					NoncurrentDays:     true,
					AbortMultipartDays: true,
				}
			case lifecycleStubUnsupported:
				d.caps = driver.LifecycleCapabilities{Supported: false}
			}
			return d, nil
		})
		store.SupportedDrivers[lifecycleStubDriverName] = true
	})
}

// makeLifecycleConnsStore seeds a single Connection with the requested
// behavior so tests can hit the registry path.
func makeLifecycleConnsStore(connID string, behavior lifecycleStubBehavior) *testMockConnectionStore {
	return &testMockConnectionStore{
		conns: []store.Connection{
			{
				ID:     connID,
				Label:  "lifecycle-" + connID,
				Driver: lifecycleStubDriverName,
				Config: map[string]string{
					"behavior": string(behavior),
					"conn_id":  connID,
				},
				Owner: "org",
			},
		},
	}
}

// TestGetLifecycle_UnsupportedDriver: garage-v1-shaped driver reports
// Supported=false; handler returns 200 with empty rules. The UI's
// "not supported" branch is the only thing that should ever fire
// here — so the response body must carry caps.Supported=false.
func TestGetLifecycle_UnsupportedDriver(t *testing.T) {
	registerLifecycleStubDriver(t)

	conns := makeLifecycleConnsStore("cid-unsup", lifecycleStubUnsupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/clusters/cid-unsup/buckets/bid-x/lifecycle")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp lifecycleGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Capabilities.Supported {
		t.Fatalf("expected Supported=false, got true")
	}
	if len(resp.Rules) != 0 {
		t.Fatalf("expected empty rules, got %d", len(resp.Rules))
	}
}

// TestGetLifecycle_HappyPath: supported driver with pre-seeded rules
// returns them verbatim alongside the capability surface.
func TestGetLifecycle_HappyPath(t *testing.T) {
	registerLifecycleStubDriver(t)

	conns := makeLifecycleConnsStore("cid-happy", lifecycleStubSupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	days := 30
	lifecycleStubSharedState.set("cid-happy", "bid-1", []driver.LifecycleRule{
		{ID: "rule-1", Status: "Enabled", Prefix: "logs/", ExpirationDays: &days},
	})

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/clusters/cid-happy/buckets/bid-1/lifecycle")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp lifecycleGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Capabilities.Supported {
		t.Fatalf("expected Supported=true, got false")
	}
	if len(resp.Rules) != 1 || resp.Rules[0].ID != "rule-1" ||
		resp.Rules[0].Status != "Enabled" || resp.Rules[0].Prefix != "logs/" ||
		resp.Rules[0].ExpirationDays == nil || *resp.Rules[0].ExpirationDays != 30 {
		t.Fatalf("unexpected rules: %+v", resp.Rules)
	}
}

// TestPutLifecycle_HappyPath: supported driver accepts a write, the
// state is persisted to the stub store, and the response echoes the
// persisted policy with the driver's capability snapshot.
func TestPutLifecycle_HappyPath(t *testing.T) {
	registerLifecycleStubDriver(t)

	conns := makeLifecycleConnsStore("cid-put", lifecycleStubSupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	days := 7
	tier := "STANDARD_IA"
	body := lifecyclePutRequest{
		Rules: []driver.LifecycleRule{
			{ID: "rule-cold", Status: "Enabled", Prefix: "raw/", TransitionDays: &days, TransitionTier: tier},
		},
	}
	bs, _ := json.Marshal(body)

	req := createAuthRequest(http.MethodPut, "/api/v1/admin/clusters/cid-put/buckets/bid-2/lifecycle")
	req.Body = nil
	req = req.WithContext(req.Context())
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/admin/clusters/cid-put/buckets/bid-2/lifecycle", bytes.NewReader(bs))
	req2.Header.Set("Content-Type", "application/json")
	for _, c := range req.Cookies() {
		req2.AddCookie(c)
	}

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req2)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp lifecycleGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 1 || resp.Rules[0].TransitionTier != tier {
		t.Fatalf("unexpected rules: %+v", resp.Rules)
	}

	// Stub state should reflect the write.
	persisted := lifecycleStubSharedState.get("cid-put", "bid-2")
	if len(persisted) != 1 || persisted[0].ID != "rule-cold" {
		t.Fatalf("expected one persisted rule, got %+v", persisted)
	}
}

// TestPutLifecycle_RejectsUnsupportedFields: PUT MUST reject rule
// fields the driver doesn't advertise. We give the stub a fresh
// capability profile (Expiration only — no Transition, no Noncurrent,
// no AbortMpu) and confirm each forbidden field flunks with 400.
func TestPutLifecycle_RejectsUnsupportedFields(t *testing.T) {
	registerLifecycleStubDriver(t)

	// Use a custom-built connection store so we can swap caps mid-test.
	connID := "cid-narrow"
	driver.Register("stub-narrow-lifecycle", func(cfg driver.Config) (driver.Driver, error) {
		return &lifecycleStubDriver{
			fanoutDriver: fanoutDriver{connID: cfg["conn_id"]},
			behavior:     lifecycleStubSupported,
			caps: driver.LifecycleCapabilities{
				Supported:  true,
				Expiration: true,
				// All other axes false.
			},
		}, nil
	})
	store.SupportedDrivers["stub-narrow-lifecycle"] = true

	conns := &testMockConnectionStore{
		conns: []store.Connection{{
			ID:     connID,
			Label:  "narrow",
			Driver: "stub-narrow-lifecycle",
			Config: map[string]string{"conn_id": connID},
			Owner:  "org",
		}},
	}
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	days := 5
	cases := []struct {
		name      string
		rule      driver.LifecycleRule
		wantCode  string
	}{
		{"transition_rejected", driver.LifecycleRule{Status: "Enabled", TransitionDays: &days}, "TRANSITION_UNSUPPORTED"},
		{"noncurrent_rejected", driver.LifecycleRule{Status: "Enabled", NoncurrentDays: &days}, "NONCURRENT_UNSUPPORTED"},
		{"abort_mpu_rejected", driver.LifecycleRule{Status: "Enabled", AbortMultipartDays: &days}, "ABORT_MPU_UNSUPPORTED"},
		{"bad_status_rejected", driver.LifecycleRule{Status: "On"}, "INVALID_RULE_STATUS"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			body := lifecyclePutRequest{Rules: []driver.LifecycleRule{tc.rule}}
			bs, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPut,
				fmt.Sprintf("/api/v1/admin/clusters/%s/buckets/bid/lifecycle", connID),
				bytes.NewReader(bs))
			req.Header.Set("Content-Type", "application/json")
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

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			var er ErrorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &er); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if er.Error.Code != tc.wantCode {
				t.Fatalf("expected code=%s, got %s (body=%s)", tc.wantCode, er.Error.Code, rr.Body.String())
			}
		})
	}
}

// TestPutLifecycle_ClearWithEmptyRules: a PUT with an empty rules
// slice clears the policy — the stub state for the bucket goes back
// to empty after the call.
func TestPutLifecycle_ClearWithEmptyRules(t *testing.T) {
	registerLifecycleStubDriver(t)

	connID := "cid-clear"
	conns := makeLifecycleConnsStore(connID, lifecycleStubSupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	days := 9
	lifecycleStubSharedState.set(connID, "bid-clr", []driver.LifecycleRule{
		{ID: "pre", Status: "Enabled", ExpirationDays: &days},
	})

	bs, _ := json.Marshal(lifecyclePutRequest{Rules: []driver.LifecycleRule{}})
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/v1/admin/clusters/%s/buckets/bid-clr/lifecycle", connID),
		bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
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
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	persisted := lifecycleStubSharedState.get(connID, "bid-clr")
	if len(persisted) != 0 {
		t.Fatalf("expected empty rules after clear, got %+v", persisted)
	}
}

// TestPutLifecycle_OnUnsupportedDriver_Returns409: even though the
// uiAdmin middleware would let the request through, the handler
// must refuse with 409 LIFECYCLE_UNSUPPORTED when caps.Supported=false.
func TestPutLifecycle_OnUnsupportedDriver_Returns409(t *testing.T) {
	registerLifecycleStubDriver(t)

	connID := "cid-refuse"
	conns := makeLifecycleConnsStore(connID, lifecycleStubUnsupported)
	reg := driver.NewRegistry(conns)
	srv := New(newTestConfig(), nil, conns, nil, reg)

	bs, _ := json.Marshal(lifecyclePutRequest{Rules: []driver.LifecycleRule{
		{Status: "Enabled"},
	}})
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/v1/admin/clusters/%s/buckets/bid-rf/lifecycle", connID),
		bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
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

	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error.Code != "LIFECYCLE_UNSUPPORTED" {
		t.Fatalf("expected LIFECYCLE_UNSUPPORTED, got %s", er.Error.Code)
	}
}
