package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrConfirmInvalid is returned by VerifyConfirmToken when the token's
// signature is invalid, the token is malformed, or it has expired.
var ErrConfirmInvalid = errors.New("invalid or expired confirm token")

// ErrConfirmMismatch is returned by VerifyConfirmToken when the token
// signature checks out but the bound op/target/user don't match the
// request — e.g. trying to use a token armed for bucket A to delete
// bucket B, or trying to use someone else's armed token.
var ErrConfirmMismatch = errors.New("confirm token does not match target or actor")

// MintConfirmToken issues a short-lived HMAC-bound token authorizing a
// single destructive operation against a specific target by a specific
// user. The "arm" half of the basement two-phase delete pattern.
//
// Token format: "<expUnix>|<op>|<target>|<userID>.<hexHMAC>"
// HMAC = HMAC-SHA256(secret, "<expUnix>|<op>|<target>|<userID>")
//
// Pipe-delimited (not colon) so op/target/userID may contain colons
// (e.g. "delete:bucket"). None of these fields may legitimately
// contain `|` or `.`; the caller is responsible for that invariant.
//
// Tokens are NOT persisted server-side — verification is purely
// stateless HMAC. Expiry is the only revocation mechanism; pick a
// short TTL (≤ 60s recommended).
func MintConfirmToken(secret []byte, op, target, userID string, ttl time.Duration) string {
	exp := nowFunc().Add(ttl).Unix()
	payload := fmt.Sprintf("%d|%s|%s|%s", exp, op, target, userID)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

// VerifyConfirmToken checks the token signature, expiry, and that the
// op/target/userID encoded in the token match what the request claims.
// Returns nil on success.
//
// Returns ErrConfirmInvalid for malformed tokens, bad signatures, and
// expired tokens. Returns ErrConfirmMismatch when the signature is
// valid but the bound parameters don't match — this is the case the
// caller should surface differently to the operator ("wrong token"
// vs. "no token / expired").
func VerifyConfirmToken(secret []byte, token, expectedOp, expectedTarget, expectedUserID string) error {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return ErrConfirmInvalid
	}
	payload, sigHex := parts[0], parts[1]

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	wantSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(wantSig), []byte(sigHex)) {
		return ErrConfirmInvalid
	}

	fields := strings.SplitN(payload, "|", 4)
	if len(fields) != 4 {
		return ErrConfirmInvalid
	}
	expUnix, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return ErrConfirmInvalid
	}
	if time.Unix(expUnix, 0).Before(nowFunc()) {
		return ErrConfirmInvalid
	}

	op, target, userID := fields[1], fields[2], fields[3]
	if op != expectedOp || target != expectedTarget || userID != expectedUserID {
		return ErrConfirmMismatch
	}
	return nil
}
