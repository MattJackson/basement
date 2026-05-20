package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("test-secret-key-for-hs256-signing-only")

func TestBcryptHashPassword(t *testing.T) {
	password := "mySecurePassword123!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
		t.Errorf("Expected bcrypt hash starting with $2a or $2b, got: %s", hash[:10])
	}

	costStr := hash[4:6]
	if costStr != "12" {
		t.Errorf("Expected bcrypt cost 12, got: %s", costStr)
	}
}

func TestBcryptVerifyPasswordMatch(t *testing.T) {
	password := "testPassword456"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !VerifyPassword(hash, password) {
		t.Error("Expected VerifyPassword to return true for correct password")
	}
}

func TestBcryptVerifyPasswordMismatch(t *testing.T) {
	password := "testPassword789"
	wrongPassword := "wrongPassword"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if VerifyPassword(hash, wrongPassword) {
		t.Error("Expected VerifyPassword to return false for incorrect password")
	}
}

func TestBcryptVerifyPasswordInvalidHash(t *testing.T) {
	invalidHash := "not-a-valid-bcrypt-hash"
	if VerifyPassword(invalidHash, "anypassword") {
		t.Error("Expected VerifyPassword to return false for invalid hash")
	}
}

func TestJwtIssueAndParseToken(t *testing.T) {
	userID := "user-123"
	role := "admin"

	token, err := IssueToken(testSecret, userID, role, true, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	claims, err := ParseToken(testSecret, token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, claims.UserID)
	}

	if claims.Role != role {
		t.Errorf("Expected Role %s, got %s", role, claims.Role)
	}
}

func TestJwtTamperFails(t *testing.T) {
	token, err := IssueToken(testSecret, "user-123", "admin", true, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("Expected JWT with 3 parts, got %d", len(parts))
	}

	tamperedParts := []string{parts[0], parts[1], "tamperedSignature"}
	tamperedToken := strings.Join(tamperedParts, ".")

	_, err = ParseToken(testSecret, tamperedToken)
	if err == nil {
		t.Error("Expected ParseToken to fail on tampered token")
	}
}

func TestJwtExpiredFails(t *testing.T) {
	oldNow := nowFunc
	nowFunc = func() time.Time { return time.Now().Add(-24 * time.Hour) }
	defer func() { nowFunc = oldNow }()

	token, err := IssueToken(testSecret, "user-123", "admin", true, -1*time.Second)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	_, err = ParseToken(testSecret, token)
	if err == nil {
		t.Error("Expected ParseToken to fail on expired token")
	}
}

func TestJwtInvalidAlgorithm(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{
		UserID: "user-123",
		Role:   "admin",
		RegisteredClaims: &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})

	tamperedToken, _ := token.SignedString([]byte{})

	_, err := ParseToken(testSecret, tamperedToken)
	if err == nil {
		t.Error("Expected ParseToken to fail on none algorithm")
	}
}

func TestMiddlewareNoCookie(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(testSecret)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestMiddlewareBadSignature(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(testSecret)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  CookieName,
		Value: "invalid.token.here",
	})
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestMiddlewareValidToken(t *testing.T) {
	token, err := IssueToken(testSecret, "user-456", "admin", true, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := FromContext(r.Context())
		if !ok {
			http.Error(w, "Claims not in context", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-User-ID", claims.UserID)
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(testSecret)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  CookieName,
		Value: token,
	})
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Header().Get("X-User-ID") != "user-456" {
		t.Errorf("Expected user ID 'user-456', got '%s'", rec.Header().Get("X-User-ID"))
	}
}

func TestRequireRoleAdmin(t *testing.T) {
	token, err := IssueToken(testSecret, "user-789", "admin", true, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(testSecret)
	requireRole := RequireRole("admin")
	// Chain: handler -> requireRole -> middleware (middleware runs first and sets claims in context)
	wrapped := middleware(requireRole(handler))

	req := httptest.NewRequest("GET", "/api/admin", nil)
	req.AddCookie(&http.Cookie{
		Name:  CookieName,
		Value: token,
	})
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for admin role, got %d", rec.Code)
	}
}

func TestRequireRoleForbidden(t *testing.T) {
	token, err := IssueToken(testSecret, "user-789", "user", false, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(testSecret)
	requireRole := RequireRole("admin")
	// Chain: handler -> requireRole -> middleware (middleware runs first and sets claims in context)
	wrapped := middleware(requireRole(handler))

	req := httptest.NewRequest("GET", "/api/admin", nil)
	req.AddCookie(&http.Cookie{
		Name:  CookieName,
		Value: token,
	})
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 for non-admin role, got %d", rec.Code)
	}
}

func TestCookieFlags(t *testing.T) {
	token, err := IssueToken(testSecret, "user-test", "admin", true, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	rec := httptest.NewRecorder()

	SetSessionCookie(rec, token, 24*time.Hour)

	cookie := rec.Header().Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("Expected Set-Cookie header to be set")
	}

	expectedPrefix := "__Host-basement_session=" + token
	if !strings.HasPrefix(cookie, expectedPrefix) {
		t.Errorf("Expected cookie to start with %s, got: %s", expectedPrefix[:50], cookie[:lo(50, len(cookie))])
	}

	flags := []string{"HttpOnly", "Secure", "SameSite=Strict", "Path=/"}
	for _, flag := range flags {
		if !strings.Contains(cookie, flag) {
			t.Errorf("Expected cookie to contain '%s', got: %s", flag, cookie)
		}
	}

	if strings.Contains(cookie, "Domain=") {
		t.Error("Expected cookie NOT to have Domain attribute (required for __Host- prefix)")
	}
}

func TestCookieClear(t *testing.T) {
	rec := httptest.NewRecorder()

	ClearSessionCookie(rec)

	cookie := rec.Header().Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("Expected Set-Cookie header to be set for clearing")
	}

	if !strings.Contains(cookie, "__Host-basement_session=") {
		t.Error("Expected cleared cookie to have __Host-basement_session name")
	}

	if !strings.Contains(cookie, "HttpOnly") {
		t.Error("Expected cleared cookie to be HttpOnly")
	}

	if !strings.Contains(cookie, "Secure") {
		t.Error("Expected cleared cookie to be Secure")
	}

	if !strings.Contains(cookie, "SameSite=Strict") {
		t.Error("Expected cleared cookie to have SameSite=Strict")
	}

	if !strings.Contains(cookie, "Path=/") {
		t.Error("Expected cleared cookie to have Path=/")
	}
}

func lo(a, b int) int {
	if a < b {
		return a
	}
	return b
}
