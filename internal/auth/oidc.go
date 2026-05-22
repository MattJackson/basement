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
	AuthTime          int64  `json:"auth_time"`
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

// ElevationAuthCodeURL builds the authorization endpoint URL for the
// sudo-style elevation flow (ADR-0003, v1.2.0c). On top of the normal
// state + nonce it appends:
//
//   - `prompt=<promptParam>` so the IdP forces a fresh re-auth even
//     when it has a cached session. Empty disables the prompt
//     parameter; callers default this from BASEMENT_OIDC_ELEVATION_PROMPT.
//   - `max_age=0` so the IdP rejects any session whose auth_time is
//     not strictly fresh — belt-and-braces for IdPs that ignore prompt.
//
// The returned URL is what the FE redirects the browser to; the
// callback handler then verifies the new ID token's auth_time was
// within ~60s and mints the elevated cookie.
func (p *OIDCProvider) ElevationAuthCodeURL(state, nonce, promptParam string) string {
	opts := []oauth2.AuthCodeOption{
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("max_age", "0"),
	}
	if promptParam != "" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", promptParam))
	}
	return p.oauth.AuthCodeURL(state, opts...)
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
	claims, _, err := p.VerifyIDTokenWithAuthTime(ctx, rawIDToken, expectedNonce)
	return claims, err
}

// VerifyIDTokenWithAuthTime is like VerifyIDToken but additionally
// returns the `auth_time` claim from the ID token (zero if absent).
// Used by the ADR-0003 v1.2.0c elevation callback to confirm the
// re-authentication happened recently.
func (p *OIDCProvider) VerifyIDTokenWithAuthTime(ctx context.Context, rawIDToken, expectedNonce string) (*OIDCClaims, int64, error) {
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, 0, fmt.Errorf("oidc: id_token verification failed: %w", err)
	}

	if expectedNonce != "" && idToken.Nonce != expectedNonce {
		return nil, 0, ErrOIDCNonceMismatch
	}

	var payload idTokenPayload
	if err := idToken.Claims(&payload); err != nil {
		return nil, 0, fmt.Errorf("oidc: parsing claims failed: %w", err)
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
	}, payload.AuthTime, nil
}

// VerifyIDTokenWithAllClaims is like VerifyIDToken but also returns the
// full decoded claim map so callers (the OIDC group-mapping sync,
// v1.3.0a) can inspect provider-specific claims like `groups`,
// `roles`, or anything else the IdP asserts.
//
// The returned map is whatever json.Unmarshal produces for the raw
// payload — top-level keys are strings, values are whatever JSON
// shape the IdP emitted (typically string or []string for group-like
// claims). ClaimStringValues() helps callers normalise a single key
// to a []string regardless of scalar-vs-array shape.
//
// Returns ErrOIDCNonceMismatch on nonce mismatch (same as VerifyIDToken)
// so the elevation + login paths can both share the same sentinel.
func (p *OIDCProvider) VerifyIDTokenWithAllClaims(ctx context.Context, rawIDToken, expectedNonce string) (*OIDCClaims, map[string]interface{}, error) {
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc: id_token verification failed: %w", err)
	}

	if expectedNonce != "" && idToken.Nonce != expectedNonce {
		return nil, nil, ErrOIDCNonceMismatch
	}

	all := map[string]interface{}{}
	if err := idToken.Claims(&all); err != nil {
		return nil, nil, fmt.Errorf("oidc: parsing all claims failed: %w", err)
	}

	var payload idTokenPayload
	if err := idToken.Claims(&payload); err != nil {
		return nil, nil, fmt.Errorf("oidc: parsing claims failed: %w", err)
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
	}, all, nil
}

// ClaimStringValues normalises a single top-level claim into a string
// slice regardless of whether the IdP encoded it as a scalar string or
// a JSON array of strings. Empty string elements are dropped. Returns
// an empty slice (not nil) when the claim is missing, of an unexpected
// shape, or contains no useful values.
//
// IdPs vary: Authentik + Keycloak typically emit `groups` as a JSON
// array; Pocket-ID emits `roles` as a comma-separated string in older
// configs; some custom OIDC providers emit a single scalar string for
// the user's primary role. Treat any non-string element as a noop and
// move on — defensive parsing because the operator can configure any
// claim name they want.
func ClaimStringValues(all map[string]interface{}, name string) []string {
	out := []string{}
	if all == nil || name == "" {
		return out
	}
	raw, ok := all[name]
	if !ok || raw == nil {
		return out
	}
	switch v := raw.(type) {
	case string:
		if v != "" {
			out = append(out, v)
		}
	case []interface{}:
		for _, el := range v {
			if s, ok := el.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
