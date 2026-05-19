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

func TestCapabilities(t *testing.T) {
	d := &driver{client: newClient(map[string]string{})}

	caps, err := d.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}

	if caps.Driver != "garage" {
		t.Errorf("caps.Driver = %q, want %q", caps.Driver, "garage")
	}

	if caps.Layout != driverpkg.LayoutApplyRevert {
		t.Errorf("caps.Layout = %q, want %q", caps.Layout, driverpkg.LayoutApplyRevert)
	}

	if !caps.Quotas {
		t.Error("caps.Quotas should be true")
	}

	if !caps.BucketAliases {
		t.Error("caps.BucketAliases should be true")
	}

	if caps.KeyModel != driverpkg.KeyModelGarage {
		t.Errorf("caps.KeyModel = %q, want %q", caps.KeyModel, driverpkg.KeyModelGarage)
	}

	if !caps.Presign {
		t.Error("caps.Presign should be true")
	}

	if !caps.Multipart {
		t.Error("caps.Multipart should be true")
	}

	if caps.Versioning {
		t.Error("caps.Versioning should be false")
	}
}

func TestHealthCheck_HappyPath(t *testing.T) {
	wantResp := getClusterHealthResponse{
		Status:         "healthy",
		KnownNodes:     5,
		ConnectedNodes: 4,
		StorageNodes:   3,
		StorageNodesUp: 3,
		Partitions:     256,
		PartitionsQuorum: 256,
		PartitionsAllOk: 256,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(wantResp)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	report, err := d.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	if report.Status != "healthy" {
		t.Errorf("report.Status = %q, want %q", report.Status, "healthy")
	}

	if report.Details["knownNodes"] != int64(5) {
		t.Errorf("report.Details[\"knownNodes\"] = %v, want 5", report.Details["knownNodes"])
	}
}

func TestHealthCheck_401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	report, err := d.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if driverErr.Err != driverpkg.ErrUnauthenticated {
		t.Errorf("err = %v, want ErrUnauthenticated", driverErr.Err)
	}

	if report.Status != "" {
		t.Errorf("report.Status = %q, want empty string on error", report.Status)
	}
}

func TestListNodes_HappyPath(t *testing.T) {
	wantResp := getClusterStatusResponse{
		NodeID:  "node-123",
		Version: "v2.3.0",
		LiveNodes: []string{"node-123", "node-456"},
		CurrentLayout: clusterLayoutVersion{
			Version: 42,
			Status:  "current",
		},
		Nodes: []nodeResp{
			{
				ID:            "node-123",
				Addr:          "192.168.1.1:3901",
				Hostname:      "node1.example.com",
				GarageVersion: "v2.3.0",
				IsUp:          true,
				Draining:      false,
				Role: &nodeRole{
					NodeAssignedRole: &nodeAssignedRole{
						ID:     "node-123",
						Zone:   "zone-a",
						Tags:   []string{"ssd", "high-memory"},
						Capacity: func() *int64 { i := int64(100000000000); return &i }(),
					},
				},
			},
			{
				ID:            "node-456",
				Addr:          "192.168.1.2:3901",
				Hostname:      "node2.example.com",
				GarageVersion: "v2.3.0",
				IsUp:          false,
				Draining:      false,
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(wantResp)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	nodes, err := d.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("len(nodes) = %d, want 2", len(nodes))
	}

	if nodes[0].ID != "node-123" {
		t.Errorf("nodes[0].ID = %q, want \"node-123\"", nodes[0].ID)
	}

	if nodes[0].Zone != "zone-a" {
		t.Errorf("nodes[0].Zone = %q, want \"zone-a\"", nodes[0].Zone)
	}

	if nodes[1].Status != "unreachable" {
		t.Errorf("nodes[1].Status = %q, want \"unreachable\"", nodes[1].Status)
	}
}

func TestListNodes_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	nodes, err := d.ListNodes(context.Background())
	if err == nil {
		t.Fatal("expected error for 403")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if driverErr.Err != driverpkg.ErrPermissionDenied {
		t.Errorf("err = %v, want ErrPermissionDenied", driverErr.Err)
	}

	if nodes != nil {
		t.Errorf("nodes = %+v, want nil on error", nodes)
	}
}

func TestGetLayout_HappyPath(t *testing.T) {
	wantResp := getClusterLayoutResponse{
		Version: 42,
		Roles: []layoutNodeRole{
			{
				ID:     "node-123",
				Zone:   "zone-a",
				Tags:   []string{"ssd"},
				Capacity: func() *int64 { i := int64(100000000000); return &i }(),
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(wantResp)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	layout, err := d.GetLayout(context.Background())
	if err != nil {
		t.Fatalf("GetLayout() error = %v", err)
	}

	if layout.Version != 42 {
		t.Errorf("layout.Version = %d, want 42", layout.Version)
	}

	if len(layout.Nodes) != 1 {
		t.Errorf("len(layout.Nodes) = %d, want 1", len(layout.Nodes))
	}

	if layout.Staged != nil {
		t.Errorf("layout.Staged = %+v, want nil", *layout.Staged)
	}
}

func TestGetLayout_500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	layout, err := d.GetLayout(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if layout.Version != 0 {
		t.Errorf("layout.Version = %d, want 0 on error", layout.Version)
	}
}
