// Package serviceaccount: basement-issued long-lived access keys for
// automated clients (CI, k8s controllers, CLI tools, MCP servers).
//
// Distinct from store.UserRegion keys, which target a BACKEND (S3, MinIO
// etc.) on the user's behalf — a ServiceAccount authenticates INTO
// basement itself. Its AccessKeyID + secret will be presented over
// SigV4 in v1.7.0b to take the place of a human-issued JWT cookie for
// machine clients; in v2.0 the gateway extends the same substrate to
// route inbound S3 traffic against a basement-issued key.
//
// This cycle (v1.7.0a) ships the data layer + admin API only — the
// SigV4 verification middleware lands in v1.7.0b, the FE in v1.7.0c.
package serviceaccount

import "time"

// Capability is a (policy capability ID, scope) pair granted to a
// service account. Mirrors the (policy.Capability, scope) tuple that
// the JWT-tier enforcer evaluates per-request — the SigV4 verifier in
// v1.7.0b will turn this list into the same call shape so the gate
// logic stays untouched.
//
// ID must reference a capability in internal/auth/policy.Registry.
// Scope must match the policy scope grammar
// (`host:*`, `cluster:{cid}`, `bucket:{cid}:{bid}`, …).
type Capability struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

// ServiceAccount is one issued credential pair plus the policy grants
// it carries.
//
// SecretKeyHash is bcrypt(plaintext). Plaintext is ONLY returned by
// Create + Rotate — never read back from disk, never logged, never
// echoed in subsequent GET / List responses. Callers verifying a
// presented secret call ServiceAccounts.VerifySecret.
//
// RevokedAt implements soft-delete: a row stays on disk after Delete
// so audit greps can still link the AccessKeyID back to a name +
// owner months after revocation. A revoked row never verifies and is
// excluded from name-uniqueness checks (so an operator can re-mint a
// "ci-prod" SA after revoking the old one).
type ServiceAccount struct {
	ID            string       `json:"id"`            // UUID
	OwnerUserID   string       `json:"ownerUserId"`   // who minted it
	Name          string       `json:"name"`          // operator-chosen, e.g. "ci-prod"
	AccessKeyID   string       `json:"accessKeyId"`   // basement-issued, prefix "BMNT" + 16 hex chars
	SecretKeyHash []byte       `json:"secretKeyHash"` // bcrypt hash; plaintext only returned at mint
	Capabilities  []Capability `json:"capabilities"`  // policy capabilities granted
	Scopes        []string     `json:"scopes"`        // scope grammar (host:*, cluster:*, bucket:cid:bid, etc.)
	CreatedAt     time.Time    `json:"createdAt"`
	ExpiresAt     *time.Time   `json:"expiresAt,omitempty"`
	LastUsedAt    *time.Time   `json:"lastUsedAt,omitempty"`
	RevokedAt     *time.Time   `json:"revokedAt,omitempty"`
}

// IsRevoked reports whether the SA has been soft-deleted.
func (s ServiceAccount) IsRevoked() bool {
	return s.RevokedAt != nil && !s.RevokedAt.IsZero()
}

// IsExpired reports whether ExpiresAt is set and in the past relative
// to `now`. Caller passes its own clock for testability.
func (s ServiceAccount) IsExpired(now time.Time) bool {
	return s.ExpiresAt != nil && !s.ExpiresAt.IsZero() && now.After(*s.ExpiresAt)
}
