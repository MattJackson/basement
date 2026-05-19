package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// TestAuthEndpoints tests login, logout, me, and capabilities handlers.
func TestAuthEndpoints(t *testing.T) {
	// Generate a test bcrypt hash for password "test"
	testPasswordHash := "$2a$12$sbmfdAJgsk09h5tQrKQkdu9QK2rhwQgMypco87QpYUWIDRFxh7D96"

	// Create a test JWT secret (32 bytes)
	testJWTSecret := make([]byte, 32)
	for i := range testJWTSecret {
		testJWTSecret[i] = byte(i)
	}

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		setupCookie    bool
		token          string
		wantStatus     int
		checkSetCookie bool
		checkBody      func(body []byte, t *testing.T) error
	}{
		// Login tests
		{
			name:       "happy path - matching creds",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]string{"username": "admin", "password": "test"},
			wantStatus: 200,
			checkSetCookie: true,
			checkBody: func(body []byte, t *testing.T) error {
				var resp UserResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					return err
				}
				if resp.Username != "admin" {
					t.Logf("expected username 'admin', got %q", resp.Username)
					return errors.New("username mismatch")
				}
				if resp.Role != "admin" {
					t.Logf("expected role 'admin', got %q", resp.Role)
					return errors.New("role mismatch")
				}
				return nil
			},
		},
		{
			name:       "wrong username",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]string{"username": "wronguser", "password": "test"},
			wantStatus: 401,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "INVALID_CREDENTIALS" {
					t.Logf("expected code 'INVALID_CREDENTIALS', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},
		{
			name:       "wrong password",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]string{"username": "admin", "password": "wrongpass"},
			wantStatus: 401,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "INVALID_CREDENTIALS" {
					t.Logf("expected code 'INVALID_CREDENTIALS', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},
		{
			name:       "missing username",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]string{"password": "test"},
			wantStatus: 400,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "INVALID_REQUEST" {
					t.Logf("expected code 'INVALID_REQUEST', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},
		{
			name:       "missing password",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]string{"username": "admin"},
			wantStatus: 400,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "INVALID_REQUEST" {
					t.Logf("expected code 'INVALID_REQUEST', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},
		{
			name:       "missing fields",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]string{},
			wantStatus: 400,
		},

		// Logout tests
		{
			name:           "logout - clears cookie",
			method:         http.MethodPost,
			path:           "/api/v1/auth/logout",
			setupCookie:    true,
			token:          generateTestToken("admin", "admin"),
			wantStatus:     204,
			checkSetCookie: true, // cookie should be set with MaxAge=-1
		},

		// Me tests - without cookie (should 401)
		{
			name:       "me without cookie - 401",
			method:     http.MethodGet,
			path:       "/api/v1/auth/me",
			wantStatus: 401,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "SESSION_REQUIRED" {
					t.Logf("expected code 'SESSION_REQUIRED', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},

		// Me tests - with valid cookie (should 200)
		{
			name:      "me with valid cookie - 200",
			method:    http.MethodGet,
			path:      "/api/v1/auth/me",
			token:     generateTestToken("admin", "admin"),
			wantStatus: 200,
			checkBody: func(body []byte, t *testing.T) error {
				var resp UserResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					return err
				}
				if resp.Username != "admin" {
					t.Logf("expected username 'admin', got %q", resp.Username)
					return errors.New("username mismatch")
				}
				if resp.Role != "admin" {
					t.Logf("expected role 'admin', got %q", resp.Role)
					return errors.New("role mismatch")
				}
				return nil
			},
		},
		{
			name:      "me with different user cookie - 200",
			method:    http.MethodGet,
			path:      "/api/v1/auth/me",
			token:     generateTestToken("testuser", "admin"),
			wantStatus: 200,
			checkBody: func(body []byte, t *testing.T) error {
				var resp UserResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					return err
				}
				if resp.Username != "testuser" {
					t.Logf("expected username 'testuser', got %q", resp.Username)
					return errors.New("username mismatch")
				}
				return nil
			},
		},

		// Capabilities tests - without cookie (should 401)
		{
			name:       "capabilities without cookie - 401",
			method:     http.MethodGet,
			path:       "/api/v1/capabilities",
			wantStatus: 401,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "SESSION_REQUIRED" {
					t.Logf("expected code 'SESSION_REQUIRED', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},

		// Capabilities tests - with valid cookie (should 501 for stub driver)
		{
			name:      "capabilities with cookie - 501 DRIVER_UNSUPPORTED",
			method:    http.MethodGet,
			path:      "/api/v1/capabilities",
			token:     generateTestToken("admin", "admin"),
			wantStatus: 501,
			checkBody: func(body []byte, t *testing.T) error {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					return err
				}
				if errResp.Error.Code != "DRIVER_UNSUPPORTED" {
					t.Logf("expected code 'DRIVER_UNSUPPORTED', got %q", errResp.Error.Code)
					return errors.New("wrong error code")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup config
			cfg := &config.Config{
				Listen:     ":0",
				SessionTTL: 24 * time.Hour,
				Admin: config.AdminConfig{
					User:         "admin",
					PasswordHash: testPasswordHash,
				},
				JWT: config.JWTConfig{
					Secret: testJWTSecret,
				},
			}

			// Setup store (nil is fine for auth tests)
			store := &store.Store{}

			// Setup stub driver that returns ErrUnsupported for Capabilities
			drv := &stubDriver{}

			// Create server
			srv := New(cfg, store, nil, drv, nil)

			// Create request
			var bodyBytes []byte
			if tt.body != nil {
				bodyBytes, _ = json.Marshal(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			// Setup cookie if needed
			if tt.setupCookie || tt.token != "" {
				token := tt.token
				if token == "" {
					token = generateTestToken("admin", "admin")
				}
				req.AddCookie(&http.Cookie{
					Name:     auth.CookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
				})
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Serve request
			srv.router.ServeHTTP(rr, req)

			// Check status code
			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.wantStatus, rr.Code, rr.Body.String())
			}

			// Check Set-Cookie header if needed
			if tt.checkSetCookie {
				cookies := rr.Result().Cookies()
				found := false
				for _, c := range cookies {
					if c.Name == auth.CookieName {
						found = true
						// For logout, cookie should have MaxAge=-1 or be empty
						if tt.name == "logout - clears cookie" {
							if c.Value != "" || c.MaxAge != -1 {
								t.Errorf("expected cookie cleared (MaxAge=-1), got Value=%q MaxAge=%d", c.Value, c.MaxAge)
							}
						}
						break
					}
				}
				if !found && tt.wantStatus == 200 {
					t.Error("expected Set-Cookie header with session cookie")
				}
			}

			// Check response body if provided
			if tt.checkBody != nil {
				if err := tt.checkBody(rr.Body.Bytes(), t); err != nil {
					t.Errorf("body check failed: %v", err)
				}
			}
		})
	}
}

// generateTestToken creates a JWT token for testing.
func generateTestToken(userID, role string) string {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	token, err := auth.IssueToken(secret, userID, role, 24*time.Hour)
	if err != nil {
		panic(err)
	}
	return token
}

// stubDriver implements driver.Driver for testing.
type stubDriver struct{}

func (s *stubDriver) Capabilities(_ context.Context) (driver.Caps, error) {
	return driver.Caps{}, driver.ErrUnsupported
}

func (s *stubDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{}, nil
}

func (s *stubDriver) ListNodes(_ context.Context) ([]driver.Node, error) {
	return []driver.Node{{ID: "n1", Hostname: "h1", Address: "a1", Zone: "z1", Role: "storage", Capacity: 1000, Tags: nil, Status: "connected", Version: "v1"}}, nil
}

func (s *stubDriver) GetLayout(_ context.Context) (driver.Layout, error) {
	return driver.Layout{Nodes: []driver.Node{{ID: "n1"}}}, nil
}

func (s *stubDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, driver.ErrUnsupported
}

func (s *stubDriver) ApplyLayout(_ context.Context) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) RevertLayout(_ context.Context) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) {
	return nil, driver.ErrUnsupported
}

func (s *stubDriver) GetBucket(_ context.Context, _ string) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}

func (s *stubDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}

func (s *stubDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}

func (s *stubDriver) DeleteBucket(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) ListKeys(_ context.Context) ([]driver.Key, error) {
	return nil, driver.ErrUnsupported
}

func (s *stubDriver) GetKey(_ context.Context, _ string) (driver.Key, error) {
	return driver.Key{}, driver.ErrUnsupported
}

func (s *stubDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, driver.ErrUnsupported
}

func (s *stubDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) DeleteKey(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) ListObjects(_ context.Context, _, _, _ string, _ int) (driver.ObjectPage, error) {
	return driver.ObjectPage{}, driver.ErrUnsupported
}

func (s *stubDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) {
	return driver.ObjectInfo{}, driver.ErrUnsupported
}

func (s *stubDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}

func (s *stubDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}

func (s *stubDriver) DeleteObject(_ context.Context, _, _ string) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, driver.ErrUnsupported
}

func (s *stubDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}

func (s *stubDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error {
	return driver.ErrUnsupported
}

// TestMain allows running setup before tests.
func TestMain(m *testing.M) {
	// Any global setup can go here
	exitCode := m.Run()
	os.Exit(exitCode)
}
