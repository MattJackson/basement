package api

import (
	"net/http"
	"regexp"
	"strings"
)

// validation.go centralizes pre-flight validation for create / update
// endpoints. The pattern across buckets, keys, and (soon) connections
// is the same: name required, pattern (sometimes), unique against
// existing peers. Keeping these in one place so behaviour stays
// consistent across resources — operator's directive:
// "feel we are duplicating logic for buckets and keys when really
//  it's all the same".

// validateName returns a non-nil errorResponse (code/status) if name
// fails the basic gate. Empty → 400 NAME_REQUIRED. Non-matching →
// 400 NAME_INVALID. On success returns nil.
//
// `field` is the user-facing field name used in the error message
// ("bucket alias", "key name", "cluster label" etc.) so the dialog
// can surface it verbatim.
type validationError struct {
	Status  int
	Code    string
	Message string
}

func validateName(field, name string, pattern *regexp.Regexp, patternHint string) *validationError {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return &validationError{
			Status:  http.StatusBadRequest,
			Code:    strings.ToUpper(strings.ReplaceAll(field, " ", "_")) + "_REQUIRED",
			Message: capitalize(field) + " is required.",
		}
	}
	if pattern != nil && !pattern.MatchString(trimmed) {
		return &validationError{
			Status:  http.StatusBadRequest,
			Code:    strings.ToUpper(strings.ReplaceAll(field, " ", "_")) + "_INVALID",
			Message: capitalize(field) + " is invalid: " + patternHint,
		}
	}
	return nil
}

// requireUniqueName returns a 409 DUPLICATE_<FIELD> error if `name`
// matches any value returned by `nameOf(item)` across `existing`.
//
// Pass the resource list in `existing`, and a callback that pulls
// the relevant name field out of each item (Key.Name, Bucket.Aliases[i],
// Connection.Label, …). The callback can return multiple names per
// item — return them all and any match triggers the conflict.
func requireUniqueName[T any](field, name string, existing []T, namesOf func(T) []string) *validationError {
	for _, item := range existing {
		for _, n := range namesOf(item) {
			if n == name {
				return &validationError{
					Status:  http.StatusConflict,
					Code:    "DUPLICATE_" + strings.ToUpper(strings.ReplaceAll(field, " ", "_")),
					Message: "A " + field + " with this value already exists. Pick a different one.",
				}
			}
		}
	}
	return nil
}

// writeValidationError serializes a validationError to the response.
// Shared 1-liner so handlers don't repeat the writeError plumbing.
func writeValidationError(w http.ResponseWriter, ve *validationError) {
	writeError(w, ve.Status, ve.Code, ve.Message, nil)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
