package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// AggregatedUserKey represents a key from a specific connection with error info.
type AggregatedUserKey struct {
	driver.Key
	ConnectionID string `json:"connectionId"`
}

// AggregatedUserKeyError represents an error from a specific connection during fan-out.
type AggregatedUserKeyError struct {
	ConnectionID string `json:"connectionId"`
	Message      string `json:"message"`
}

// AggregatedUserKeysResponse is the response for cross-cluster key listing filtered by user grants.
type AggregatedUserKeysResponse struct {
	Keys   []AggregatedUserKey    `json:"keys"`
	Errors []AggregatedUserKeyError `json:"errors,omitempty"`
}

// userListKeysHandler handles GET /api/v1/user/keys.
func (s *Server) userListKeysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	// Get visible connections for this user.
	visibleConnIDs := s.userVisibleConnections(r.Context())
	if visibleConnIDs == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	// If no visible connections, return empty response.
	if len(visibleConnIDs) == 0 {
		writeJSON(w, http.StatusOK, AggregatedUserKeysResponse{
			Keys:   []AggregatedUserKey{},
			Errors: []AggregatedUserKeyError{},
		})
		return
	}

	// Get all connections and filter to visible ones.
	allConns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	var visibleConns []interface{}
	for _, conn := range allConns {
		for _, id := range visibleConnIDs {
			if conn.ID == id || s.userOwnsConnection(r.Context(), conn.ID) {
				visibleConns = append(visibleConns, conn)
				break
			}
		}
	}

	type result struct {
		key  driver.Key
		connID string
		err  error
	}

	results := make([]result, 0)
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	concurrency := 5

	sem := make(chan struct{}, concurrency)

	for _, connIntf := range visibleConns {
		conn := connIntf.(store.Connection)
		wg.Add(1)
		go func(conn store.Connection) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Per-cluster 3s deadline so an unreachable cluster doesn't block.
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()

			drv, err := s.reg.For(ctx, conn.ID)
			if err != nil {
				mu.Lock()
				results = append(results, result{connID: conn.ID, err: &driver.Error{Op: "ListKeys", Driver: conn.Driver, Err: err, Message: "building driver"}})
				mu.Unlock()
				return
			}

			keys, err := drv.ListKeys(ctx)
			if err != nil {
				mu.Lock()
				results = append(results, result{connID: conn.ID, err: &driver.Error{Op: "ListKeys", Driver: conn.Driver, Err: err, Message: "listing keys"}})
				mu.Unlock()
				return
			}

			for _, k := range keys {
				mu.Lock()
				results = append(results, result{key: k, connID: conn.ID})
				mu.Unlock()
			}
		}(conn)
	}

	wg.Wait()

	resp := AggregatedUserKeysResponse{
		Keys:   make([]AggregatedUserKey, 0),
		Errors: make([]AggregatedUserKeyError, 0),
	}

	for _, r := range results {
		if r.err != nil {
			resp.Errors = append(resp.Errors, AggregatedUserKeyError{
				ConnectionID: r.connID,
				Message:      r.err.Error(),
			})
		} else {
			resp.Keys = append(resp.Keys, AggregatedUserKey{
				Key:        r.key,
				ConnectionID: r.connID,
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
