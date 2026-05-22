package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrTokenExpired is returned by ParseToken when the JWT has expired.
var ErrTokenExpired = errors.New("token has expired")

// ErrInvalidSignature is returned by ParseToken when the JWT signature is invalid.
var ErrInvalidSignature = errors.New("invalid signature")

// ErrInvalidAlgorithm is returned by ParseToken when the JWT algorithm is unsupported.
var ErrInvalidAlgorithm = errors.New("unsupported algorithm")

// Claims extends jwt.RegisteredClaims with UserID, Role, UIAdmin, and the
// ADR-0003 sudo-style elevation fields (Mode, ModeExpiresAt).
//
// Mode is one of "user" | "admin" | "elevated"; new logins default to
// "user". ModeExpiresAt is a unix seconds timestamp; 0 means the mode
// never expires (USER default). Tokens issued before v1.2.0a omit both
// fields — the policy gate treats those as ADMIN mode for a 7-day grace
// window so matthew's existing session keeps working across the deploy.
// See policy_gates.currentMode + ADR-0003 "Backwards compatibility".
type Claims struct {
	UserID        string `json:"userId"`
	Role          string `json:"role"`
	UIAdmin       bool   `json:"uiAdmin"`
	Mode          string `json:"mode,omitempty"`
	ModeExpiresAt int64  `json:"mext,omitempty"`
	*jwt.RegisteredClaims
}

// nowFunc allows tests to inject time for deterministic expiry testing.
var nowFunc = time.Now

// IssueToken creates a JWT with HS256 signing, userID, role, and uiAdmin
// claims. Mode defaults to "user" and ModeExpiresAt to 0 (never expires
// at this layer — the session JWT's own ExpiresAt still bounds it).
// Callers that need a different mode (sudo-style elevation per ADR-0003)
// use IssueTokenWithMode.
func IssueToken(secret []byte, userID, role string, uiAdmin bool, ttl time.Duration) (string, error) {
	return IssueTokenWithMode(secret, userID, role, uiAdmin, "user", 0, ttl)
}

// IssueTokenWithMode mints a JWT with explicit mode + mode-expiry
// claims. Used by the elevation endpoint after a successful re-auth to
// bump the session into ADMIN or ELEVATED state.
//
// modeExpiresAtUnix is in unix seconds; pass 0 for "never expires at the
// mode layer" (the JWT's own ExpiresAt still bounds the session).
func IssueTokenWithMode(secret []byte, userID, role string, uiAdmin bool, mode string, modeExpiresAtUnix int64, ttl time.Duration) (string, error) {
	if mode == "" {
		mode = "user"
	}
	claims := &Claims{
		UserID:        userID,
		Role:          role,
		UIAdmin:       uiAdmin,
		Mode:          mode,
		ModeExpiresAt: modeExpiresAtUnix,
		RegisteredClaims: &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(nowFunc().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(nowFunc()),
			NotBefore: jwt.NewNumericDate(nowFunc()),
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ParseToken validates a JWT with HS256 algorithm and returns the Claims.
func ParseToken(secret []byte, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, ErrInvalidAlgorithm
		}
		return secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		if strings.Contains(err.Error(), "invalid signature") {
			return nil, ErrInvalidSignature
		}
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidSignature
	}

	return claims, nil
}
