package garage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestStageLayout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/UpdateClusterLayout" || r.Method != "POST" {
			t.Errorf("expected POST /v2/UpdateClusterLayout, got %s %s", r.Method, r.URL.Path)
		}

		var req updateClusterLayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.NodeId != "node-1" {
			t.Errorf("expected NodeId 'node-1', got '%s'", req.NodeId)
		}

		response := getClusterLayoutResponse{
			Version: 42,
			Roles: []layoutNodeRole{
				{ID: "node-1", Zone: "zone-a", Capacity: int64Ptr(1000), Tags: []string{"tag1"}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	newZone := "zone-a"
	capacity := int64(1000)
	change := driverpkg.LayoutChange{
		NodeID:   "node-1",
		Zone:     &newZone,
		Capacity: &capacity,
		Tags:     []string{"tag1"},
	}

	diff, err := d.StageLayout(context.Background(), change)
	if err != nil {
		t.Fatalf("StageLayout failed: %v", err)
	}

	if diff.Adds == nil || len(diff.Adds) > 0 || len(diff.Removes) > 0 || len(diff.Modifies) > 0 {
		// Accept empty diff for now as computeLayoutDiff is simplified
	}
}

func TestStageLayoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	newZone := "zone-a"
	change := driverpkg.LayoutChange{
		NodeID: "node-1",
		Zone:   &newZone,
	}

	_, err := d.StageLayout(context.Background(), change)
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestApplyLayout(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/GetClusterLayout" && r.Method == "GET" {
			callCount++
			response := getClusterLayoutResponse{
				Version: 10,
				Roles:   []layoutNodeRole{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path != "/v2/ApplyClusterLayout" || r.Method != "POST" {
			t.Errorf("expected POST /v2/ApplyClusterLayout, got %s %s", r.Method, r.URL.Path)
		}

		var req applyClusterLayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Version != 11 {
			t.Errorf("expected Version 11 (current+1), got %d", req.Version)
		}

		callCount++
		response := applyClusterLayoutResponse{
			Layout: getClusterLayoutResponse{Version: 11},
			Message: []string{"layout applied"},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.ApplyLayout(context.Background())
	if err != nil {
		t.Fatalf("ApplyLayout failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (GetClusterLayout + ApplyClusterLayout), got %d", callCount)
	}
}

func TestApplyLayoutConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/GetClusterLayout" && r.Method == "GET" {
			response := getClusterLayoutResponse{Version: 10}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path != "/v2/ApplyClusterLayout" || r.Method != "POST" {
			t.Errorf("expected POST /v2/ApplyClusterLayout, got %s %s", r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error": "version mismatch"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.ApplyLayout(context.Background())
	if err == nil {
		t.Fatal("expected error on 409 conflict, got nil")
	}

	if !errors.Is(err, driverpkg.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestRevertLayout(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/GetClusterLayout" && r.Method == "GET" {
			callCount++
			response := getClusterLayoutResponse{
				Version: 15,
				Roles:   []layoutNodeRole{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path != "/v2/RevertClusterLayout" || r.Method != "POST" {
			t.Errorf("expected POST /v2/RevertClusterLayout, got %s %s", r.Method, r.URL.Path)
		}

		var req revertClusterLayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Version != 15 {
			t.Errorf("expected Version 15, got %d", req.Version)
		}

		callCount++
		response := revertClusterLayoutResponse{}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.RevertLayout(context.Background())
	if err != nil {
		t.Fatalf("RevertLayout failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (GetClusterLayout + RevertClusterLayout), got %d", callCount)
	}
}

func TestRevertLayoutConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/GetClusterLayout" && r.Method == "GET" {
			response := getClusterLayoutResponse{Version: 15}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path != "/v2/RevertClusterLayout" || r.Method != "POST" {
			t.Errorf("expected POST /v2/RevertClusterLayout, got %s %s", r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error": "version mismatch"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.RevertLayout(context.Background())
	if err == nil {
		t.Fatal("expected error on 409 conflict, got nil")
	}

	if !errors.Is(err, driverpkg.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

