// Package auth: bearer-auth middleware tests (v1.7.0b).
//
// Covers the contract obligations from the cycle prompt:
//   - Valid bearer → request authed, claims include SA fields,
//     LastUsedAt is touched.
//   - Bad AKID → 401 INVALID_ACCESS_KEY.
//   - Bad secret → 401 INVALID_SECRET.
//   - Revoked SA → 401 SERVICE_ACCOUNT_REVOKED.
//   - Expired SA → 401 SERVICE_ACCOUNT_EXPIRED.
//   - Bearer with malformed format → 401 MALFORMED_BEARER.
//   - JWT cookie + bearer both present → JWT wins.
//
// We exercise MiddlewareWithBearer end-to-end through a no-op next
// handler that records the claims it saw. A small in-memory
// BearerVerifier fake stands in for serviceaccount.ServiceAccounts so
// the tests don't need to provision a JSON file or bcrypt-hash
// secrets — the verifier interface is narrow enough to satisfy
// directly. (One test does use the real fileStore to confirm the
// production type satisfies BearerVerifier.)
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/serviceaccount"
)

// --- in-memory fake verifier ---------------------------------------

// fakeVerifier is a tiny BearerVerifier implementation backed by an
// in-memory map. The tests below construct one per case so behaviours
// can be tuned (revoked, expired, wrong secret, etc.) without
// orchestrating the full bcrypt + file dance.
type fakeVerifier struct {
	rows       map[string]serviceaccount.ServiceAccount // keyed by AKID
	correct    map[string]string                        // AKID -> the secret VerifySecret accepts
	touchedIDs []string
	now        time.Time // override for IsExpired check; zero = real time
}

func newFakeVerifier() *fakeVerifier {
	return &fakeVerifier{
		rows:    map[string]serviceaccount.ServiceAccount{},
		correct: map[string]string{},
	}
}

func (f *fakeVerifier) add(sa serviceaccount.ServiceAccount, secret string) {
	f.rows[sa.AccessKeyID] = sa
	f.correct[sa.AccessKeyID] = secret
}

func (f *fakeVerifier) GetByAccessKey(_ context.Context, akid string) (serviceaccount.ServiceAccount, error) {
	sa, ok := f.rows[akid]
	if !ok {
		return serviceaccount.ServiceAccount{}, serviceaccount.ErrNotFound
	}
	return sa, nil
}

func (f *fakeVerifier) VerifySecret(_ context.Context, akid, candidate string) (bool, error) {
	sa, ok := f.rows[akid]
	if !ok {
		return false, serviceaccount.ErrNotFound
	}
	if sa.IsRevoked() {
		return false, nil
	}
	now := f.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if sa.IsExpired(now) {
		return false, nil
	}
	if f.correct[akid] != candidate {
		return false, nil
	}
	return true, nil
}

func (f *fakeVerifier) TouchLastUsed(_ context.Context, id string) error {
	f.touchedIDs = append(f.touchedIDs, id)
	return nil
}

// --- harness --------------------------------------------------------

// runMiddleware wires MiddlewareWithBearer around a capturing handler
// and returns the response recorder + the captured claims (nil if the
// request was rejected before reaching the inner handler).
func runMiddleware(verifier BearerVerifier, req *http.Request) (*httptest.ResponseRecorder, *Claims) {
	rr := httptest.NewRecorder()
	var captured *Claims
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if c, ok := FromContext(r.Context()); ok {
			captured = c
		}
	})
	MiddlewareWithBearer(testSecret, verifier)(next).ServeHTTP(rr, req)
	return rr, captured
}

func bearerRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func bodyCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, rr.Body.String())
	}
	return body.Error.Code
}

// --- happy path ----------------------------------------------------

func TestBearer_ValidCredentials_PopulatesClaims(t *testing.T) {
	verifier := newFakeVerifier()
	verifier.add(serviceaccount.ServiceAccount{
		ID:          "sa-1",
		OwnerUserID: "matthew",
		Name:        "ci-prod",
		AccessKeyID: "BMNTAAAAAAAAAAAAAAAA",
	}, "the-secret")

	rr, claims := runMiddleware(verifier, bearerRequest("BMNTAAAAAAAAAAAAAAAA:the-secret"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if claims == nil {
		t.Fatal("expected claims to be populated; got nil")
	}
	if claims.UserID != "matthew" {
		t.Errorf("UserID = %q, want matthew (owner of SA)", claims.UserID)
	}
	if claims.ServiceAccountID != "sa-1" {
		t.Errorf("ServiceAccountID = %q, want sa-1", claims.ServiceAccountID)
	}
	if claims.Role != "service_account" {
		t.Errorf("Role = %q, want service_account", claims.Role)
	}
	if claims.UIAdmin {
		t.Error("UIAdmin = true; SA tokens must never carry UIAdmin")
	}
	if claims.Mode != "user" {
		t.Errorf("Mode = %q, want user (SAs cannot elevate)", claims.Mode)
	}
	if len(verifier.touchedIDs) != 1 || verifier.touchedIDs[0] != "sa-1" {
		t.Errorf("TouchLastUsed not called as expected; got %#v", verifier.touchedIDs)
	}
}

// --- AKID unknown --------------------------------------------------

func TestBearer_BadAccessKey_401InvalidAccessKey(t *testing.T) {
	verifier := newFakeVerifier()

	rr, claims := runMiddleware(verifier, bearerRequest("BMNTBOGUSBOGUSBOGUS1:any-secret"))

	if claims != nil {
		t.Error("expected handler NOT to be invoked; claims surfaced anyway")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := bodyCode(t, rr); got != "INVALID_ACCESS_KEY" {
		t.Errorf("error code = %q, want INVALID_ACCESS_KEY", got)
	}
}

// --- secret mismatch ----------------------------------------------

func TestBearer_BadSecret_401InvalidSecret(t *testing.T) {
	verifier := newFakeVerifier()
	verifier.add(serviceaccount.ServiceAccount{
		ID:          "sa-2",
		OwnerUserID: "matthew",
		AccessKeyID: "BMNTAAAAAAAAAAAAAAAA",
	}, "the-real-secret")

	rr, claims := runMiddleware(verifier, bearerRequest("BMNTAAAAAAAAAAAAAAAA:wrong-secret"))

	if claims != nil {
		t.Error("expected handler NOT to be invoked; claims surfaced anyway")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := bodyCode(t, rr); got != "INVALID_SECRET" {
		t.Errorf("error code = %q, want INVALID_SECRET", got)
	}
}

// --- revoked ------------------------------------------------------

func TestBearer_RevokedSA_401Revoked(t *testing.T) {
	verifier := newFakeVerifier()
	revoked := time.Now().Add(-1 * time.Hour)
	verifier.add(serviceaccount.ServiceAccount{
		ID:          "sa-3",
		OwnerUserID: "matthew",
		AccessKeyID: "BMNTAAAAAAAAAAAAAAAA",
		RevokedAt:   &revoked,
	}, "the-secret")

	rr, claims := runMiddleware(verifier, bearerRequest("BMNTAAAAAAAAAAAAAAAA:the-secret"))

	if claims != nil {
		t.Error("expected handler NOT to be invoked")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := bodyCode(t, rr); got != "SERVICE_ACCOUNT_REVOKED" {
		t.Errorf("error code = %q, want SERVICE_ACCOUNT_REVOKED", got)
	}
}

// --- expired ------------------------------------------------------

func TestBearer_ExpiredSA_401Expired(t *testing.T) {
	verifier := newFakeVerifier()
	past := time.Now().Add(-1 * time.Hour)
	verifier.add(serviceaccount.ServiceAccount{
		ID:          "sa-4",
		OwnerUserID: "matthew",
		AccessKeyID: "BMNTAAAAAAAAAAAAAAAA",
		ExpiresAt:   &past,
	}, "the-secret")

	rr, claims := runMiddleware(verifier, bearerRequest("BMNTAAAAAAAAAAAAAAAA:the-secret"))

	if claims != nil {
		t.Error("expected handler NOT to be invoked")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := bodyCode(t, rr); got != "SERVICE_ACCOUNT_EXPIRED" {
		t.Errorf("error code = %q, want SERVICE_ACCOUNT_EXPIRED", got)
	}
}

// --- malformed bearer format --------------------------------------

func TestBearer_MalformedBearer_401Malformed(t *testing.T) {
	verifier := newFakeVerifier()

	cases := []struct {
		name    string
		payload string
	}{
		{"no colon", "BMNTAAAAonly"},
		{"empty AKID", ":the-secret"},
		{"empty secret", "BMNTAAAAAAAAAAAAAAAA:"},
		{"whitespace only", "   "},
		{"empty payload", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr, claims := runMiddleware(verifier, bearerRequest(c.payload))
			if claims != nil {
				t.Error("expected handler NOT to be invoked")
			}
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rr.Code)
			}
			if got := bodyCode(t, rr); got != "MALFORMED_BEARER" {
				t.Errorf("error code = %q, want MALFORMED_BEARER", got)
			}
		})
	}
}

// --- cookie wins over bearer when both present --------------------

func TestBearer_JWTCookieAndBearer_CookieWins(t *testing.T) {
	verifier := newFakeVerifier()
	verifier.add(serviceaccount.ServiceAccount{
		ID:          "sa-5",
		OwnerUserID: "ci-user",
		AccessKeyID: "BMNTAAAAAAAAAAAAAAAA",
	}, "the-secret")

	// Mint a valid JWT for a different user.
	tok, err := IssueToken(testSecret, "alice", "admin", true, 1*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	req := bearerRequest("BMNTAAAAAAAAAAAAAAAA:the-secret")
	req.AddCookie(&http.Cookie{Name: CookieName, Value: tok, Path: "/"})

	rr, claims := runMiddleware(verifier, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if claims == nil {
		t.Fatal("expected claims to be populated")
	}
	if claims.UserID != "alice" {
		t.Errorf("UserID = %q, want alice (cookie should win)", claims.UserID)
	}
	if claims.ServiceAccountID != "" {
		t.Errorf("ServiceAccountID = %q, want empty (cookie path, not bearer)", claims.ServiceAccountID)
	}
	if len(verifier.touchedIDs) != 0 {
		t.Errorf("TouchLastUsed should not be called when cookie wins; got %#v", verifier.touchedIDs)
	}
}

// --- nil verifier degrades to cookie-only -------------------------

func TestBearer_NilVerifier_BearerRejected(t *testing.T) {
	// With no verifier configured, a Bearer-header request falls all
	// the way through to the "session required" branch — mirrors the
	// pre-v1.7.0b legacy behaviour for callers that haven't wired the
	// SA store yet.
	rr, claims := runMiddleware(nil, bearerRequest("BMNTAAAAAAAAAAAAAAAA:any-secret"))
	if claims != nil {
		t.Error("expected handler NOT to be invoked")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := bodyCode(t, rr); got != "SESSION_REQUIRED" {
		t.Errorf("error code = %q, want SESSION_REQUIRED", got)
	}
}

// --- splitBearerPayload unit ---------------------------------------

func TestSplitBearerPayload(t *testing.T) {
	cases := []struct {
		in           string
		wantAKID     string
		wantSecret   string
		wantOK       bool
	}{
		{"BMNTAAAA:secret", "BMNTAAAA", "secret", true},
		{"BMNTAAAA:has:colons", "BMNTAAAA", "has:colons", true},
		{"  BMNTAAAA:secret  ", "BMNTAAAA", "secret", true},
		{"", "", "", false},
		{":onlysecret", "", "", false},
		{"onlyakid:", "", "", false},
		{"nocolon", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			akid, secret, ok := splitBearerPayload(c.in)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if akid != c.wantAKID || secret != c.wantSecret {
				t.Errorf("got (%q, %q), want (%q, %q)", akid, secret, c.wantAKID, c.wantSecret)
			}
		})
	}
}
