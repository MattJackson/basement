package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattjackson/basement/internal/auth"
		"github.com/mattjackson/basement/internal/store"
)

// createUserClusterRequest represents the payload for creating a user-owned cluster.
type createUserClusterRequest struct {
	Label  string             `json:"label"`
	Driver string             `json:"driver"`
	Config map[string]string  `json:"config"`
	Color  string             `json:"color,omitempty"`
}

// createUserClusterHandler handles POST /api/v1/user/clusters.
// Creates a new Connection record with ownerScope = <userID>.
// Server-side gates:
//   1. OrgCapabilities.AllowUserBackends == true, else 403 with code "USER_BACKENDS_DISABLED".
//   2. driver in OrgCapabilities.UserBackendDrivers, else 403 with code "DRIVER_NOT_ENABLED_FOR_USERS".
//   3. Tests connection synchronously via driver HealthCheck before saving. If test fails, returns 400.
func (s *Server) createUserClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	// Get current user claims.
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	// Parse request body.
	var req createUserClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	// Validate required fields.
	req.Label = strings.TrimSpace(req.Label)
	if req.Label == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Label is required")
		return
	}
	if req.Driver == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Driver is required")
		return
	}
	if req.Config == nil || len(req.Config) == 0 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Config is required")
		return
	}

	// Gate 1: Check AllowUserBackends capability.
	caps := s.store.OrgCapabilities().Get()
	if !caps.AllowUserBackends {
		writeError(w, http.StatusForbidden, "USER_BACKENDS_DISABLED", "Adding user clusters is disabled by your administrator.", nil)
		return
	}

	// Gate 2: Check driver is in UserBackendDrivers (if non-empty whitelist).
	userDrivers := caps.UserBackendDrivers
	if len(userDrivers) > 0 && !slices.Contains(userDrivers, req.Driver) {
		writeError(w, http.StatusForbidden, "DRIVER_NOT_ENABLED_FOR_USERS", "This driver is not enabled for user clusters.", nil)
		return
	}

	// Validate driver is in supported drivers list.
	if !store.SupportedDrivers[req.Driver] {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_DRIVER", "Driver not supported")
		return
	}

	// Gate 3: Test connection synchronously via HealthCheck.
	drv, err := s.reg.For(r.Context(), req.Driver)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DRIVER_REGISTRY_ERROR", "Driver registry error: "+err.Error(), nil)
		return
	}

	health, err := drv.HealthCheck(r.Context())
	if err != nil {
		var details map[string]interface{}
		if hBytes, _ := json.Marshal(health); len(hBytes) > 0 {
			json.Unmarshal(hBytes, &details)
		}
		writeError(w, http.StatusBadRequest, "CONNECTION_TEST_FAILED", "Connection test failed: "+err.Error(), details)
		return
	}

	// Create the connection record with owner = userID.
	conn := store.Connection{
		ID:        uuid.New().String(),
		Label:     req.Label,
		Driver:    req.Driver,
		Config:    req.Config,
		Color:     req.Color,
		Owner:     claims.UserID, // user-owned cluster
		CreatedAt: time.Now(),
	}

	created, err := s.conns.Create(r.Context(), conn)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to create connection")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

// testUserClusterRequest represents the payload for testing a user cluster connection.
type testUserClusterRequest struct {
	Driver string            `json:"driver"`
	Config map[string]string `json:"config"`
}

// testUserClusterHandler handles POST /api/v1/user/clusters/_test.
// Dry-run connection test without saving. Useful for "Test connection" button.
func (s *Server) testUserClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	// Get current user claims (for auth).
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	// Parse request body.
	var req testUserClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	if req.Driver == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Driver is required")
		return
	}
	if req.Config == nil || len(req.Config) == 0 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Config is required")
		return
	}

	// Validate driver is in supported drivers list.
	if !store.SupportedDrivers[req.Driver] {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_DRIVER", "Driver not supported")
		return
	}

	// Test connection synchronously via HealthCheck.
	drv, err := s.reg.For(r.Context(), req.Driver)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DRIVER_REGISTRY_ERROR", "Driver registry error: "+err.Error(), nil)
		return
	}

	health, err := drv.HealthCheck(r.Context())
	if err != nil {
		var details map[string]interface{}
		if hBytes, _ := json.Marshal(health); len(hBytes) > 0 {
			json.Unmarshal(hBytes, &details)
		}
		writeError(w, http.StatusBadRequest, "CONNECTION_TEST_FAILED", "Connection test failed: "+err.Error(), details)
		return
	}

	// Success - return health report.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"healthy": true,
		"details": health,
	})
}
