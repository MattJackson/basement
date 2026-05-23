// Audit -> Prometheus bridge (v1.11.0f).
//
// AuditCollector wraps an audit.Logger and, in addition to forwarding
// every event to the wrapped logger, increments the appropriate
// Prometheus counter. This lets the existing audit pipeline drive the
// auth/audit/federation/backup/webhook counters without instrumenting
// every call site individually.
//
// The wrap is intentionally minimal: we honour the original Logger's
// failure modes (the file logger silently swallows I/O errors, the
// noop discards) and never block on metric updates — Prometheus
// counters are lock-free atomics, so the overhead per event is
// negligible.
package metrics

import (
	"strings"
	"time"

	"github.com/mattjackson/basement/internal/audit"
)

// AuditCollector is an audit.Logger decorator that also increments
// Prometheus counters keyed off the event's Action/Result.
//
// Action mapping (best-effort, additive — unrecognised actions still
// bump basement_audit_events_total but don't fan out to specialty
// counters):
//
//   - auth:login      -> basement_auth_attempts_total{result}
//   - federation:*    -> basement_federation_replicate_total{result}
//   - backup:run_*    -> basement_backup_runs_total{result}
//                       + basement_backup_last_success_timestamp_seconds
//   - webhook:fired_* -> basement_webhook_deliveries_total{result}
type AuditCollector struct {
	wrapped audit.Logger
	c       *Collector
}

// NewAuditCollector wraps an audit.Logger so every Log call also
// updates the relevant Prometheus counter on c. Tests that don't
// have a collector should call audit.NewFileLogger directly.
func NewAuditCollector(wrapped audit.Logger, c *Collector) *AuditCollector {
	return &AuditCollector{wrapped: wrapped, c: c}
}

// Log forwards to the wrapped logger and updates the relevant
// Prometheus counter(s) for this event.
func (a *AuditCollector) Log(e audit.Event) {
	a.wrapped.Log(e)
	if a.c == nil {
		return
	}
	a.c.RecordAuditEvent(e.Action)

	switch {
	case e.Action == "auth:login":
		// Result is "success" or "failure" matching the Prometheus
		// label convention 1:1.
		a.c.RecordAuthAttempt(e.Result)

	case strings.HasPrefix(e.Action, "federation:replicate"):
		a.c.RecordFederationReplicate(e.Result)

	case e.Action == "backup:run_complete":
		// resourceBackup() formats as "backup:<id>". Strip the
		// prefix so the Prom label matches the operator-facing ID.
		backupID := strings.TrimPrefix(e.Resource, "backup:")
		a.c.RecordBackupRun(backupID, e.Result, time.Now().UTC())

	case e.Action == "backup:run_failed" || e.Action == "backup:run_start":
		backupID := strings.TrimPrefix(e.Resource, "backup:")
		if e.Action == "backup:run_failed" {
			a.c.RecordBackupRun(backupID, "failure", time.Now().UTC())
		}

	case strings.HasPrefix(e.Action, "webhook:fired_"):
		a.c.RecordWebhookDelivery(e.Result)
	}
}

// Query is a straight pass-through to the wrapped logger.
func (a *AuditCollector) Query(from, to time.Time, filter audit.QueryFilter) ([]audit.Event, error) {
	return a.wrapped.Query(from, to, filter)
}

// QueryWithTotal is a straight pass-through to the wrapped logger.
func (a *AuditCollector) QueryWithTotal(from, to time.Time, filter audit.QueryFilter) ([]audit.Event, int, error) {
	return a.wrapped.QueryWithTotal(from, to, filter)
}
