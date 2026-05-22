// Package auth: tests for ClaimStringValues + VerifyIDTokenWithAllClaims
// (v1.3.0a — OIDC group-claim auto-mapping).
package auth

import (
	"context"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"
)

func TestClaimStringValues_StringScalar(t *testing.T) {
	claims := map[string]interface{}{
		"groups": "admins",
	}
	got := ClaimStringValues(claims, "groups")
	want := []string{"admins"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClaimStringValues=%v, want %v", got, want)
	}
}

func TestClaimStringValues_StringArray(t *testing.T) {
	claims := map[string]interface{}{
		"groups": []interface{}{"admins", "engineers", "everyone"},
	}
	got := ClaimStringValues(claims, "groups")
	sort.Strings(got)
	want := []string{"admins", "engineers", "everyone"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClaimStringValues=%v, want %v", got, want)
	}
}

func TestClaimStringValues_MissingClaimReturnsEmpty(t *testing.T) {
	claims := map[string]interface{}{
		"sub": "user-1",
	}
	got := ClaimStringValues(claims, "groups")
	if len(got) != 0 {
		t.Errorf("ClaimStringValues=%v, want empty", got)
	}
	if got == nil {
		t.Error("ClaimStringValues must return non-nil empty slice")
	}
}

func TestClaimStringValues_EmptyStringDropped(t *testing.T) {
	claims := map[string]interface{}{
		"groups": []interface{}{"admins", "", "engineers"},
	}
	got := ClaimStringValues(claims, "groups")
	want := []string{"admins", "engineers"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClaimStringValues=%v, want %v", got, want)
	}
}

func TestClaimStringValues_NonStringElementsDropped(t *testing.T) {
	claims := map[string]interface{}{
		"groups": []interface{}{"admins", 42, true, "engineers"},
	}
	got := ClaimStringValues(claims, "groups")
	want := []string{"admins", "engineers"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClaimStringValues=%v, want %v", got, want)
	}
}

func TestClaimStringValues_NilMap(t *testing.T) {
	got := ClaimStringValues(nil, "groups")
	if len(got) != 0 {
		t.Errorf("ClaimStringValues=%v, want empty", got)
	}
}

func TestVerifyIDTokenWithAllClaims_ExposesGroupsArray(t *testing.T) {
	p, sign, issuer := newTestOIDCServer(t)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	claims := `{
		"iss":    "` + issuer + `",
		"aud":    "test-client",
		"sub":    "user-7",
		"exp":    ` + exp + `,
		"nonce":  "nc-9",
		"email":  "bob@example.com",
		"groups": ["platform-admins", "engineers"]
	}`

	got, all, err := p.VerifyIDTokenWithAllClaims(context.Background(), sign(claims), "nc-9")
	if err != nil {
		t.Fatalf("VerifyIDTokenWithAllClaims: %v", err)
	}
	if got.Subject != "user-7" {
		t.Errorf("Subject=%q, want \"user-7\"", got.Subject)
	}
	groups := ClaimStringValues(all, "groups")
	want := []string{"platform-admins", "engineers"}
	if !reflect.DeepEqual(groups, want) {
		t.Errorf("groups=%v, want %v", groups, want)
	}
}

func TestVerifyIDTokenWithAllClaims_NonceMismatch(t *testing.T) {
	p, sign, issuer := newTestOIDCServer(t)

	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	claims := `{
		"iss":   "` + issuer + `",
		"aud":   "test-client",
		"sub":   "user-7",
		"exp":   ` + exp + `,
		"nonce": "actual-nonce"
	}`
	_, _, err := p.VerifyIDTokenWithAllClaims(context.Background(), sign(claims), "expected-nonce")
	if err == nil {
		t.Fatal("expected nonce-mismatch error, got nil")
	}
}
