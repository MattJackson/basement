package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mattjackson/basement/internal/driver"
)

// Error represents a uniform API error response shape per design.md.
type Error struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// ErrorResponse wraps the error in the design.md spec shape.
type ErrorResponse struct {
	Error Error `json:"error"`
}

// writeError writes a uniform error response with the specified status code and details.
func writeError(w http.ResponseWriter, status int, code string, message string, details map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: Error{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

// writeErrorSimple is a convenience wrapper for errors without details.
func writeErrorSimple(w http.ResponseWriter, status int, code string, message string) {
	writeError(w, status, code, message, nil)
}

// writeDriverError translates driver.Error sentinels to HTTP responses per design.md spec.
func writeDriverError(w http.ResponseWriter, op string, err error) {
	var de *driver.Error
	if errors.As(err, &de) {
		switch {
		case errors.Is(err, driver.ErrUnsupported):
			writeError(w, 501, "DRIVER_UNSUPPORTED", de.Message, nil)
		case errors.Is(err, driver.ErrNotFound):
			writeError(w, 404, "NOT_FOUND", de.Message, nil)
		case errors.Is(err, driver.ErrPermissionDenied):
			writeError(w, 403, "DRIVER_FORBIDDEN", de.Message, nil)
		case errors.Is(err, driver.ErrConflict):
			writeError(w, 409, "CONFLICT", de.Message, nil)
		case errors.Is(err, driver.ErrInvalid):
			writeError(w, 400, "INVALID", de.Message, nil)
		default:
			writeError(w, 500, "INTERNAL", "driver error", nil)
		}
		return
	}
	writeError(w, 500, "INTERNAL", "internal error", nil)
}

// writeJSON encodes a value as JSON with proper Content-Type header.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
