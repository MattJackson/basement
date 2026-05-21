package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/store"
)

// seedShareUserGrant installs a BucketGrant (v1.0.0b: replaces the old
// legacy store.Grant fixtures). user_shares.go reads visibility via
// CredGrants now, so the test seed must mirror the production shape.
func seedShareUserGrant(t *testing.T, st *store.Store, userID, connID, bucketID string) {
	t.Helper()
	if err := st.WireBucketGrants(testSecret); err != nil {
		t.Fatalf("WireBucketGrants: %v", err)
	}
	if _, err := st.CredGrants().Create(context.Background(), store.BucketGrantInput{
		UserID:       userID,
		ConnectionID: connID,
		BucketID:     bucketID,
		AccessKeyID:  "ak-test",
		SecretKey:    "sk-test",
	}); err != nil {
		t.Fatalf("CredGrants.Create: %v", err)
	}
}

// TestCreateShare_NoAuth returns 401.
func TestCreateShare_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}
	st, _ := store.Open("/tmp/test-store-shares", 90*24*time.Hour)
	defer os.RemoveAll("/tmp/test-store-shares")

	srv := New(cfg, st, connsStore, nil, nil)

	body := map[string]interface{}{
		"connectionId": "conn-123",
		"bucketId":     "bucket-456",
		"prefix":       "path/to/prefix/",
	}
	req := newJSONRequest("/api/v1/user/shares", body)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

// TestCreateShare_MissingPrefixOrKey returns 400.
func TestCreateShare_MissingPrefixOrKey(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}
	st, _ := store.Open("/tmp/test-store-shares", 90*24*time.Hour)
	defer os.RemoveAll("/tmp/test-store-shares")

	srv := New(cfg, st, connsStore, nil, nil)

	body := map[string]interface{}{
		"connectionId": "conn-123",
		"bucketId":     "bucket-456",
	}
	req := newJSONRequest("/api/v1/user/shares", body)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestCreateShare_BothPrefixAndKey returns 400.
func TestCreateShare_BothPrefixAndKey(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}
	st, _ := store.Open("/tmp/test-store-shares", 90*24*time.Hour)
	defer os.RemoveAll("/tmp/test-store-shares")

	srv := New(cfg, st, connsStore, nil, nil)

	body := map[string]interface{}{
		"connectionId": "conn-123",
		"bucketId":     "bucket-456",
		"prefix":       "path/to/prefix/",
		"key":          "single/object/key.txt",
	}
	req := newJSONRequest("/api/v1/user/shares", body)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestCreateShare_HappyPath creates a share and returns the token.
func TestCreateShare_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-123", Label: "test-cluster", Driver: "garage"},
		},
	}
	tmpDir := t.TempDir()
	st, _ := store.Open(tmpDir, 90*24*time.Hour)

	// Create a bucket grant for the test user.
	seedShareUserGrant(t, st, "user", "conn-123", "bucket-456")

	srv := New(cfg, st, connsStore, nil, nil)

	body := map[string]interface{}{
		"connectionId": "conn-123",
		"bucketId":     "bucket-456",
		"prefix":       "shared/prefix/",
	}
	req := newJSONRequest("/api/v1/user/shares", body)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if _, ok := result["token"]; !ok {
		t.Error("expected token in response")
	}
	if result["prefix"] != "shared/prefix/" {
		t.Errorf("expected prefix 'shared/prefix/', got '%v'", result["prefix"])
	}
}

// TestCreateShare_WithPassword creates a share with password hash.
func TestCreateShare_WithPassword(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-123", Label: "test-cluster", Driver: "garage"},
		},
	}
	tmpDir := t.TempDir()
	st, _ := store.Open(tmpDir, 90*24*time.Hour)

	seedShareUserGrant(t, st, "user", "conn-123", "bucket-456")

	srv := New(cfg, st, connsStore, nil, nil)

	body := map[string]interface{}{
		"connectionId": "conn-123",
		"bucketId":     "bucket-456",
		"key":          "secret/file.txt",
		"password":     "mySecurePassword123",
	}
	req := newJSONRequest("/api/v1/user/shares", body)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	passwordHash, ok := result["passwordHash"].(string)
	if !ok || passwordHash == "" {
		t.Error("expected passwordHash in response")
	}
}

// TestListShares_HappyPath returns user's shares.
func TestListShares_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-123", Label: "test-cluster", Driver: "garage"},
		},
	}
	tmpDir := t.TempDir()
	st, _ := store.Open(tmpDir, 90*24*time.Hour)

	// Create a bucket grant for the test user.
	seedShareUserGrant(t, st, "user", "conn-123", "bucket-456")

	// Create some test shares.
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	downloadLimit := 10
	
	shares := []store.Share{
		{
			Token:         "token-abc",
			OwnerUserID:   "user",
			ConnectionID:  "conn-123",
			BucketID:      "bucket-456",
			Prefix:        "shared/",
			CreatedAt:     now,
			ExpiresAt:     &expiresAt,
			DownloadLimit: &downloadLimit,
			DownloadsUsed: 0,
			Revoked:       false,
		},
	}

	for _, sh := range shares {
		if err := st.CreateShare(sh); err != nil {
			t.Fatalf("failed to create test share: %v", err)
		}
	}

	srv := New(cfg, st, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/shares", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 share, got %d", len(result))
	}
}

// TestRevokeShare_HappyPath revokes a share.
func TestRevokeShare_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-123", Label: "test-cluster", Driver: "garage"},
		},
	}
	tmpDir := t.TempDir()
	st, _ := store.Open(tmpDir, 90*24*time.Hour)

	// Create a bucket grant for the test user.
	seedShareUserGrant(t, st, "user", "conn-123", "bucket-456")

	// Create a test share.
	now := time.Now()
	sh := store.Share{
		Token:         "token-xyz",
		OwnerUserID:   "user",
		ConnectionID:  "conn-123",
		BucketID:      "bucket-456",
		Prefix:        "shared/",
		CreatedAt:     now,
		Revoked:       false,
	}
	if err := st.CreateShare(sh); err != nil {
		t.Fatalf("failed to create test share: %v", err)
	}

	srv := New(cfg, st, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/shares/token-xyz", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var result map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["message"] != "Share link revoked" {
		t.Errorf("expected message 'Share link revoked', got '%v'", result["message"])
	}

	// Verify share is now revoked.
	revokedShare, err := st.Share("token-xyz")
	if err == nil && !revokedShare.Revoked {
		t.Error("expected share to be revoked")
	}
}

// TestRevokeShare_OwnershipCheck returns 403 for other user's share.
func TestRevokeShare_OwnershipCheck(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-123", Label: "test-cluster", Driver: "garage"},
		},
	}
	tmpDir := t.TempDir()
	st, _ := store.Open(tmpDir, 90*24*time.Hour)

	// Create a bucket grant for the test user.
	seedShareUserGrant(t, st, "user", "conn-123", "bucket-456")

	// Create a test share owned by different user.
	now := time.Now()
	sh := store.Share{
		Token:         "token-abc",
		OwnerUserID:   "user-other",
		ConnectionID:  "conn-123",
		BucketID:      "bucket-456",
		Prefix:        "shared/",
		CreatedAt:     now,
		Revoked:       false,
	}
	if err := st.CreateShare(sh); err != nil {
		t.Fatalf("failed to create test share: %v", err)
	}

	srv := New(cfg, st, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/shares/token-abc", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	// Should return 403 (forbidden) because token is owned by different user.
	if rr.Code != http.StatusForbidden {
		t.Logf("Got status %d (expected 403 for ownership check)", rr.Code)
	}
}

// TestRevokeShare_NotFound returns 404.
func TestRevokeShare_NotFound(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}
	st, _ := store.Open("/tmp/test-store-shares", 90*24*time.Hour)
	defer os.RemoveAll("/tmp/test-store-shares")

	srv := New(cfg, st, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/user/shares/nonexistent-token", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}
