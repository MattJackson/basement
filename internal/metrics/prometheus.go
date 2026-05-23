// Prometheus exporter for the basement-server (v1.11.0f).
//
// Mounted at /metrics on the API router. Exposes a fixed set of
// counters, histograms and gauges in Prometheus text format. The
// fixed-set design (rather than a free-form registry) keeps the
// exposed surface small and stable — operators wire alerts against
// these names with confidence, and the dashboard JSON in
// docs/observability ships against the same names.
//
// Authentication: by convention /metrics is unauthenticated and
// operators front it with a network allowlist. Setting
// BASEMENT_METRICS_TOKEN enables a Bearer-token gate for deployments
// that can't enforce the allowlist at the network layer (e.g. shared
// ingress in a multi-tenant cluster).
//
// Instrumentation strategy: HTTP request metrics are populated by a
// thin middleware (PromMiddleware). Auth attempts / audit events /
// federation replication / backup runs / webhook deliveries are
// driven by a wrapping audit logger (auditCollector) that observes
// every audit.Event flowing through the existing audit pipeline.
// Gauges (active sessions, federations, buckets) are populated
// on-demand by callers via SetGauge*.
//
// Tests live in prometheus_test.go.
package metrics

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector bundles every basement Prometheus metric behind a single
// dependency. main.go constructs one and threads it through the audit
// wrapper, the API middleware and the federation/webhook engines.
type Collector struct {
	reg *prometheus.Registry

	httpRequests        *prometheus.CounterVec
	httpDuration        *prometheus.HistogramVec
	authAttempts        *prometheus.CounterVec
	auditEvents         *prometheus.CounterVec
	federationReplicate *prometheus.CounterVec
	federationLag       *prometheus.GaugeVec
	backupRuns          *prometheus.CounterVec
	backupLastSuccess   *prometheus.GaugeVec
	webhookDeliveries   *prometheus.CounterVec
	activeSessions      prometheus.Gauge
	serviceAccounts     prometheus.Gauge
	bucketsTotal        *prometheus.GaugeVec
	objectsTotal        *prometheus.GaugeVec
	buildInfo           *prometheus.GaugeVec
}

// NewCollector builds a Collector with every metric pre-registered on
// a private registry — promhttp.HandlerFor uses that registry so the
// exporter exposes ONLY basement metrics (no Go runtime / process
// collectors). Operators who want Go runtime metrics can scrape the
// pprof endpoint or wire their own exporter.
func NewCollector() *Collector {
	reg := prometheus.NewRegistry()
	c := &Collector{
		reg: reg,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "basement_http_requests_total",
			Help: "HTTP requests handled, partitioned by method, route template and response status.",
		}, []string{"method", "path", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "basement_http_request_duration_seconds",
			Help: "HTTP request latency in seconds, partitioned by method and route template.",
			// Buckets tuned for an admin UI: most requests are sub-50ms
			// JSON CRUD; the long tail is bucket-list fan-out and
			// federation status pages that can hit a few seconds.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "path"}),
		authAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "basement_auth_attempts_total",
			Help: "Authentication attempts, partitioned by result (success|failure).",
		}, []string{"result"}),
		auditEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "basement_audit_events_total",
			Help: "Audit events emitted, partitioned by action.",
		}, []string{"action"}),
		federationReplicate: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "basement_federation_replicate_total",
			Help: "Federation replication outcomes, partitioned by result.",
		}, []string{"result"}),
		federationLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "basement_federation_lag_seconds",
			Help: "Replica lag in seconds, partitioned by federation_id and replica.",
		}, []string{"federation_id", "replica"}),
		backupRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "basement_backup_runs_total",
			Help: "Backup runs, partitioned by result.",
		}, []string{"result"}),
		backupLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "basement_backup_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful backup, partitioned by backup_id.",
		}, []string{"backup_id"}),
		webhookDeliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "basement_webhook_deliveries_total",
			Help: "Webhook delivery outcomes, partitioned by result.",
		}, []string{"result"}),
		activeSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "basement_active_sessions",
			Help: "Active authenticated sessions (approximation: tokens issued in the last SessionTTL).",
		}),
		serviceAccounts: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "basement_service_accounts_total",
			Help: "Total service accounts across every owner.",
		}),
		bucketsTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "basement_buckets_total",
			Help: "Buckets present, partitioned by driver and cluster (connection ID).",
		}, []string{"driver", "cluster"}),
		objectsTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "basement_objects_total",
			Help: "Objects present, partitioned by driver, cluster and bucket. May be sampled when expensive.",
		}, []string{"driver", "cluster", "bucket"}),
		buildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "basement_build_info",
			Help: "Build info — value is always 1; labels carry version and commit.",
		}, []string{"version", "commit"}),
	}

	reg.MustRegister(
		c.httpRequests,
		c.httpDuration,
		c.authAttempts,
		c.auditEvents,
		c.federationReplicate,
		c.federationLag,
		c.backupRuns,
		c.backupLastSuccess,
		c.webhookDeliveries,
		c.activeSessions,
		c.serviceAccounts,
		c.bucketsTotal,
		c.objectsTotal,
		c.buildInfo,
	)
	return c
}

// Registry exposes the underlying registry. Tests use it; production
// only needs Handler().
func (c *Collector) Registry() *prometheus.Registry { return c.reg }

// Handler returns the /metrics HTTP handler. When token is non-empty,
// requests must carry a matching Authorization: Bearer <token> header
// or receive 401. Constant-time compare to avoid timing leaks on
// short tokens.
func (c *Collector) Handler(token string) http.Handler {
	base := promhttp.HandlerFor(c.reg, promhttp.HandlerOpts{
		// Disable the Go-process error log — basement's slog handler
		// already captures unexpected scrape errors via the panic
		// recover middleware.
		ErrorHandling: promhttp.ContinueOnError,
	})
	if token == "" {
		return base
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="basement-metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		base.ServeHTTP(w, r)
	})
}

// SetBuildInfo records the running version+commit. Called once at
// boot from main.go. Idempotent.
func (c *Collector) SetBuildInfo(version, commit string) {
	c.buildInfo.WithLabelValues(version, commit).Set(1)
}

// SetActiveSessions sets the active-session gauge. Caller is
// responsible for whatever sampling/derivation is appropriate.
func (c *Collector) SetActiveSessions(n int) { c.activeSessions.Set(float64(n)) }

// SetServiceAccountsTotal sets the service-accounts gauge.
func (c *Collector) SetServiceAccountsTotal(n int) { c.serviceAccounts.Set(float64(n)) }

// SetBucketsForCluster sets the buckets gauge for one (driver, cluster) pair.
func (c *Collector) SetBucketsForCluster(driver, cluster string, n int) {
	c.bucketsTotal.WithLabelValues(driver, cluster).Set(float64(n))
}

// SetObjectsForBucket sets the objects gauge for one (driver, cluster, bucket).
func (c *Collector) SetObjectsForBucket(driver, cluster, bucket string, n int64) {
	c.objectsTotal.WithLabelValues(driver, cluster, bucket).Set(float64(n))
}

// SetFederationLag sets the replica-lag gauge in seconds.
func (c *Collector) SetFederationLag(federationID, replica string, lagSec float64) {
	c.federationLag.WithLabelValues(federationID, replica).Set(lagSec)
}

// RecordBackupRun bumps the backup-run counter; on success, also
// updates the last-success gauge.
func (c *Collector) RecordBackupRun(backupID, result string, at time.Time) {
	c.backupRuns.WithLabelValues(result).Inc()
	if result == "success" {
		c.backupLastSuccess.WithLabelValues(backupID).Set(float64(at.Unix()))
	}
}

// RecordWebhookDelivery bumps the webhook delivery counter.
func (c *Collector) RecordWebhookDelivery(result string) {
	c.webhookDeliveries.WithLabelValues(result).Inc()
}

// RecordAuthAttempt bumps the auth-attempt counter.
func (c *Collector) RecordAuthAttempt(result string) {
	c.authAttempts.WithLabelValues(result).Inc()
}

// RecordAuditEvent bumps the audit-event counter. Bypasses the
// audit-collector wrapper for tests/callers that need direct access.
func (c *Collector) RecordAuditEvent(action string) {
	c.auditEvents.WithLabelValues(action).Inc()
}

// RecordFederationReplicate bumps the federation replicate counter.
func (c *Collector) RecordFederationReplicate(result string) {
	c.federationReplicate.WithLabelValues(result).Inc()
}

// PromMiddleware returns an http.Handler middleware that records HTTP
// request count + latency. Route templates come from chi's
// RouteContext when available; otherwise the raw URL path is used.
// Tests that don't care about per-route cardinality pass a nil
// routeNamer and get the raw path.
//
// The chi-route-template lookup is deferred to a callback (routeFor)
// so this package stays free of any chi dependency.
func (c *Collector) PromMiddleware(routeFor func(*http.Request) string) func(http.Handler) http.Handler {
	if routeFor == nil {
		routeFor = func(r *http.Request) string { return r.URL.Path }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			elapsed := time.Since(start).Seconds()
			path := routeFor(r)
			method := r.Method
			c.httpRequests.WithLabelValues(method, path, strconv.Itoa(rw.status)).Inc()
			c.httpDuration.WithLabelValues(method, path).Observe(elapsed)
		})
	}
}

// statusRecorder is a tiny http.ResponseWriter wrapper that captures
// the final status code so the middleware can label the counter.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}
