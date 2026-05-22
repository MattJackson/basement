package garage_v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestScrubSupport_ReportsSupportedTrue(t *testing.T) {
	d := newTestDriver("")
	caps := d.ScrubSupport()
	if !caps.Supported {
		t.Errorf("Supported = false, want true")
	}
}

func TestScrubState_RunningRoundTrip(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/worker" || r.Method != http.MethodGet {
			t.Errorf("expected GET /v1/worker, got %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]scrubWorkerResponseV1{
			{
				Name:          "block_scrub",
				State:         "busy",
				Progress:      "120/500",
				Message:       "scanning",
				BlocksScanned: 120,
				BlocksCorrupt: 0,
			},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	st, err := d.ScrubState(context.Background())
	if err != nil {
		t.Fatalf("ScrubState: %v", err)
	}
	if !st.Running {
		t.Errorf("Running = false, want true")
	}
	if st.ProgressPercent != 24 {
		t.Errorf("ProgressPercent = %d, want 24", st.ProgressPercent)
	}
	if st.BlocksScanned != 120 {
		t.Errorf("BlocksScanned = %d, want 120", st.BlocksScanned)
	}
	if st.Message != "scanning" {
		t.Errorf("Message = %q, want %q", st.Message, "scanning")
	}
}

func TestScrubState_NeverRun_ReturnsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]scrubWorkerResponseV1{
			{Name: "lifecycle_worker", State: "idle"},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	st, err := d.ScrubState(context.Background())
	if err != nil {
		t.Fatalf("ScrubState: %v", err)
	}
	if st.Running {
		t.Errorf("Running = true, want false on never-run")
	}
	if st.ProgressPercent != 0 {
		t.Errorf("ProgressPercent = %d, want 0", st.ProgressPercent)
	}
}

func TestScrubState_404_ReturnsAvailabilityMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	st, err := d.ScrubState(context.Background())
	if err != nil {
		t.Fatalf("ScrubState should soft-fail on 404, got err=%v", err)
	}
	if st.Message == "" {
		t.Error("expected non-empty Message explaining unavailable")
	}
}

func TestScrubState_CompletedScrub_ReportsHundredPercent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]scrubWorkerResponseV1{
			{
				Name:          "block_scrub",
				State:         "idle",
				BlocksScanned: 5000,
				BlocksCorrupt: 2,
				LastSeen:      "2026-05-20T12:00:00Z",
			},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	st, err := d.ScrubState(context.Background())
	if err != nil {
		t.Fatalf("ScrubState: %v", err)
	}
	if st.Running {
		t.Errorf("Running = true on idle, want false")
	}
	if st.ProgressPercent != 100 {
		t.Errorf("ProgressPercent = %d, want 100 (completed)", st.ProgressPercent)
	}
	if st.LastCompleted.IsZero() {
		t.Error("LastCompleted should be parsed from LastSeen")
	}
	if st.BlocksCorrupt != 2 {
		t.Errorf("BlocksCorrupt = %d, want 2", st.BlocksCorrupt)
	}
}

func TestStartScrub_PostsToWorkerEndpoint(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/worker/scrub-blocks" || r.Method != http.MethodPost {
			t.Errorf("expected POST /v1/worker/scrub-blocks, got %s %s", r.Method, r.URL.Path)
		}
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if err := d.StartScrub(context.Background()); err != nil {
		t.Fatalf("StartScrub: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
}

func TestStartScrub_403_ReturnsPermissionDenied(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.StartScrub(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Ensure the standard sentinel mapping fires.
	if !isWrappedSentinel(err, driverpkg.ErrPermissionDenied) {
		t.Errorf("err = %v, want wrapping ErrPermissionDenied", err)
	}
}

func isWrappedSentinel(err, target error) bool {
	type isr interface{ Is(error) bool }
	for err != nil {
		if err == target {
			return true
		}
		if i, ok := err.(isr); ok && i.Is(target) {
			return true
		}
		type uw interface{ Unwrap() error }
		u, ok := err.(uw)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
