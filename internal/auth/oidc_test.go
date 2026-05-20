package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"golang.org/x/oauth2"

	"github.com/mattjackson/basement/internal/config"
)

// newTestOIDCServer spins up an in-process OIDC issuer (discovery +
// JWKS) signed by a freshly-generated RSA keypair, and returns:
//   - the configured OIDCProvider
//   - a function that signs ID tokens for arbitrary raw-JSON claims
//   - the httptest.Server.URL (== issuer URL)
func newTestOIDCServer(t *testing.T) (*OIDCProvider, func(rawClaims string) string, string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	srv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{
			{PublicKey: priv.Public(), KeyID: "test-key", Algorithm: oidc.RS256},
		},
	}
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)
	srv.SetIssuer(httpSrv.URL)

	cfg := config.OIDCConfig{
		Issuer:       httpSrv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/api/v1/auth/oidc/callback",
	}

	p, err := NewOIDCProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	sign := func(rawClaims string) string {
		return oidctest.SignIDToken(priv, "test-key", oidc.RS256, rawClaims)
	}

	return p, sign, httpSrv.URL
}

func TestNewOIDCProvider_RequiresFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.OIDCConfig
		want string
	}{
		{
			name: "missing issuer",
			cfg:  config.OIDCConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "u"},
			want: "issuer",
		},
		{
			name: "missing client_id",
			cfg:  config.OIDCConfig{Issuer: "https://x", ClientSecret: "s", RedirectURL: "u"},
			want: "client_id",
		},
		{
			name: "missing client_secret",
			cfg:  config.OIDCConfig{Issuer: "https://x", ClientID: "c", RedirectURL: "u"},
			want: "client_secret",
		},
		{
			name: "missing redirect_url",
			cfg:  config.OIDCConfig{Issuer: "https://x", ClientID: "c", ClientSecret: "s"},
			want: "redirect_url",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOIDCProvider(context.Background(), tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestOIDCProvider_AuthCodeURL_IncludesStateAndNonce(t *testing.T) {
	p, _, _ := newTestOIDCServer(t)

	got := p.AuthCodeURL("st-123", "nc-456")
	if !strings.Contains(got, "state=st-123") {
		t.Errorf("AuthCodeURL missing state: %s", got)
	}
	if !strings.Contains(got, "nonce=nc-456") {
		t.Errorf("AuthCodeURL missing nonce: %s", got)
	}
	if !strings.Contains(got, "response_type=code") {
		t.Errorf("AuthCodeURL missing response_type=code: %s", got)
	}
	if !strings.Contains(got, "scope=openid+profile+email") {
		t.Errorf("AuthCodeURL missing openid+profile+email scopes: %s", got)
	}
}

func TestOIDCProvider_VerifyIDToken_HappyPath(t *testing.T) {
	p, sign, issuer := newTestOIDCServer(t)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	claims := `{
		"iss":   "` + issuer + `",
		"aud":   "test-client",
		"sub":   "user-42",
		"exp":   ` + exp + `,
		"nonce": "nc-456",
		"email": "alice@example.com",
		"name":  "Alice Anderson"
	}`

	got, err := p.VerifyIDToken(context.Background(), sign(claims), "nc-456")
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}
	if got.Subject != "user-42" {
		t.Errorf("Subject=%q, want \"user-42\"", got.Subject)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email=%q, want \"alice@example.com\"", got.Email)
	}
	if got.Name != "Alice Anderson" {
		t.Errorf("Name=%q, want \"Alice Anderson\"", got.Name)
	}
	if got.Provider != issuer {
		t.Errorf("Provider=%q, want %q", got.Provider, issuer)
	}
}

func TestOIDCProvider_VerifyIDToken_NoncMismatchRejected(t *testing.T) {
	p, sign, issuer := newTestOIDCServer(t)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	claims := `{
		"iss":   "` + issuer + `",
		"aud":   "test-client",
		"sub":   "user-1",
		"exp":   ` + exp + `,
		"nonce": "actual-nonce"
	}`

	_, err := p.VerifyIDToken(context.Background(), sign(claims), "expected-nonce")
	if !errors.Is(err, ErrOIDCNonceMismatch) {
		t.Fatalf("expected ErrOIDCNonceMismatch, got %v", err)
	}
}

func TestOIDCProvider_VerifyIDToken_ExpiredTokenRejected(t *testing.T) {
	p, sign, issuer := newTestOIDCServer(t)

	expiredAt := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	claims := `{
		"iss": "` + issuer + `",
		"aud": "test-client",
		"sub": "user-1",
		"exp": ` + expiredAt + `,
		"nonce": "nc"
	}`

	_, err := p.VerifyIDToken(context.Background(), sign(claims), "nc")
	if err == nil {
		t.Fatal("expected expired-token error, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got: %v", err)
	}
}

func TestOIDCProvider_VerifyIDToken_BadAudienceRejected(t *testing.T) {
	p, sign, issuer := newTestOIDCServer(t)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	claims := `{
		"iss": "` + issuer + `",
		"aud": "different-client",
		"sub": "user-1",
		"exp": ` + exp + `,
		"nonce": "nc"
	}`

	_, err := p.VerifyIDToken(context.Background(), sign(claims), "nc")
	if err == nil {
		t.Fatal("expected audience-mismatch error, got nil")
	}
}

func TestIDTokenFromOAuth2_MissingIDToken(t *testing.T) {
	tok := &oauth2.Token{AccessToken: "x"}
	_, err := IDTokenFromOAuth2(tok)
	if err == nil {
		t.Fatal("expected error for missing id_token, got nil")
	}
	if !strings.Contains(err.Error(), "id_token") {
		t.Errorf("error missing 'id_token': %v", err)
	}
}
