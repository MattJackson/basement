// Package api: tests for the v0.9.0h orphan-creds migration handlers.
//
// Three behaviours have to hold:
//
//   1. Happy path: a Connection with access_key_id + secret_key in its
//      config, two requested bucket aliases → two BucketGrants minted +
//      two bucket_user assignments + creds stripped from the
//      Connection's config + cached driver invalidated.
//   2. NO_ORPHAN_CREDS: when the Connection's config doesn't carry the
//      legacy fields, the handler returns 400 NO_ORPHAN_CREDS and
//      neither the BucketGrants store nor the Connection are touched.
//   3. Rollback: when the cred-strip step fails (simulated via the mock
//      Connections.Update returning an error), every BucketGrant the
//      handler had created in step 2 is rolled back so disk state stays
//      consistent.
//
// Tests install a real file-backed policy enforcer + real BucketGrants
// store and assign the calling admin user host_admin @ host:* so the
// capability gate (host:manage_policies) passes — the seed roles
// include host_admin → *:* which subsumes the capability.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newMigrationTestEnv wires a Server with the same shape as the policy
// tests: real Store + real file-backed policy + an in-memory mock
// Connections store, plus an admin host_admin@host:* assignment so the
// migration handlers pass their gate.
func newMigrationTestEnv(t *testing.T) (*Server, *testMockConnectionStore, policy.Enforcer, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v090h-migration-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		cleanup()
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireBucketGrants(cfg.JWT.Secret); err != nil {
		cleanup()
		t.Fatalf("WireBucketGrants: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		cleanup()
		t.Fatalf("policy.Open: %v", err)
	}
	// generateAdminToken issues a JWT with UserID "admin"; assign that
	// user host_admin so the gate passes.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "admin", RoleID: "host_admin", Scope: "host:*",
	}); err != nil {
		cleanup()
		t.Fatalf("AssignRole: %v", err)
	}

	conns := &testMockConnectionStore{}
	srv := New(cfg, st, conns, nil, nil)
	srv.SetPolicy(enf)
	return srv, conns, enf, cleanup
}

// adminMigrationReq builds an admin-authenticated request with an
// optional JSON body. Mirrors adminPolicyReq.
func adminMigrationReq(method, url string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, url, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	r.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return r
}

// TestListOrphanCreds_OK: one Connection has legacy creds, one
// doesn't. The handler returns the first only.
func TestListOrphanCreds_OK(t *testing.T) {
	srv, conns, _, cleanup := newMigrationTestEnv(t)
	defer cleanup()

	// Seed two connections — one with creds, one without.
	conns.conns = []store.Connection{
		{
			ID:     "cid-orphan",
			Label:  "classe",
			Driver: store.DriverGarageV1,
			Config: map[string]string{
				"admin_url":     "https://admin.example",
				"admin_token":   "tok",
				"s3_endpoint":   "https://s3.example",
				"access_key_id": "GK_old",
				"secret_key":    "oldsecret",
			},
		},
		{
			ID:     "cid-clean",
			Label:  "lsi",
			Driver: store.DriverGarageV1,
			Config: map[string]string{
				"admin_url":   "https://admin.example",
				"admin_token": "tok",
				"s3_endpoint": "https://s3.example",
			},
		},
	}

	req := adminMigrationReq(http.MethodGet, "/api/v1/admin/migrations/orphan_creds", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	var resp orphanCredsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d (orphans=%+v)", len(resp.Orphans), resp.Orphans)
	}
	o := resp.Orphans[0]
	if o.ConnectionID != "cid-orphan" {
		t.Errorf("expected connectionId=cid-orphan, got %q", o.ConnectionID)
	}
	if o.Label != "classe" {
		t.Errorf("expected label=classe, got %q", o.Label)
	}
	if o.AccessKeyID != "GK_old" {
		t.Errorf("expected accessKeyId=GK_old, got %q", o.AccessKeyID)
	}
	if !o.HasSecretKey {
		t.Errorf("expected hasSecretKey=true")
	}
}

// TestListOrphanCreds_None: no orphans → empty list (not nil), 200.
func TestListOrphanCreds_None(t *testing.T) {
	srv, conns, _, cleanup := newMigrationTestEnv(t)
	defer cleanup()

	conns.conns = []store.Connection{
		{
			ID:     "cid-clean",
			Label:  "lsi",
			Driver: store.DriverGarageV1,
			Config: map[string]string{
				"admin_url": "https://admin.example",
			},
		},
	}

	req := adminMigrationReq(http.MethodGet, "/api/v1/admin/migrations/orphan_creds", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp orphanCredsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Orphans == nil {
		t.Errorf("expected non-nil orphans slice")
	}
	if len(resp.Orphans) != 0 {
		t.Errorf("expected 0 orphans, got %d", len(resp.Orphans))
	}
}

// TestListOrphanCreds_NoCapability: a token without host_admin
// assignment hits 403 even when admin role passes the legacy gate.
func TestListOrphanCreds_NoCapability(t *testing.T) {
	srv, _, enf, cleanup := newMigrationTestEnv(t)
	defer cleanup()

	// Revoke the seed host_admin assignment so the gate fails.
	_ = enf.UnassignRole("admin", "host_admin", "host:*")

	req := adminMigrationReq(http.MethodGet, "/api/v1/admin/migrations/orphan_creds", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without host:manage_policies, got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
}

// TestMigrateOrphanCreds_HappyPath: migrate two aliases → two grants
// minted, two bucket_user assignments, creds stripped from Connection,
// 200 response with grantsCreated=2.
func TestMigrateOrphanCreds_HappyPath(t *testing.T) {
	srv, conns, enf, cleanup := newMigrationTestEnv(t)
	defer cleanup()

	conns.conns = []store.Connection{
		{
			ID:     "cid-orphan",
			Label:  "classe",
			Driver: store.DriverGarageV1,
			Config: map[string]string{
				"admin_url":     "https://admin.example",
				"admin_token":   "tok",
				"s3_endpoint":   "https://s3.example",
				"access_key_id": "GK_old",
				"secret_key":    "oldsecret",
			},
		},
	}

	body, _ := json.Marshal(migrateOrphanCredsRequest{
		UserID:        "matthew",
		BucketAliases: []string{"lsi", "cheshire"},
	})
	req := adminMigrationReq(http.MethodPost,
		"/api/v1/admin/migrations/orphan_creds/cid-orphan/grant", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp migrateOrphanCredsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.GrantsCreated != 2 {
		t.Errorf("expected grantsCreated=2, got %d", resp.GrantsCreated)
	}
	if !resp.ConnectionUpdated {
		t.Errorf("expected connectionUpdated=true")
	}

	// BucketGrants persisted with the right shape + the secret encrypted.
	grants, _ := srv.store.CredGrants().ListForUser(context.Background(), "matthew")
	if len(grants) != 2 {
		t.Fatalf("expected 2 grants for matthew, got %d (%+v)", len(grants), grants)
	}
	seenAlias := map[string]bool{}
	for _, g := range grants {
		seenAlias[g.BucketID] = true
		if g.AccessKeyID != "GK_old" {
			t.Errorf("expected accessKeyId=GK_old, got %q", g.AccessKeyID)
		}
		if len(g.SecretKeyEnc) == 0 {
			t.Errorf("expected encrypted secret bytes, got empty")
		}
		plain, err := srv.store.CredGrants().Decrypt(g)
		if err != nil {
			t.Errorf("decrypt: %v", err)
		}
		if plain != "oldsecret" {
			t.Errorf("decrypt round-trip mismatch: %q", plain)
		}
	}
	if !seenAlias["lsi"] || !seenAlias["cheshire"] {
		t.Errorf("expected both lsi + cheshire grants, got %+v", seenAlias)
	}

	// bucket_user assignments at the per-bucket scope exist.
	assignments := enf.AssignmentsFor("matthew")
	wantScopes := map[string]bool{
		"bucket:cid-orphan:lsi":      true,
		"bucket:cid-orphan:cheshire": true,
	}
	for _, a := range assignments {
		if a.RoleID == "bucket_user" && wantScopes[a.Scope] {
			delete(wantScopes, a.Scope)
		}
	}
	if len(wantScopes) != 0 {
		t.Errorf("missing bucket_user assignments at: %+v", wantScopes)
	}

	// Connection's creds stripped from config; other keys intact.
	updated, _ := conns.Get(context.Background(), "cid-orphan")
	if _, ok := updated.Config["access_key_id"]; ok {
		t.Errorf("expected access_key_id stripped, still present")
	}
	if _, ok := updated.Config["secret_key"]; ok {
		t.Errorf("expected secret_key stripped, still present")
	}
	if updated.Config["admin_url"] != "https://admin.example" {
		t.Errorf("expected admin_url intact, got %q", updated.Config["admin_url"])
	}
	if updated.Config["s3_endpoint"] != "https://s3.example" {
		t.Errorf("expected s3_endpoint intact, got %q", updated.Config["s3_endpoint"])
	}
}

// TestMigrateOrphanCreds_NoOrphan: Connection without creds → 400
// NO_ORPHAN_CREDS, nothing mutated.
func TestMigrateOrphanCreds_NoOrphan(t *testing.T) {
	srv, conns, _, cleanup := newMigrationTestEnv(t)
	defer cleanup()

	conns.conns = []store.Connection{
		{
			ID:     "cid-clean",
			Label:  "lsi",
			Driver: store.DriverGarageV1,
			Config: map[string]string{
				"admin_url":   "https://admin.example",
				"admin_token": "tok",
			},
		},
	}

	body, _ := json.Marshal(migrateOrphanCredsRequest{
		UserID:        "matthew",
		BucketAliases: []string{"lsi"},
	})
	req := adminMigrationReq(http.MethodPost,
		"/api/v1/admin/migrations/orphan_creds/cid-clean/grant", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 NO_ORPHAN_CREDS, got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
	if !bodyHasCode(rr, "NO_ORPHAN_CREDS") {
		t.Errorf("expected error code NO_ORPHAN_CREDS; body=%s", rr.Body.String())
	}

	grants, _ := srv.store.CredGrants().ListForUser(context.Background(), "matthew")
	if len(grants) != 0 {
		t.Errorf("expected no grants minted, got %d", len(grants))
	}
}

// TestMigrateOrphanCreds_Rollback: cred-strip fails (mock
// Connections.Update returns error) → all created BucketGrants are
// deleted so disk state stays consistent.
func TestMigrateOrphanCreds_Rollback(t *testing.T) {
	srv, conns, _, cleanup := newMigrationTestEnv(t)
	defer cleanup()

	conns.conns = []store.Connection{
		{
			ID:     "cid-orphan",
			Label:  "classe",
			Driver: store.DriverGarageV1,
			Config: map[string]string{
				"admin_url":     "https://admin.example",
				"access_key_id": "GK_old",
				"secret_key":    "oldsecret",
			},
		},
	}
	// Force Update to fail to simulate the cred-strip step blowing up
	// (disk full, permissions error, atomic-write race, etc.).
	conns.updateFunc = func(_ context.Context, _ string, _ store.Connection) (store.Connection, error) {
		return store.Connection{}, errors.New("simulated disk-write failure")
	}

	body, _ := json.Marshal(migrateOrphanCredsRequest{
		UserID:        "matthew",
		BucketAliases: []string{"lsi", "cheshire"},
	})
	req := adminMigrationReq(http.MethodPost,
		"/api/v1/admin/migrations/orphan_creds/cid-orphan/grant", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 CONNECTION_UPDATE_FAILED, got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
	if !bodyHasCode(rr, "CONNECTION_UPDATE_FAILED") {
		t.Errorf("expected code CONNECTION_UPDATE_FAILED; body=%s", rr.Body.String())
	}

	// All grants rolled back.
	grants, _ := srv.store.CredGrants().ListForUser(context.Background(), "matthew")
	if len(grants) != 0 {
		t.Errorf("expected rollback to leave 0 grants, got %d (%+v)", len(grants), grants)
	}
}

// TestMigrateOrphanCreds_MissingFields: empty userId or empty
// bucketAliases → 400 INVALID_REQUEST.
func TestMigrateOrphanCreds_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		body migrateOrphanCredsRequest
	}{
		{"missing userId", migrateOrphanCredsRequest{BucketAliases: []string{"lsi"}}},
		{"empty aliases", migrateOrphanCredsRequest{UserID: "matthew", BucketAliases: []string{}}},
		{"whitespace-only aliases", migrateOrphanCredsRequest{UserID: "matthew", BucketAliases: []string{"   "}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, conns, _, cleanup := newMigrationTestEnv(t)
			defer cleanup()

			conns.conns = []store.Connection{
				{
					ID:     "cid-orphan",
					Label:  "x",
					Driver: store.DriverGarageV1,
					Config: map[string]string{
						"access_key_id": "GK",
						"secret_key":    "sec",
					},
				},
			}

			body, _ := json.Marshal(tc.body)
			req := adminMigrationReq(http.MethodPost,
				"/api/v1/admin/migrations/orphan_creds/cid-orphan/grant", body)
			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
	}
}
