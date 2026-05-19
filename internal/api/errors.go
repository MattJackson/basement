package api

import (
	"encoding/json"
	"net/http"
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
