package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/mattjackson/basement/internal/config"
)

// OIDCStateCookieName is the short-lived cookie holding state + nonce
// during an in-flight OIDC authorization code exchange.
//
// SameSite=Lax (not Strict) is required because the OIDC provider
// redirects the browser back to /api/v1/auth/oidc/callback, which is
// a cross-site navigation; Strict would drop the cookie.
const OIDCStateCookieName = "__Host-basement_oidc_state"

// ErrOIDCNonceMismatch is returned by VerifyIDToken when the nonce in the
// verified ID token does not equal the nonce the caller passed in.
var ErrOIDCNonceMismatch = errors.New("oidc: nonce mismatch")

// OIDCClaims is the subset of OIDC ID-token claims basement consumes
// to provision users. Provider holds the issuer URL.
type OIDCClaims struct {
	Subject  string
	Email    string
	Name     string
	Provider string
}

// idTokenPayload is the raw JSON shape we decode standard claims from.
type idTokenPayload struct {
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

// OIDCProvider wraps a coreos/go-oidc Provider + oauth2.Config so
// callers can perform authorization-code flow without depending on
// either library directly.
type OIDCProvider struct {
	cfg      config.OIDCConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// NewOIDCProvider discovers the issuer's OIDC configuration and returns
// a ready-to-use provider. Returns an error if discovery fails or any
// of cfg.Issuer / cfg.ClientID / cfg.ClientSecret / cfg.RedirectURL is
// empty.
func NewOIDCProvider(ctx context.Context, cfg config.OIDCConfig) (*OIDCProvider, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("oidc: issuer is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("oidc: client_id is required")
	}
	if cfg.ClientSecret == "" {
		return nil, errors.New("oidc: client_secret is required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("oidc: redirect_url is required")
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery failed: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &OIDCProvider{
		cfg:      cfg,
		provider: provider,
		verifier: verifier,
		oauth:    oauthCfg,
	}, nil
}

// Issuer returns the configured issuer URL. This is also the Provider
// value persisted alongside user records.
func (p *OIDCProvider) Issuer() string {
	return p.cfg.Issuer
}

// AutoProvision returns the configured AutoProvision flag.
func (p *OIDCProvider) AutoProvision() bool {
	return p.cfg.AutoProvision
}

// AuthCodeURL builds the provider's authorization endpoint URL with the
// given state + nonce. Callers must persist both values out-of-band
// (typically in a short-lived signed cookie) and compare them on the
// callback to defend against CSRF + token replay.
func (p *OIDCProvider) AuthCodeURL(state, nonce string) string {
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
}

// Exchange swaps an authorization code for an OAuth2 token. The returned
// token carries the raw ID token in token.Extra("id_token").
func (p *OIDCProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.oauth.Exchange(ctx, code)
}

// IDTokenFromOAuth2 extracts and returns the raw id_token string from a
// successful OAuth2 token response. Returns an error if id_token is
// missing (e.g. the provider returned only an access token).
func IDTokenFromOAuth2(tok *oauth2.Token) (string, error) {
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", errors.New("oidc: no id_token in token response")
	}
	return raw, nil
}

// VerifyIDToken validates a raw ID token against the provider's JWKS,
// audience, expiry, and the provided expectedNonce. On success it returns
// the projected OIDCClaims.
func (p *OIDCProvider) VerifyIDToken(ctx context.Context, rawIDToken, expectedNonce string) (*OIDCClaims, error) {
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: id_token verification failed: %w", err)
	}

	if expectedNonce != "" && idToken.Nonce != expectedNonce {
		return nil, ErrOIDCNonceMismatch
	}

	var payload idTokenPayload
	if err := idToken.Claims(&payload); err != nil {
		return nil, fmt.Errorf("oidc: parsing claims failed: %w", err)
	}

	displayName := payload.Name
	if displayName == "" {
		displayName = payload.PreferredUsername
	}

	return &OIDCClaims{
		Subject:  idToken.Subject,
		Email:    payload.Email,
		Name:     displayName,
		Provider: idToken.Issuer,
	}, nil
}
