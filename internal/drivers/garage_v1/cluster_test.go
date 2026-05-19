package garage_v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func newTestDriver(url string) *driver {
	return &driver{client: &client{baseURL: url, token: "test-token", http: &http.Client{}}}
}

func int64Ptr(v int64) *int64 { return &v }

func TestCapabilities(t *testing.T) {
	d := newTestDriver("")
	caps, err := d.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.Driver != "garage-v1" {
		t.Errorf("Driver = %q, want garage-v1", caps.Driver)
	}
	if caps.Layout != driverpkg.LayoutApplyRevert {
		t.Errorf("Layout = %q, want stage-apply-revert", caps.Layout)
	}
	if !caps.Quotas || !caps.BucketAliases {
		t.Error("expected Quotas and BucketAliases true")
	}
	if caps.KeyModel != driverpkg.KeyModelGarage {
		t.Errorf("KeyModel = %q, want garage", caps.KeyModel)
	}
}

func TestHealthCheck(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" || r.Method != "GET" {
			t.Errorf("expected GET /v1/health, got %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(getHealthResponseV1{
			Status:           "healthy",
			KnownNodes:       4,
			ConnectedNodes:   4,
			StorageNodes:     3,
			StorageNodesOk:   3,
			Partitions:       256,
			PartitionsQuorum: 256,
			PartitionsAllOk:  256,
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	hr, err := d.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if hr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", hr.Status)
	}
	if hr.Details["storageNodesOk"].(int64) != 3 {
		t.Errorf("storageNodesOk = %v, want 3", hr.Details["storageNodesOk"])
	}
}

func TestHealthCheck_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.HealthCheck(context.Background())
	if !errors.Is(err, driverpkg.ErrPermissionDenied) {
		t.Errorf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestListNodes(t *testing.T) {
	// Garage v1.0.1 returns roles[] only via GET /v1/layout — NOT in the
	// layout sub-object on GET /v1/status. ListNodes hits both and merges.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("unexpected method %s for %s", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/v1/status":
			resp := getStatusResponseV1{
				Node:          "node-self",
				GarageVersion: "v1.0.1",
				KnownNodes: []nodeNetworkInfoV1{
					{ID: "n1", Addr: "10.0.0.1:3901", IsUp: true, Hostname: "orion"},
					{ID: "n2", Addr: "10.0.0.2:3901", IsUp: false, Hostname: "pegasus"},
					{ID: "n3", Addr: "10.0.0.3:3901", IsUp: true, Hostname: "gateway-node"},
				},
				Layout: clusterLayoutV1{Version: 7}, // no roles on /v1/status
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/v1/layout":
			resp := clusterLayoutV1{
				Version: 7,
				Roles: []nodeClusterInfoV1{
					{ID: "n1", Zone: "dc1", Capacity: int64Ptr(1000000), Tags: []string{"fast"}},
					{ID: "n3", Zone: "dc2", Capacity: nil, Tags: []string{"gateway"}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	nodes, err := d.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(nodes))
	}

	// New order: layout.roles first (n1, n3), then unseen knownNodes (n2).
	// This matches Garage's actual "layout = cluster membership" semantics.

	// n1: in layout, has capacity -> storage; knownNode says isUp -> connected
	if nodes[0].ID != "n1" || nodes[0].Zone != "dc1" || nodes[0].Capacity != 1000000 || nodes[0].Role != "storage" {
		t.Errorf("n1 = %+v", nodes[0])
	}
	if nodes[0].Status != "connected" {
		t.Errorf("n1 status = %q, want connected", nodes[0].Status)
	}

	// n3: in layout with nil capacity -> gateway; knownNode isUp -> connected
	if nodes[1].ID != "n3" || nodes[1].Role != "gateway" {
		t.Errorf("n3 = %+v (expected role=gateway)", nodes[1])
	}
	if nodes[1].Status != "connected" {
		t.Errorf("n3 status = %q, want connected", nodes[1].Status)
	}

	// n2: knownNode but NOT in layout — surfaced at the end as unassigned
	if nodes[2].ID != "n2" || nodes[2].Status != "unreachable" {
		t.Errorf("n2 = %+v", nodes[2])
	}
	if nodes[2].Role != "unassigned" {
		t.Errorf("n2 role = %q, want unassigned (knownNode not in layout)", nodes[2].Role)
	}
}

// TestListNodes_singleNodeCluster covers the case that bit us in
// production: Garage v1.0.1 returns knownNodes=[] on a settled
// single-node cluster, but the local node IS still in layout.roles.
// Iterating knownNodes alone (the previous behavior) produced an empty
// dashboard despite a healthy cluster.
func TestListNodes_singleNodeCluster(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/status":
			resp := getStatusResponseV1{
				Node:          "self-id",
				GarageVersion: "v1.0.1",
				KnownNodes:    []nodeNetworkInfoV1{}, // empty — single-node, no gossip peers
				Layout:        clusterLayoutV1{Version: 1},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/v1/layout":
			resp := clusterLayoutV1{
				Version: 1,
				Roles: []nodeClusterInfoV1{
					{ID: "self-id", Zone: "dc1", Capacity: int64Ptr(1000000000000), Tags: []string{}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	nodes, err := d.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (the local node from layout.roles)", len(nodes))
	}
	if nodes[0].ID != "self-id" {
		t.Errorf("node id = %q, want %q", nodes[0].ID, "self-id")
	}
	if nodes[0].Role != "storage" {
		t.Errorf("node role = %q, want storage", nodes[0].Role)
	}
	if nodes[0].Status != "connected" {
		t.Errorf("local node status = %q, want connected (admin API responded so it's up)", nodes[0].Status)
	}
}

func TestListNodes_401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.ListNodes(context.Background())
	if !errors.Is(err, driverpkg.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestGetLayout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/layout" || r.Method != "GET" {
			t.Errorf("expected GET /v1/layout, got %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(clusterLayoutV1{
			Version: 12,
			Roles: []nodeClusterInfoV1{
				{ID: "node-a", Zone: "z1", Capacity: int64Ptr(500), Tags: []string{"t1"}},
			},
			StagedRoleChanges: []nodeRoleChangeV1{
				{ID: "node-b", Zone: "z2", Capacity: int64Ptr(700), Tags: []string{"t2"}},
				{ID: "node-old", Remove: true},
			},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	layout, err := d.GetLayout(context.Background())
	if err != nil {
		t.Fatalf("GetLayout: %v", err)
	}
	if layout.Version != 12 {
		t.Errorf("Version = %d, want 12", layout.Version)
	}
	if len(layout.Nodes) != 1 || layout.Nodes[0].ID != "node-a" {
		t.Errorf("Nodes = %+v", layout.Nodes)
	}
	if layout.Staged == nil {
		t.Fatal("expected Staged to be non-nil")
	}
	if layout.Staged.Version != 13 {
		t.Errorf("Staged.Version = %d, want 13", layout.Staged.Version)
	}
	if len(layout.Staged.Nodes) != 2 {
		t.Fatalf("Staged.Nodes len = %d, want 2", len(layout.Staged.Nodes))
	}
	if layout.Staged.Nodes[1].Status != "removed" {
		t.Errorf("removal node status = %q, want removed", layout.Staged.Nodes[1].Status)
	}
}

func TestGetLayout_NoStaged(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(clusterLayoutV1{
			Version: 1,
			Roles:   []nodeClusterInfoV1{},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	layout, err := d.GetLayout(context.Background())
	if err != nil {
		t.Fatalf("GetLayout: %v", err)
	}
	if layout.Staged != nil {
		t.Errorf("expected Staged nil with no staged changes, got %+v", layout.Staged)
	}
}

func TestStageLayout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/layout" || r.Method != "POST" {
			t.Errorf("expected POST /v1/layout, got %s %s", r.Method, r.URL.Path)
		}
		var body []nodeRoleChangeV1
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body) != 1 {
			t.Fatalf("expected single-element array, got len=%d", len(body))
		}
		if body[0].ID != "node-x" {
			t.Errorf("id = %q, want node-x", body[0].ID)
		}
		if body[0].Zone != "geneva" {
			t.Errorf("zone = %q, want geneva", body[0].Zone)
		}
		if body[0].Capacity == nil || *body[0].Capacity != 100000000000 {
			t.Errorf("capacity = %v, want 100000000000", body[0].Capacity)
		}
		_ = json.NewEncoder(w).Encode(clusterLayoutV1{Version: 5})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)

	zone := "geneva"
	capacity := int64(100000000000)
	change := driverpkg.LayoutChange{
		NodeID:   "node-x",
		Zone:     &zone,
		Capacity: &capacity,
		Tags:     []string{"fast"},
	}
	if _, err := d.StageLayout(context.Background(), change); err != nil {
		t.Fatalf("StageLayout: %v", err)
	}
}

func TestStageLayout_400(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	zone := "x"
	_, err := d.StageLayout(context.Background(), driverpkg.LayoutChange{NodeID: "n", Zone: &zone})
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestStageLayout_Remove(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []nodeRoleChangeV1
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 1 || !body[0].Remove {
			t.Errorf("expected single removal entry, got %+v", body)
		}
		_ = json.NewEncoder(w).Encode(clusterLayoutV1{Version: 5})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if _, err := d.StageLayout(context.Background(), driverpkg.LayoutChange{NodeID: "node-gone"}); err != nil {
		t.Fatalf("StageLayout: %v", err)
	}
}

func TestApplyLayout(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/layout" && r.Method == "GET":
			calls++
			_ = json.NewEncoder(w).Encode(clusterLayoutV1{Version: 10})
		case r.URL.Path == "/v1/layout/apply" && r.Method == "POST":
			var body layoutVersionV1
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Version != 11 {
				t.Errorf("version = %d, want 11 (current+1)", body.Version)
			}
			calls++
			_ = json.NewEncoder(w).Encode(applyLayoutResponseV1{Message: []string{"applied"}, Layout: clusterLayoutV1{Version: 11}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if err := d.ApplyLayout(context.Background()); err != nil {
		t.Fatalf("ApplyLayout: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (GET + POST), got %d", calls)
	}
}

func TestApplyLayout_BadVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/layout" && r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(clusterLayoutV1{Version: 10})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.ApplyLayout(context.Background())
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestRevertLayout(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/layout" && r.Method == "GET":
			calls++
			_ = json.NewEncoder(w).Encode(clusterLayoutV1{Version: 8})
		case r.URL.Path == "/v1/layout/revert" && r.Method == "POST":
			var body layoutVersionV1
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Version != 9 {
				t.Errorf("version = %d, want 9", body.Version)
			}
			calls++
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if err := d.RevertLayout(context.Background()); err != nil {
		t.Fatalf("RevertLayout: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestRevertLayout_500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/layout" && r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(clusterLayoutV1{Version: 1})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if err := d.RevertLayout(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
}
