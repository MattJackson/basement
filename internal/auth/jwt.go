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

// Claims extends jwt.RegisteredClaims with UserID and Role fields.
type Claims struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
	*jwt.RegisteredClaims
}

// nowFunc allows tests to inject time for deterministic expiry testing.
var nowFunc = time.Now

// IssueToken creates a JWT with HS256 signing, userID, and role claims.
func IssueToken(secret []byte, userID, role string, ttl time.Duration) (string, error) {
	claims := &Claims{
		UserID: userID,
		Role:   role,
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
