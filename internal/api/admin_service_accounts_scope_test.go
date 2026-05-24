package api

import "testing"

// TestValidateServiceAccountScope_EmptyString
func TestValidateServiceAccountScope_EmptyString(t *testing.T) {
	err := validateServiceAccountScope("")
	if err == nil {
		t.Error("expected error for empty scope")
	}
}

// TestValidateServiceAccountScope_ValidHostStar
func TestValidateServiceAccountScope_ValidHostStar(t *testing.T) {
	err := validateServiceAccountScope("host:*")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateServiceAccountScope_InvalidHostFormat
func TestValidateServiceAccountScope_InvalidHostFormat(t *testing.T) {
	testCases := []string{
		"host:",
		"host:*:extra",
		"host:cluster:*",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}

// TestValidateServiceAccountScope_ValidClusterStar
func TestValidateServiceAccountScope_ValidClusterStar(t *testing.T) {
	err := validateServiceAccountScope("cluster:*")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateServiceAccountScope_ValidClusterWithID
func TestValidateServiceAccountScope_ValidClusterWithID(t *testing.T) {
	err := validateServiceAccountScope("cluster:c123")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateServiceAccountScope_InvalidClusterFormat
func TestValidateServiceAccountScope_InvalidClusterFormat(t *testing.T) {
	testCases := []string{
		"cluster:",
		"cluster",
		"cluster:*:extra",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}

// TestValidateServiceAccountScope_ValidBucket
func TestValidateServiceAccountScope_ValidBucket(t *testing.T) {
	testCases := []string{
		"bucket:c1:b1",
		"bucket:cluster-id:*",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc, err)
		}
	}
}

// TestValidateServiceAccountScope_InvalidBucketFormat
func TestValidateServiceAccountScope_InvalidBucketFormat(t *testing.T) {
	testCases := []string{
		"bucket:",
		"bucket:c1",
		"bucket:",
		"bucket::b1",
		"bucket:c1:b1:extra",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}

// TestValidateServiceAccountScope_ValidKey
func TestValidateServiceAccountScope_ValidKey(t *testing.T) {
	testCases := []string{
		"key:c1:k123",
		"key:cluster-id:*",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc, err)
		}
	}
}

// TestValidateServiceAccountScope_InvalidKeyFormat
func TestValidateServiceAccountScope_InvalidKeyFormat(t *testing.T) {
	testCases := []string{
		"key:",
		"key:c1",
		"key::k123",
		"key:c1:k1:extra",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}

// TestValidateServiceAccountScope_UnknownDomain
func TestValidateServiceAccountScope_UnknownDomain(t *testing.T) {
	testCases := []string{
		"invalid:*",
		"user:c1:b1",
		"foo:bar",
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}

// TestValidateServiceAccountScope_MissingSegments
func TestValidateServiceAccountScope_MissingSegments(t *testing.T) {
	testCases := []string{
		"bucket::b1",   // Missing cluster ID
		"bucket:c1:",   // Missing bucket ID
		"key::k123",    // Missing cluster ID
		"key:c1:",      // Missing key ID
	}
	for _, tc := range testCases {
		err := validateServiceAccountScope(tc)
		if err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}

// TestValidateServiceAccountScope_AllValidScopes
func TestValidateServiceAccountScope_AllValidScopes(t *testing.T) {
	validScopes := []string{
		"host:*",
		"cluster:*",
		"cluster:my-cluster",
		"bucket:c1:*",
		"bucket:c1:b1",
		"key:k123:*",
		"key:c1:k123",
	}
	for _, scope := range validScopes {
		err := validateServiceAccountScope(scope)
		if err != nil {
			t.Errorf("scope %q should be valid: %v", scope, err)
		}
	}
}

// TestValidateServiceAccountScope_AllInvalidScopes
func TestValidateServiceAccountScope_AllInvalidScopes(t *testing.T) {
	invalidScopes := []string{
		"",                           // Empty
		"host:",                      // Incomplete host
		"cluster:",                   // Incomplete cluster
		"bucket:",                    // Missing all bucket parts
		"bucket:c1",                  // Missing bucket ID
		"key:",                       // Missing all key parts
		"key:c1",                     // Missing key ID
		"invalid:*",                  // Unknown domain
		"user:admin:*",               // Unknown domain
	}
	for _, scope := range invalidScopes {
		err := validateServiceAccountScope(scope)
		if err == nil {
			t.Errorf("scope %q should be invalid, got nil", scope)
		}
	}
}
