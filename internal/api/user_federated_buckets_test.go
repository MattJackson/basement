// Package api: user-tier federation handler tests (v1.6.0c).
//
// Every test stands up a Server wired with:
//   - an in-memory federation store (federation.Open against a TempDir)
//   - a recording mock engine so EnsureLoop / RemoveLoop / TriggerNow
//     calls can be asserted without spinning up real per-federation
//     goroutines
//   - a UserRegions store so the create / update handlers can verify
//     ownership of the (primary + replica) regions
//
// The recording engine lives in this file (mockFederationEngine). The
// production *federation.Engine satisfies the same federationEngine
// interface, so production wiring stays one assignment in main.go.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/federation"
	"github.com/mattjackson/basement/internal/store"
)

// mockFederationEngine is a recording stand-in for *federation.Engine
// in handler tests. Captures every EnsureLoop / RemoveLoop / TriggerNow
// call so tests can assert "engine.Foo was called for ID X" without
// dealing with goroutine scheduling.
type mockFederationEngine struct {
	mu       sync.Mutex
	ensured  []string
	removed  []string
	triggers []string
}

func (m *mockFederationEngine) EnsureLoop(_ context.Context, fbID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensured = append(m.ensured, fbID)
}

func (m *mockFederationEngine) RemoveLoop(fbID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, fbID)
}

func (m *mockFederationEngine) TriggerNow(fbID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggers = append(m.triggers, fbID)
	return nil
}

func (m *mockFederationEngine) ensuredCount(fbID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, id := range m.ensured {
		if id == fbID {
			n++
		}
	}
	return n
}

func (m *mockFederationEngine) removedCount(fbID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, id := range m.removed {
		if id == fbID {
			n++
		}
	}
	return n
}

func (m *mockFederationEngine) triggeredCount(fbID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, id := range m.triggers {
		if id == fbID {
			n++
		}
	}
	return n
}

// federationTestEnv carries the assembled server + dependencies for
// one test. Cleanup is registered via t.Cleanup in newFederationTestEnv.
type federationTestEnv struct {
	srv    *Server
	engine *mockFederationEngine
	store  federation.FederatedBuckets
}

// newFederationTestEnv builds a Server with the federation store +
// recording mock engine wired in, plus a UserRegions store ready for
// per-user region seeding via seedRegion below.
func newFederationTestEnv(t *testing.T) *federationTestEnv {
	t.Helper()
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir

	st, err := store.Open(dataDir, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireUserRegions(cfg.JWT.Secret); err != nil {
		t.Fatalf("WireUserRegions: %v", err)
	}

	fedStore, err := federation.Open(dataDir)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	engine := &mockFederationEngine{}
	srv.SetFederation(fedStore, engine)

	return &federationTestEnv{
		srv:    srv,
		engine: engine,
		store:  fedStore,
	}
}

// seedRegion creates a UserRegion for the given user so the federation
// validation can confirm ownership. Returns the new region's ID.
func (e *federationTestEnv) seedRegion(t *testing.T, userID, alias, endpoint string) string {
	t.Helper()
	regions := e.srv.regionsStore()
	if regions == nil {
		t.Fatalf("regionsStore returned nil")
	}
	created, err := regions.Create(context.Background(), store.UserRegion{
		UserID:       userID,
		Alias:        alias,
		Endpoint:     endpoint,
		AccessKeyID:  "AK_" + userID + "_" + alias,
		SecretKeyEnc: []byte("secret-for-test"),
		Region:       "us-east-1",
	})
	if err != nil {
		t.Fatalf("regions.Create: %v", err)
	}
	return created.ID
}

// fedUserCookie attaches a USER-mode JWT for the given user ID. Mirrors
// userCookie from user_backups_test.go.
func fedUserCookie(t *testing.T, userID string) *http.Cookie {
	t.Helper()
	token, err := auth.IssueToken(testSecret, userID, "user", false, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return &http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// validFederationBody returns a known-good create body for the given
// region IDs. Tests mutate it as needed.
func validFederationBody(name, primaryRegion, replicaRegion string) map[string]interface{} {
	return map[string]interface{}{
		"name": name,
		"primary": map[string]string{
			"regionId": primaryRegion,
			"bucket":   "primary-bucket",
		},
		"replicas": []map[string]string{
			{"regionId": replicaRegion, "bucket": "replica-bucket"},
		},
	}
}

// TestCreateFederation_Happy: a well-formed POST returns 201 with an ID,
// persists into the store, and pokes the engine via EnsureLoop.
func TestCreateFederation_Happy(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage-home", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2-offsite", "https://s3.us-west-002.backblazeb2.com")

	req := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var got federatedBucketResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Fatalf("expected non-empty ID")
	}
	if got.OwnerUserID != "matthew" {
		t.Fatalf("expected OwnerUserID=matthew, got %q", got.OwnerUserID)
	}
	if got.Name != "lsi" {
		t.Fatalf("expected name=lsi, got %q", got.Name)
	}
	if got.Policy.SyncMode == "" {
		t.Fatalf("expected SyncMode default to be applied, got empty")
	}
	if env.engine.ensuredCount(got.ID) != 1 {
		t.Fatalf("expected EnsureLoop to fire once for %q, got %d", got.ID, env.engine.ensuredCount(got.ID))
	}

	// GET should return the same federation.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/federated-buckets/"+got.ID, nil)
	getReq.AddCookie(fedUserCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}

	// LIST should return exactly the one row.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/federated-buckets", nil)
	listReq.AddCookie(fedUserCookie(t, "matthew"))
	listRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("LIST: expected 200, got %d", listRR.Code)
	}
	var listGot []federatedBucketResponse
	_ = json.NewDecoder(listRR.Body).Decode(&listGot)
	if len(listGot) != 1 {
		t.Fatalf("expected 1 federation in list, got %d", len(listGot))
	}
}

// TestGetFederation_Ownership: user A's federation is 404 to user B.
func TestGetFederation_Ownership(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "alice", "garage", "https://s3.alice.example.com")
	replicaID := env.seedRegion(t, "alice", "b2", "https://s3.us-west-002.backblazeb2.com")

	// Alice creates the federation.
	req := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("alice-fed", primaryID, replicaID))
	req.AddCookie(fedUserCookie(t, "alice"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create as alice: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	// Matthew tries to GET → 404 (not 403).
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/federated-buckets/"+created.ID, nil)
	getReq.AddCookie(fedUserCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", getRR.Code, getRR.Body.String())
	}
}

// TestCreateFederation_DuplicateName: same user creates two federations
// with the same name → second one is 409 DUPLICATE_NAME.
func TestCreateFederation_DuplicateName(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	body := validFederationBody("lsi", primaryID, replicaID)
	for i := 0; i < 2; i++ {
		req := newJSONRequest("/api/v1/user/federated-buckets", body)
		req.AddCookie(fedUserCookie(t, "matthew"))
		rr := httptest.NewRecorder()
		env.srv.router.ServeHTTP(rr, req)
		if i == 0 {
			if rr.Code != http.StatusCreated {
				t.Fatalf("first create: expected 201, got %d body=%s", rr.Code, rr.Body.String())
			}
			continue
		}
		if rr.Code != http.StatusConflict {
			t.Fatalf("second create: expected 409, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp ErrorResponse
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if resp.Error.Code != "DUPLICATE_NAME" {
			t.Fatalf("expected code=DUPLICATE_NAME, got %q", resp.Error.Code)
		}
	}
}

// TestCreateFederation_InvalidName: special characters reject with
// 400 INVALID_NAME.
func TestCreateFederation_InvalidName(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	body := validFederationBody("not allowed!", primaryID, replicaID)
	req := newJSONRequest("/api/v1/user/federated-buckets", body)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_NAME" {
		t.Fatalf("expected code=INVALID_NAME, got %q", resp.Error.Code)
	}
}

// TestCreateFederation_ReplicaEqualsPrimary: a replica that duplicates
// the primary (same region + bucket) rejects as DUPLICATE_TARGET.
func TestCreateFederation_ReplicaEqualsPrimary(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")

	body := map[string]interface{}{
		"name": "self-replica",
		"primary": map[string]string{
			"regionId": primaryID,
			"bucket":   "lsi",
		},
		"replicas": []map[string]string{
			{"regionId": primaryID, "bucket": "lsi"},
		},
	}
	req := newJSONRequest("/api/v1/user/federated-buckets", body)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "DUPLICATE_TARGET" {
		t.Fatalf("expected code=DUPLICATE_TARGET, got %q", resp.Error.Code)
	}
}

// TestCreateFederation_InvalidScheduleCron: SyncMode=scheduled with a
// malformed cron expression rejects with 400 INVALID_SCHEDULE.
func TestCreateFederation_InvalidScheduleCron(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	body := map[string]interface{}{
		"name": "scheduled",
		"primary": map[string]string{
			"regionId": primaryID,
			"bucket":   "primary-bucket",
		},
		"replicas": []map[string]string{
			{"regionId": replicaID, "bucket": "replica-bucket"},
		},
		"policy": map[string]interface{}{
			"syncMode":    "scheduled",
			"schedule":    "this is not cron",
			"lagAlertSec": 300,
		},
	}
	req := newJSONRequest("/api/v1/user/federated-buckets", body)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_SCHEDULE" {
		t.Fatalf("expected code=INVALID_SCHEDULE, got %q", resp.Error.Code)
	}
}

// TestFailoverFederation_Happy: promoting an existing replica swaps
// primary <-> replica, fires the engine, and surfaces the new primary
// on GET.
func TestFailoverFederation_Happy(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	// Promote the b2 replica to primary.
	failoverBody := map[string]string{
		"newPrimaryRegionId": replicaID,
		"newPrimaryBucket":   "replica-bucket",
	}
	foReq := newJSONRequest("/api/v1/user/federated-buckets/"+created.ID+"/failover", failoverBody)
	foReq.AddCookie(fedUserCookie(t, "matthew"))
	foRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(foRR, foReq)
	if foRR.Code != http.StatusOK {
		t.Fatalf("failover: expected 200, got %d body=%s", foRR.Code, foRR.Body.String())
	}
	var afterFO federatedBucketResponse
	_ = json.NewDecoder(foRR.Body).Decode(&afterFO)
	if afterFO.Primary.RegionID != replicaID || afterFO.Primary.Bucket != "replica-bucket" {
		t.Fatalf("expected primary swapped to b2 replica, got %+v", afterFO.Primary)
	}
	if len(afterFO.Replicas) != 1 || afterFO.Replicas[0].RegionID != primaryID || afterFO.Replicas[0].Bucket != "primary-bucket" {
		t.Fatalf("expected old primary demoted to sole replica, got %+v", afterFO.Replicas)
	}

	// TriggerNow should have fired.
	if env.engine.triggeredCount(created.ID) < 1 {
		t.Fatalf("expected TriggerNow after failover, got %d calls", env.engine.triggeredCount(created.ID))
	}

	// GET reflects the new primary.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/federated-buckets/"+created.ID, nil)
	getReq.AddCookie(fedUserCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	var afterGet federatedBucketResponse
	_ = json.NewDecoder(getRR.Body).Decode(&afterGet)
	if afterGet.Primary.RegionID != replicaID {
		t.Fatalf("GET after failover: expected primary=%q, got %q", replicaID, afterGet.Primary.RegionID)
	}
}

// TestFailoverFederation_NotAReplica: failover to a (regionId, bucket)
// that isn't a current replica returns 404 NOT_A_REPLICA.
func TestFailoverFederation_NotAReplica(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	failoverBody := map[string]string{
		"newPrimaryRegionId": replicaID,
		"newPrimaryBucket":   "no-such-bucket",
	}
	foReq := newJSONRequest("/api/v1/user/federated-buckets/"+created.ID+"/failover", failoverBody)
	foReq.AddCookie(fedUserCookie(t, "matthew"))
	foRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(foRR, foReq)
	if foRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", foRR.Code, foRR.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(foRR.Body).Decode(&resp)
	if resp.Error.Code != "NOT_A_REPLICA" {
		t.Fatalf("expected code=NOT_A_REPLICA, got %q", resp.Error.Code)
	}
}

// TestDeleteFederation_Happy: DELETE returns 204 and fires
// engine.RemoveLoop.
func TestDeleteFederation_Happy(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("ephemeral", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/user/federated-buckets/"+created.ID, nil)
	delReq.AddCookie(fedUserCookie(t, "matthew"))
	delRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", delRR.Code, delRR.Body.String())
	}
	if env.engine.removedCount(created.ID) != 1 {
		t.Fatalf("expected RemoveLoop to fire once for %q, got %d",
			created.ID, env.engine.removedCount(created.ID))
	}

	// Subsequent GET is 404.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/federated-buckets/"+created.ID, nil)
	getReq.AddCookie(fedUserCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("after delete: expected 404, got %d", getRR.Code)
	}
}

// TestResyncFederation_Happy: POST .../resync returns 200 and fires
// engine.TriggerNow.
func TestResyncFederation_Happy(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	resyncReq := httptest.NewRequest(http.MethodPost, "/api/v1/user/federated-buckets/"+created.ID+"/resync", nil)
	resyncReq.Header.Set("Content-Type", "application/json")
	resyncReq.AddCookie(fedUserCookie(t, "matthew"))
	resyncRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(resyncRR, resyncReq)
	if resyncRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resyncRR.Code, resyncRR.Body.String())
	}
	if env.engine.triggeredCount(created.ID) < 1 {
		t.Fatalf("expected TriggerNow to fire on resync, got 0 calls")
	}
}

// TestUpdateFederation_Happy: PUT changes the policy + replicas, fires
// engine.TriggerNow, and GET reflects the change.
func TestUpdateFederation_Happy(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")
	thirdID := env.seedRegion(t, "matthew", "wasabi", "https://s3.us-east-1.wasabisys.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	// Add a second replica via PUT.
	updateBody := map[string]interface{}{
		"name": "lsi",
		"primary": map[string]string{
			"regionId": primaryID,
			"bucket":   "primary-bucket",
		},
		"replicas": []map[string]string{
			{"regionId": replicaID, "bucket": "replica-bucket"},
			{"regionId": thirdID, "bucket": "wasabi-replica"},
		},
		"policy": map[string]interface{}{
			"syncMode":    "continuous",
			"lagAlertSec": 600,
			"writeQuorum": 2,
		},
	}
	data, _ := json.Marshal(updateBody)
	updReq := httptest.NewRequest(http.MethodPut, "/api/v1/user/federated-buckets/"+created.ID,
		bytes.NewReader(data))
	updReq.Header.Set("Content-Type", "application/json")
	updReq.AddCookie(fedUserCookie(t, "matthew"))
	updRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(updRR, updReq)
	if updRR.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d body=%s", updRR.Code, updRR.Body.String())
	}
	var updated federatedBucketResponse
	_ = json.NewDecoder(updRR.Body).Decode(&updated)
	if len(updated.Replicas) != 2 {
		t.Fatalf("expected 2 replicas after update, got %d", len(updated.Replicas))
	}
	if updated.Policy.LagAlertSec != 600 {
		t.Fatalf("expected LagAlertSec=600, got %d", updated.Policy.LagAlertSec)
	}
	if env.engine.triggeredCount(created.ID) < 1 {
		t.Fatalf("expected TriggerNow after update")
	}
}

// TestFindFederationByTarget_PrimaryMatch: hitting /by-target with the
// primary's (regionId, bucket) returns 200 + the federation record.
func TestFindFederationByTarget_PrimaryMatch(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	url := "/api/v1/user/federated-buckets/by-target?regionId=" + primaryID + "&bucket=primary-bucket"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got federatedBucketResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("expected federation %q, got %q", created.ID, got.ID)
	}
}

// TestFindFederationByTarget_ReplicaMatch: hitting /by-target with a
// replica's (regionId, bucket) also returns 200 + the federation record.
func TestFindFederationByTarget_ReplicaMatch(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")

	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	var created federatedBucketResponse
	_ = json.NewDecoder(createRR.Body).Decode(&created)

	url := "/api/v1/user/federated-buckets/by-target?regionId=" + replicaID + "&bucket=replica-bucket"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got federatedBucketResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.ID != created.ID {
		t.Fatalf("expected federation %q, got %q", created.ID, got.ID)
	}
}

// TestFindFederationByTarget_NoMatch: when no federation matches the
// (regionId, bucket) pair, the endpoint returns 204 No Content (NOT
// 404) so the bucket browser can speculatively probe without flooding
// the network panel with 404s.
func TestFindFederationByTarget_NoMatch(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "matthew", "garage", "https://s3.home.example.com")

	// Probe with no federations defined at all.
	url := "/api/v1/user/federated-buckets/by-target?regionId=" + primaryID + "&bucket=some-bucket"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body on 204, got %q", rr.Body.String())
	}

	// Now create a federation but probe a (region, bucket) it doesn't
	// cover — still 204.
	replicaID := env.seedRegion(t, "matthew", "b2", "https://s3.us-west-002.backblazeb2.com")
	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("lsi", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "matthew"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}

	url2 := "/api/v1/user/federated-buckets/by-target?regionId=" + primaryID + "&bucket=unknown-bucket"
	req2 := httptest.NewRequest(http.MethodGet, url2, nil)
	req2.AddCookie(fedUserCookie(t, "matthew"))
	rr2 := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content after no-match, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

// TestFindFederationByTarget_OwnershipScoped: user A's federation is
// invisible to user B via /by-target — B gets 204 No Content even
// though the (regionId, bucket) pair matches A's record.
func TestFindFederationByTarget_OwnershipScoped(t *testing.T) {
	env := newFederationTestEnv(t)
	primaryID := env.seedRegion(t, "alice", "garage", "https://s3.alice.example.com")
	replicaID := env.seedRegion(t, "alice", "b2", "https://s3.us-west-002.backblazeb2.com")

	// Alice creates the federation.
	createReq := newJSONRequest("/api/v1/user/federated-buckets",
		validFederationBody("alice-fed", primaryID, replicaID))
	createReq.AddCookie(fedUserCookie(t, "alice"))
	createRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create as alice: expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}

	// Matthew probes /by-target with alice's (region, bucket) → 204
	// (never leaks the existence of alice's federation).
	url := "/api/v1/user/federated-buckets/by-target?regionId=" + primaryID + "&bucket=primary-bucket"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("cross-owner probe: expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Alice's own probe still hits — sanity check the row exists.
	req2 := httptest.NewRequest(http.MethodGet, url, nil)
	req2.AddCookie(fedUserCookie(t, "alice"))
	rr2 := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("alice's own probe: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

// TestHealthRank_PendingBetweenInSyncAndLagging pins the v1.11.0.9
// healthRank ordering: HealthPending is worse than in-sync (so a
// pending replica drags the summary off the green path) but better
// than lagging (so a one-pending + one-lagging federation surfaces
// the lagging — the more actionable signal).
//
// Operator-facing impact: a fresh federation whose only replica is
// pending verification renders as computedHealth="pending" in the
// /api/v1/user/federated-buckets list — yellow signal, not green.
func TestHealthRank_PendingBetweenInSyncAndLagging(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign of healthRank(a) - healthRank(b): -1 a<b, 0 a==b, 1 a>b
	}{
		{federation.HealthInSync, federation.HealthPending, -1},
		{federation.HealthPending, federation.HealthLagging, -1},
		{federation.HealthLagging, federation.HealthStale, -1},
		{federation.HealthStale, federation.HealthBroken, -1},
		{federation.HealthPending, federation.HealthInSync, 1},
		{federation.HealthBroken, federation.HealthPending, 1},
		{federation.HealthInSync, federation.HealthInSync, 0},
	}
	for _, c := range cases {
		got := sign(healthRank(c.a) - healthRank(c.b))
		if got != c.want {
			t.Errorf("healthRank(%q) vs healthRank(%q): got sign=%d, want %d (ranks %d, %d)",
				c.a, c.b, got, c.want, healthRank(c.a), healthRank(c.b))
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// TestToFederatedBucketResponse_PendingSurfacedInSummary verifies the
// v1.11.0.9 health-summary wiring end-to-end: a federation with a
// single pending replica gets computedHealth="pending" in the
// response, and a federation with a pending + a lagging replica
// surfaces the worse "lagging" signal.
func TestToFederatedBucketResponse_PendingSurfacedInSummary(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name     string
		replicas []federation.ReplicaTarget
		want     string
	}{
		{
			name: "single pending replica → summary=pending",
			replicas: []federation.ReplicaTarget{
				{RegionID: "r1", Bucket: "b1", Health: federation.HealthPending},
			},
			want: federation.HealthPending,
		},
		{
			name: "pending + in-sync → summary=pending (worse wins)",
			replicas: []federation.ReplicaTarget{
				{RegionID: "r1", Bucket: "b1", Health: federation.HealthInSync, LastSync: now.Add(-time.Minute)},
				{RegionID: "r2", Bucket: "b2", Health: federation.HealthPending},
			},
			want: federation.HealthPending,
		},
		{
			name: "pending + lagging → summary=lagging (lagging is worse)",
			replicas: []federation.ReplicaTarget{
				{RegionID: "r1", Bucket: "b1", Health: federation.HealthPending},
				{RegionID: "r2", Bucket: "b2", Health: federation.HealthLagging, LastSync: now.Add(-time.Hour)},
			},
			want: federation.HealthLagging,
		},
		{
			name: "pending + broken → summary=broken",
			replicas: []federation.ReplicaTarget{
				{RegionID: "r1", Bucket: "b1", Health: federation.HealthPending},
				{RegionID: "r2", Bucket: "b2", Health: federation.HealthBroken},
			},
			want: federation.HealthBroken,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fb := federation.FederatedBucket{
				Replicas: c.replicas,
				Policy:   federation.DefaultPolicy(),
			}
			got := toFederatedBucketResponse(fb)
			if got.ComputedHealth != c.want {
				t.Fatalf("ComputedHealth=%q, want %q", got.ComputedHealth, c.want)
			}
		})
	}
}

// TestFindFederationByTarget_MissingParams: omitting regionId or
// bucket returns 400 INVALID_REQUEST.
func TestFindFederationByTarget_MissingParams(t *testing.T) {
	env := newFederationTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/federated-buckets/by-target", nil)
	req.AddCookie(fedUserCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}
