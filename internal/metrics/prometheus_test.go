package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
)

// TestPrometheusHandler_ExposesEveryMetricFamily verifies that a fresh
// collector advertises every basement_* metric family in the /metrics
// output (zero-value samples are emitted because the registry walks
// every registered metric, even those with no observations yet).
func TestPrometheusHandler_ExposesEveryMetricFamily(t *testing.T) {
	c := NewCollector()
	c.SetBuildInfo("v1.11.0f", "deadbeef")

	// Seed at least one sample per labelled metric so the registry
	// emits them — Prometheus omits CounterVec / GaugeVec / Histogram
	// rows that have never been touched (no zero-valued time series).
	c.RecordAuthAttempt("success")
	c.RecordAuditEvent("auth:login")
	c.RecordFederationReplicate("success")
	c.RecordBackupRun("b-1", "success", time.Unix(1700000000, 0))
	c.RecordWebhookDelivery("success")
	c.SetActiveSessions(3)
	c.SetServiceAccountsTotal(5)
	c.SetBucketsForCluster("garage", "c-1", 7)
	c.SetObjectsForBucket("garage", "c-1", "photos", 9000)
	c.SetFederationLag("f-1", "us-west-2:photos-replica", 42)

	// Drive one HTTP request through the middleware so the http_requests
	// + http_request_duration_seconds families surface.
	mw := c.PromMiddleware(nil)
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	c.Handler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics returned %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	want := []string{
		"basement_http_requests_total",
		"basement_http_request_duration_seconds",
		"basement_auth_attempts_total",
		"basement_audit_events_total",
		"basement_federation_replicate_total",
		"basement_federation_lag_seconds",
		"basement_backup_runs_total",
		"basement_backup_last_success_timestamp_seconds",
		"basement_webhook_deliveries_total",
		"basement_active_sessions",
		"basement_service_accounts_total",
		"basement_buckets_total",
		"basement_objects_total",
		"basement_build_info",
	}
	for _, name := range want {
		if !strings.Contains(body, name) {
			t.Errorf("metric %q missing from /metrics body", name)
		}
	}

	// Content-Type per Prometheus text format spec.
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") && !strings.HasPrefix(ct, "application/openmetrics") {
		t.Errorf("unexpected /metrics Content-Type %q", ct)
	}
}

// TestPrometheusHandler_TokenGate verifies the optional bearer-token
// gate refuses requests without the matching token and accepts those
// with it.
func TestPrometheusHandler_TokenGate(t *testing.T) {
	c := NewCollector()
	h := c.Handler("s3cr3t")

	// No token -> 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token request: status=%d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("WWW-Authenticate header missing Bearer challenge, got %q", got)
	}

	// Wrong token -> 401.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong-token request: status=%d, want 401", rec.Code)
	}

	// Right token -> 200.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("right-token request: status=%d, want 200", rec.Code)
	}
}

// TestPromMiddleware_RecordsCountAndDuration verifies the counter
// increments and the histogram observes a positive duration sample
// for one HTTP request.
func TestPromMiddleware_RecordsCountAndDuration(t *testing.T) {
	c := NewCollector()
	mw := c.PromMiddleware(func(r *http.Request) string {
		return "/api/v1/test/{id}"
	})
	wrapped := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Brief pause so the histogram observes a non-zero duration —
		// the lowest bucket is 5ms and we want to land somewhere
		// non-zero (any bucket > -Inf works for assertion below).
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	for i := 0; i < 3; i++ {
		wrapped.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodPost, "/api/v1/test/42", nil))
	}

	body := dumpMetrics(t, c)

	wantCounter := `basement_http_requests_total{method="POST",path="/api/v1/test/{id}",status="201"} 3`
	if !strings.Contains(body, wantCounter) {
		t.Errorf("expected counter line %q in /metrics body, got:\n%s", wantCounter, body)
	}

	// Histogram emits *_count, *_sum and one *_bucket per upper bound.
	wantCount := `basement_http_request_duration_seconds_count{method="POST",path="/api/v1/test/{id}"} 3`
	if !strings.Contains(body, wantCount) {
		t.Errorf("expected histogram count %q in /metrics body", wantCount)
	}
}

// TestRecordBackupRun_UpdatesLastSuccessGauge verifies success runs
// update the last-success gauge with the run's timestamp, while
// failure runs only increment the counter.
func TestRecordBackupRun_UpdatesLastSuccessGauge(t *testing.T) {
	c := NewCollector()

	ts := time.Unix(1715000000, 0).UTC()
	c.RecordBackupRun("b-photos", "success", ts)
	c.RecordBackupRun("b-photos", "failure", ts.Add(time.Hour))

	body := dumpMetrics(t, c)

	wantSuccess := `basement_backup_runs_total{result="success"} 1`
	wantFailure := `basement_backup_runs_total{result="failure"} 1`
	wantGauge := `basement_backup_last_success_timestamp_seconds{backup_id="b-photos"} 1.715e+09`

	for _, w := range []string{wantSuccess, wantFailure, wantGauge} {
		if !strings.Contains(body, w) {
			t.Errorf("expected %q in /metrics body, got:\n%s", w, body)
		}
	}
}

// TestAuditCollector_FansOutAuthLoginToAuthAttempts verifies the
// audit.Logger wrapper routes auth:login events to the auth-attempts
// counter (success + failure both flow through).
func TestAuditCollector_FansOutAuthLoginToAuthAttempts(t *testing.T) {
	c := NewCollector()
	wrapped := NewAuditCollector(audit.NewNoop(), c)

	wrapped.Log(audit.Event{Action: "auth:login", Result: "success"})
	wrapped.Log(audit.Event{Action: "auth:login", Result: "failure"})
	wrapped.Log(audit.Event{Action: "auth:login", Result: "failure"})

	body := dumpMetrics(t, c)

	for _, want := range []string{
		`basement_auth_attempts_total{result="success"} 1`,
		`basement_auth_attempts_total{result="failure"} 2`,
		`basement_audit_events_total{action="auth:login"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in /metrics body, got:\n%s", want, body)
		}
	}
}

// TestAuditCollector_FansOutWebhookAndFederation verifies the wrapper
// also drives the webhook + federation specialty counters.
func TestAuditCollector_FansOutWebhookAndFederation(t *testing.T) {
	c := NewCollector()
	wrapped := NewAuditCollector(audit.NewNoop(), c)

	wrapped.Log(audit.Event{Action: "webhook:fired_success", Result: "success"})
	wrapped.Log(audit.Event{Action: "webhook:fired_failure", Result: "failure"})
	wrapped.Log(audit.Event{Action: "federation:replicate_object", Result: "success"})
	wrapped.Log(audit.Event{Action: "federation:replicate_delete", Result: "failure"})

	body := dumpMetrics(t, c)

	for _, want := range []string{
		`basement_webhook_deliveries_total{result="success"} 1`,
		`basement_webhook_deliveries_total{result="failure"} 1`,
		`basement_federation_replicate_total{result="success"} 1`,
		`basement_federation_replicate_total{result="failure"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in /metrics body, got:\n%s", want, body)
		}
	}
}

// TestAuditCollector_BackupRunCompleteUpdatesGauge verifies a
// backup:run_complete audit event with Result=success updates both
// the run counter and the last-success gauge.
func TestAuditCollector_BackupRunCompleteUpdatesGauge(t *testing.T) {
	c := NewCollector()
	wrapped := NewAuditCollector(audit.NewNoop(), c)

	wrapped.Log(audit.Event{
		Action:   "backup:run_complete",
		Resource: "backup:b-nightly",
		Result:   "success",
	})

	body := dumpMetrics(t, c)

	if !strings.Contains(body, `basement_backup_runs_total{result="success"} 1`) {
		t.Errorf("backup runs counter missing or wrong, got:\n%s", body)
	}
	if !strings.Contains(body, `basement_backup_last_success_timestamp_seconds{backup_id="b-nightly"}`) {
		t.Errorf("last-success gauge missing for b-nightly, got:\n%s", body)
	}
}

// dumpMetrics renders the collector's /metrics body for assertion.
func dumpMetrics(t *testing.T, c *Collector) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c.Handler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics returned %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
