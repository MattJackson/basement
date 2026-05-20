package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// TestValidateName_Empty covers the NAME_REQUIRED branch.
func TestValidateName_Empty(t *testing.T) {
	ve := validateName("bucket alias", "", nil, "")
	if ve == nil {
		t.Fatal("expected non-nil validation error for empty name")
	}
	if ve.Status != http.StatusBadRequest {
		t.Errorf("status=%d, want %d", ve.Status, http.StatusBadRequest)
	}
	if ve.Code != "BUCKET_ALIAS_REQUIRED" {
		t.Errorf("code=%q, want BUCKET_ALIAS_REQUIRED", ve.Code)
	}
	if ve.Message != "Bucket alias is required." {
		t.Errorf("message=%q, want \"Bucket alias is required.\"", ve.Message)
	}
}

// TestValidateName_Whitespace covers the trimmed-empty branch.
func TestValidateName_Whitespace(t *testing.T) {
	ve := validateName("key name", "   \t\n  ", nil, "")
	if ve == nil {
		t.Fatal("expected non-nil validation error for whitespace name")
	}
	if ve.Code != "KEY_NAME_REQUIRED" {
		t.Errorf("code=%q, want KEY_NAME_REQUIRED", ve.Code)
	}
}

// TestValidateName_PatternMismatch covers the NAME_INVALID branch.
func TestValidateName_PatternMismatch(t *testing.T) {
	pat := regexp.MustCompile(`^[a-z0-9]+$`)
	ve := validateName("bucket alias", "Bad Name", pat, "must be lowercase alphanumeric")
	if ve == nil {
		t.Fatal("expected validation error for pattern mismatch")
	}
	if ve.Status != http.StatusBadRequest {
		t.Errorf("status=%d, want %d", ve.Status, http.StatusBadRequest)
	}
	if ve.Code != "BUCKET_ALIAS_INVALID" {
		t.Errorf("code=%q, want BUCKET_ALIAS_INVALID", ve.Code)
	}
	if ve.Message != "Bucket alias is invalid: must be lowercase alphanumeric" {
		t.Errorf("message=%q", ve.Message)
	}
}

// TestValidateName_PatternMatches covers the all-pass branch.
func TestValidateName_PatternMatches(t *testing.T) {
	pat := regexp.MustCompile(`^[a-z0-9-]+$`)
	if ve := validateName("alias", "valid-name", pat, "lowercase"); ve != nil {
		t.Errorf("expected nil for valid name, got %+v", ve)
	}
}

// TestValidateName_NilPattern covers the no-pattern happy path.
func TestValidateName_NilPattern(t *testing.T) {
	if ve := validateName("label", "anything goes here", nil, ""); ve != nil {
		t.Errorf("expected nil for non-empty name with nil pattern, got %+v", ve)
	}
}

// TestValidateName_TrimsBeforeMatching covers trimming before pattern check.
func TestValidateName_TrimsBeforeMatching(t *testing.T) {
	pat := regexp.MustCompile(`^[a-z]+$`)
	if ve := validateName("alias", "   abc   ", pat, "lowercase only"); ve != nil {
		t.Errorf("expected nil for trimmed-matching name, got %+v", ve)
	}
}

// TestRequireUniqueName_Conflict covers the DUPLICATE branch.
func TestRequireUniqueName_Conflict(t *testing.T) {
	type item struct{ name string }
	items := []item{{"foo"}, {"bar"}}
	ve := requireUniqueName("bucket alias", "bar", items, func(i item) []string { return []string{i.name} })
	if ve == nil {
		t.Fatal("expected validation error for duplicate name")
	}
	if ve.Status != http.StatusConflict {
		t.Errorf("status=%d, want %d", ve.Status, http.StatusConflict)
	}
	if ve.Code != "DUPLICATE_BUCKET_ALIAS" {
		t.Errorf("code=%q, want DUPLICATE_BUCKET_ALIAS", ve.Code)
	}
}

// TestRequireUniqueName_NoConflict covers the no-duplicate branch.
func TestRequireUniqueName_NoConflict(t *testing.T) {
	type item struct{ name string }
	items := []item{{"foo"}, {"bar"}}
	if ve := requireUniqueName("label", "baz", items, func(i item) []string { return []string{i.name} }); ve != nil {
		t.Errorf("expected nil for unique name, got %+v", ve)
	}
}

// TestRequireUniqueName_EmptyList covers the empty-input branch.
func TestRequireUniqueName_EmptyList(t *testing.T) {
	if ve := requireUniqueName("label", "anything", []string{}, func(s string) []string { return []string{s} }); ve != nil {
		t.Errorf("expected nil for empty existing list, got %+v", ve)
	}
}

// TestRequireUniqueName_MultipleNamesPerItem covers buckets with several aliases.
func TestRequireUniqueName_MultipleNamesPerItem(t *testing.T) {
	type bucket struct{ aliases []string }
	items := []bucket{
		{aliases: []string{"primary", "alt1", "alt2"}},
		{aliases: []string{"second"}},
	}
	// Match on a non-primary alias — verifies we scan all returned names.
	ve := requireUniqueName("alias", "alt2", items, func(b bucket) []string { return b.aliases })
	if ve == nil {
		t.Fatal("expected conflict matching second-position alias")
	}
	if ve.Code != "DUPLICATE_ALIAS" {
		t.Errorf("code=%q, want DUPLICATE_ALIAS", ve.Code)
	}
}

// TestRequireUniqueName_FieldWithSpaces converts to underscores in code.
func TestRequireUniqueName_FieldWithSpaces(t *testing.T) {
	type item struct{ name string }
	items := []item{{"x"}}
	ve := requireUniqueName("bucket alias", "x", items, func(i item) []string { return []string{i.name} })
	if ve == nil {
		t.Fatal("expected conflict")
	}
	if ve.Code != "DUPLICATE_BUCKET_ALIAS" {
		t.Errorf("code=%q, want DUPLICATE_BUCKET_ALIAS (with spaces converted)", ve.Code)
	}
}

// TestWriteValidationError ensures the helper produces the right HTTP shape.
func TestWriteValidationError(t *testing.T) {
	ve := &validationError{
		Status:  http.StatusBadRequest,
		Code:    "TEST_CODE",
		Message: "test message",
	}
	rr := httptest.NewRecorder()
	writeValidationError(rr, ve)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type=%q", ct)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.Error.Code != "TEST_CODE" {
		t.Errorf("code=%q", resp.Error.Code)
	}
	if resp.Error.Message != "test message" {
		t.Errorf("message=%q", resp.Error.Message)
	}
}

// TestCapitalize covers the unexported helper.
func TestCapitalize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"a", "A"},
		{"foo", "Foo"},
		{"already Capitalized", "Already Capitalized"},
		{"Foo Bar", "Foo Bar"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := capitalize(tt.in); got != tt.want {
				t.Errorf("capitalize(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
