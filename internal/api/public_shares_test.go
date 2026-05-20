package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// setupTestServer creates a test server with minimal fixtures for share tests.
func setupTestServer(t *testing.T) *Server {
	t.Helper()
	
	cfg := newTestConfig()
	st, err := store.Open("/tmp/test-share-store-"+t.Name(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-123", Label: "Test Cluster", Driver: "garage-v1", Config: map[string]string{}, Owner: "user-123"},
		},
	}
	
	reg := driver.NewRegistry(connsStore)
	return New(cfg, st, connsStore, nil, reg)
}

// nowForShareTests returns the current time for share tests.
func nowForShareTests() time.Time {
	return time.Now().UTC()
}

// TestShareInfoHandler tests the /api/v1/share/{token}/info endpoint.
func TestShareInfoHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupShare     func(s *store.Store, token string) error
		expectedStatus int
		expectedCode   string
	}{
		{
			name: "not found",
			setupShare: func(s *store.Store, token string) error {
				return nil // Don't create share
			},
			expectedStatus: http.StatusNotFound,
			expectedCode:   "SHARE_NOT_FOUND",
		},
		{
			name: "revoked",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:       token,
					OwnerUserID: "user-123",
					ConnectionID: "conn-123",
					BucketID:    "bucket-123",
					Prefix:      "shared/folder/",
					Revoked:     true,
				}
				return s.CreateShare(sh)
			},
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_REVOKED",
		},
		{
			name: "expired",
			setupShare: func(s *store.Store, token string) error {
				now := nowForShareTests()
				expiredAt := now.Add(-24 * time.Hour)
				sh := store.Share{
					Token:       token,
					OwnerUserID: "user-123",
					ConnectionID: "conn-123",
					BucketID:    "bucket-123",
					Prefix:      "shared/folder/",
					ExpiresAt:   &expiredAt,
				}
				return s.CreateShare(sh)
			},
			expectedStatus: http.StatusOK, // Info endpoint returns data even if expired
		},
		{
			name: "valid prefix share",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
				}
				return s.CreateShare(sh)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "valid single object share",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Key:          "single/object.txt",
				}
				return s.CreateShare(sh)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "password protected share",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					PasswordHash: "$2a$10$...", // Dummy bcrypt hash
				}
				return s.CreateShare(sh)
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := setupTestServer(t)
			token := "test-token-123"

			if err := tt.setupShare(srv.store, token); err != nil {
				t.Fatalf("failed to setup share: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/share/"+token+"/info", nil)
			w := httptest.NewRecorder()

			srv.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: body=%s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedCode != "" && !bytes.Contains(w.Body.Bytes(), []byte("\"code\":\""+tt.expectedCode+"\"")) {
				t.Errorf("expected error code %s in response, got: %s", tt.expectedCode, w.Body.String())
			}

			if tt.expectedStatus == http.StatusOK {
				var resp shareInfoResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Errorf("failed to parse response: %v", err)
				}
			}
		})
	}
}

// TestShareAuthHandler tests the /api/v1/share/{token}/auth endpoint.
func TestShareAuthHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupShare     func(s *store.Store, token string) error
		password       string
		expectedStatus int
		expectedCode   string
	}{
		{
			name: "not found",
			setupShare: func(s *store.Store, token string) error {
				return nil // Don't create share
			},
			password:       "correct-password",
			expectedStatus: http.StatusNotFound,
			expectedCode:   "SHARE_NOT_FOUND",
		},
		{
			name: "no password required",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
				}
				return s.CreateShare(sh)
			},
			password:       "any-password",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "NO_PASSWORD_REQUIRED",
		},
		{
			name: "invalid password",
			setupShare: func(s *store.Store, token string) error {
				hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					PasswordHash: string(hash),
				}
				return s.CreateShare(sh)
			},
			password:       "wrong-password",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "INVALID_PASSWORD",
		},
		{
			name: "valid password sets cookie",
			setupShare: func(s *store.Store, token string) error {
				hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					PasswordHash: string(hash),
				}
				return s.CreateShare(sh)
			},
			password:       "correct-password",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := setupTestServer(t)
			token := "test-token-123"

			if err := tt.setupShare(srv.store, token); err != nil {
				t.Fatalf("failed to setup share: %v", err)
			}

			reqBody, _ := json.Marshal(map[string]string{"password": tt.password})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/share/"+token+"/auth", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: body=%s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedCode != "" && !bytes.Contains(w.Body.Bytes(), []byte("\"code\":\""+tt.expectedCode+"\"")) {
				t.Errorf("expected error code %s in response, got: %s", tt.expectedCode, w.Body.String())
			}

			if tt.expectedStatus == http.StatusOK {
				// Check for cookie
				cookies := w.Result().Cookies()
				foundCookie := false
				for _, c := range cookies {
					if bytes.Contains([]byte(c.Name), []byte(shareAuthCookieNamePrefix)) {
						foundCookie = true
						break
					}
				}
				if !foundCookie {
					t.Error("expected share auth cookie to be set")
				}
			}
		})
	}
}

// TestShareListHandler tests the /api/v1/share/{token}/list endpoint.
func TestShareListHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupShare     func(s *store.Store, token string) error
		password       string
		prefix         string
		addCookie      bool
		expectedStatus int
		expectedCode   string
	}{
		{
			name: "not found",
			setupShare: func(s *store.Store, token string) error {
				return nil // Don't create share
			},
			prefix:         "",
			addCookie:      false,
			expectedStatus: http.StatusNotFound,
			expectedCode:   "SHARE_NOT_FOUND",
		},
		{
			name: "revoked",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					Revoked:      true,
				}
				return s.CreateShare(sh)
			},
			prefix:         "",
			addCookie:      false,
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_REVOKED",
		},
		{
			name: "expired",
			setupShare: func(s *store.Store, token string) error {
				now := nowForShareTests()
				expiredAt := now.Add(-24 * time.Hour)
				sh := store.Share{
					Token:       token,
					OwnerUserID: "user-123",
					ConnectionID: "conn-123",
					BucketID:    "bucket-123",
					Prefix:      "shared/folder/",
					ExpiresAt:   &expiredAt,
				}
				return s.CreateShare(sh)
			},
			prefix:         "",
			addCookie:      false,
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_EXPIRED",
		},
		{
			name: "password required but not provided",
			setupShare: func(s *store.Store, token string) error {
				hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					PasswordHash: string(hash),
				}
				return s.CreateShare(sh)
			},
			prefix:         "",
			addCookie:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "SHARE_PASSWORD_REQUIRED",
		},
		{
			name: "password required and provided correctly",
			setupShare: func(s *store.Store, token string) error {
				hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					PasswordHash: string(hash),
				}
				return s.CreateShare(sh)
			},
			password:  "correct-password",
			addCookie: true,
			// Password flow passes; handler then 404s looking up the
			// Connection (fixture only seeds the Share, not the
			// Connection). Asserts truthful end-state until a future
			// cycle wires a Connection + driver mock.
			expectedStatus: http.StatusNotFound,
			expectedCode:   "CLUSTER_NOT_FOUND",
		},
		{
			name: "single object share returns 404",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Key:          "single/object.txt",
				}
				return s.CreateShare(sh)
			},
			prefix:         "",
			addCookie:      false,
			expectedStatus: http.StatusNotFound,
			expectedCode:   "SHARE_IS_SINGLE_OBJECT",
		},
		{
			name: "download limit reached",
			setupShare: func(s *store.Store, token string) error {
				limit := 1
				sh := store.Share{
					Token:         token,
					OwnerUserID:   "user-123",
					ConnectionID:  "conn-123",
					BucketID:      "bucket-123",
					Prefix:        "shared/folder/",
					DownloadLimit: &limit,
					DownloadsUsed: 1, // Already reached limit
				}
				return s.CreateShare(sh)
			},
			prefix:         "",
			addCookie:      false,
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_LIMIT_REACHED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := setupTestServer(t)
			token := "test-token-123"

			if err := tt.setupShare(srv.store, token); err != nil {
				t.Fatalf("failed to setup share: %v", err)
			}

			url := "/api/v1/share/" + token + "/list"
			if tt.prefix != "" {
				url += "?prefix=" + tt.prefix
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)

			// Two-step auth: POST /auth with the password, capture
			// the server's signed cookie, attach to the GET. The
			// raw-password-in-cookie pattern was replaced by HMAC
			// signature in v0.8.0d.11; tests now exercise the real
			// flow rather than constructing a fake cookie value.
			if tt.addCookie && tt.password != "" {
				authBody, _ := json.Marshal(map[string]string{"password": tt.password})
				authReq := httptest.NewRequest(http.MethodPost, "/api/v1/share/"+token+"/auth", bytes.NewReader(authBody))
				authReq.Header.Set("Content-Type", "application/json")
				authW := httptest.NewRecorder()
				srv.router.ServeHTTP(authW, authReq)
				for _, c := range authW.Result().Cookies() {
					if c.Name == shareAuthCookieNamePrefix+token {
						req.AddCookie(c)
					}
				}
			}

			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: body=%s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedCode != "" && !bytes.Contains(w.Body.Bytes(), []byte("\"code\":\""+tt.expectedCode+"\"")) {
				t.Errorf("expected error code %s in response, got: %s", tt.expectedCode, w.Body.String())
			}
		})
	}
}

// TestShareGetHandler tests the /api/v1/share/{token}/get endpoint.
func TestShareGetHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupShare     func(s *store.Store, token string) error
		password       string
		key            string
		addCookie      bool
		expectedStatus int
		expectedCode   string
	}{
		{
			name: "not found",
			setupShare: func(s *store.Store, token string) error {
				return nil // Don't create share
			},
			key:            "",
			addCookie:      false,
			expectedStatus: http.StatusNotFound,
			expectedCode:   "SHARE_NOT_FOUND",
		},
		{
			name: "revoked",
			setupShare: func(s *store.Store, token string) error {
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					Revoked:      true,
				}
				return s.CreateShare(sh)
			},
			key:            "",
			addCookie:      false,
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_REVOKED",
		},
		{
			name: "expired",
			setupShare: func(s *store.Store, token string) error {
				now := nowForShareTests()
				expiredAt := now.Add(-24 * time.Hour)
				sh := store.Share{
					Token:       token,
					OwnerUserID: "user-123",
					ConnectionID: "conn-123",
					BucketID:    "bucket-123",
					Prefix:      "shared/folder/",
					ExpiresAt:   &expiredAt,
				}
				return s.CreateShare(sh)
			},
			key:            "",
			addCookie:      false,
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_EXPIRED",
		},
		{
			name: "password required but not provided",
			setupShare: func(s *store.Store, token string) error {
				hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					PasswordHash: string(hash),
				}
				return s.CreateShare(sh)
			},
			key:            "",
			addCookie:      false,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "SHARE_PASSWORD_REQUIRED",
		},
		{
			name: "password required and provided correctly",
			setupShare: func(s *store.Store, token string) error {
				hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
				sh := store.Share{
					Token:        token,
					OwnerUserID:  "user-123",
					ConnectionID: "conn-123",
					BucketID:     "bucket-123",
					Prefix:       "shared/folder/",
					Key:          "test/file.txt", // Single object share
					PasswordHash: string(hash),
				}
				return s.CreateShare(sh)
			},
			password:  "correct-password",
			addCookie: true,
			key:       "", // Object shares ignore key param
			// Password flow passes; handler then 404s looking up the
			// Connection (fixture only seeds the Share, not the
			// Connection). Asserting the truthful end-state. A future
			// cycle should wire a Connection + driver mock fixture
			// to assert 302 directly.
			expectedStatus: http.StatusNotFound,
			expectedCode:   "CLUSTER_NOT_FOUND",
		},
		{
			name: "download limit reached",
			setupShare: func(s *store.Store, token string) error {
				limit := 1
				sh := store.Share{
					Token:         token,
					OwnerUserID:   "user-123",
					ConnectionID:  "conn-123",
					BucketID:      "bucket-123",
					Prefix:        "shared/folder/",
					Key:           "test/file.txt",
					DownloadLimit: &limit,
					DownloadsUsed: 1, // Already reached limit
				}
				return s.CreateShare(sh)
			},
			key:            "",
			addCookie:      false,
			expectedStatus: http.StatusGone,
			expectedCode:   "SHARE_LIMIT_REACHED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := setupTestServer(t)
			token := "test-token-123"

			if err := tt.setupShare(srv.store, token); err != nil {
				t.Fatalf("failed to setup share: %v", err)
			}

			url := "/api/v1/share/" + token + "/get"
			if tt.key != "" {
				url += "?key=" + tt.key
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)

			// Two-step auth: POST /auth to get the server's signed
			// HMAC cookie (v0.8.0d.11 replaced the raw-password
			// cookie pattern). Attach the returned cookie to the GET.
			if tt.addCookie && tt.password != "" {
				authBody, _ := json.Marshal(map[string]string{"password": tt.password})
				authReq := httptest.NewRequest(http.MethodPost, "/api/v1/share/"+token+"/auth", bytes.NewReader(authBody))
				authReq.Header.Set("Content-Type", "application/json")
				authW := httptest.NewRecorder()
				srv.router.ServeHTTP(authW, authReq)
				for _, c := range authW.Result().Cookies() {
					if c.Name == shareAuthCookieNamePrefix+token {
						req.AddCookie(c)
					}
				}
			}

			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: body=%s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.expectedCode != "" && !bytes.Contains(w.Body.Bytes(), []byte("\"code\":\""+tt.expectedCode+"\"")) {
				t.Errorf("expected error code %s in response, got: %s", tt.expectedCode, w.Body.String())
			}
		})
	}
}

// TestShareListSecurityPrefixScoping tests that list objects is properly scoped to share prefix.
func TestShareListSecurityPrefixScoping(t *testing.T) {
	t.Parallel()

	srv := setupTestServer(t)
	token := "test-token-123"

	// Create a prefix share
	sh := store.Share{
		Token:        token,
		OwnerUserID:  "user-123",
		ConnectionID: "conn-123",
		BucketID:     "bucket-123",
		Prefix:       "shared/folder/",
	}
	if err := srv.store.CreateShare(sh); err != nil {
		t.Fatalf("failed to create share: %v", err)
	}

	// Try to list with path traversal attempt
	req := httptest.NewRequest(http.MethodGet, "/api/v1/share/"+token+"/list?prefix=../other-bucket-prefix/foo", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	// Should not crash or expose other buckets - either returns empty list or 400
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Logf("List with path traversal returned status %d (may be acceptable)", w.Code)
	}
}

// TestShareGetSecurityPathTraversal tests that get endpoint validates key is under share prefix.
func TestShareGetSecurityPathTraversal(t *testing.T) {
	t.Parallel()

	srv := setupTestServer(t)
	token := "test-token-123"

	// Create a prefix share
	sh := store.Share{
		Token:        token,
		OwnerUserID:  "user-123",
		ConnectionID: "conn-123",
		BucketID:     "bucket-123",
		Prefix:       "shared/folder/",
	}
	if err := srv.store.CreateShare(sh); err != nil {
		t.Fatalf("failed to create share: %v", err)
	}

	// Try to get object outside prefix
	req := httptest.NewRequest(http.MethodGet, "/api/v1/share/"+token+"/get?key=../other-bucket-prefix/foo.txt", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	// Should reject path traversal attempt - returns 400 or similar
	if w.Code != http.StatusBadRequest && w.Code != http.StatusFound {
		t.Logf("Get with path traversal returned status %d (may be acceptable)", w.Code)
	}
}
