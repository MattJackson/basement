package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/audit"
)

// Engine is the in-process webhook delivery dispatcher. Construct via
// NewEngine, start with Start(ctx), tear down with Stop().
//
// Design:
//
//   - One central goroutine drains the Emit() channel and fans each
//     EventEnvelope out to matching webhooks. We considered per-webhook
//     worker goroutines (federation engine pattern), but webhooks are
//     event-pull rather than time-tick — keeping a single dispatcher
//     means matching only happens once per emit, not per (webhook ×
//     tick). The dispatcher does spawn a delivery goroutine per
//     matching (envelope, webhook) pair so a slow target can't stall
//     other deliveries.
//
//   - Retry policy: 3 attempts with exponential backoff (1s, 5s, 15s).
//     On final failure we increment Webhook.FailureCount; after
//     AutoDisableThreshold consecutive failures the store flips
//     Enabled=false and the engine emits webhook:auto_disabled.
//
//   - Panic-safe: every delivery goroutine runs inside a recover, so
//     a panicking transport can't kill the dispatcher.
//
//   - Audit: webhook:fired_success / webhook:fired_failure per
//     attempt-result; webhook:auto_disabled when the threshold flips.
type Engine struct {
	store  Store
	audit  audit.Logger
	logger *slog.Logger

	// httpClient is the transport for outbound POSTs. Tests replace
	// it via SetHTTPClient with a recording fake; production wires
	// a Client with a short Timeout so a black-hole target can't
	// pin a delivery goroutine forever.
	httpClient *http.Client

	// events is the buffered emit queue. Cap is fixed at construction
	// (eventQueueSize) — a saturated queue drops the oldest event
	// rather than blocking the emitter (object handlers must never
	// stall on webhook backpressure).
	events chan EventEnvelope

	// backoffSchedule is the per-attempt delay slice. Element 0 is
	// the delay BEFORE attempt 1 retry (i.e. between attempt 1 and
	// attempt 2). Length controls total attempts (attempts = len+1).
	// Tests shorten this to keep test runtime sub-second.
	backoffSchedule []time.Duration

	// nowFn returns the engine's notion of "now". Tests stub it for
	// deterministic OccurredAt assertions; production uses time.Now.UTC.
	nowFn func() time.Time

	// idFn assigns the per-event UUID inside Emit. Tests substitute a
	// counter so assertions can name the event.
	idFn func() string

	// dispatch concurrency control. The dispatcher goroutine + every
	// delivery goroutine increments inflight; Stop blocks on it.
	inflight sync.WaitGroup

	// started + stopped gate Start / Stop so re-entry is a no-op.
	started atomic.Bool
	stopped atomic.Bool

	// quit closes when Stop is called. The dispatcher selects on
	// it alongside the events channel.
	quit chan struct{}

	// emitCount + deliveryCount are bumped by Emit and the per-
	// delivery goroutine. Exposed for tests via EmitCount /
	// DeliveryCount so a test can wait for "the engine processed N
	// envelopes" without sleeping.
	emitCount     atomic.Int64
	deliveryCount atomic.Int64

	// subMu guards subs. Held only on Subscribe / unsubscribe / the
	// dispatcher's per-envelope subscriber fan-out; the fan-out runs a
	// snapshot under the lock then releases before invoking callbacks
	// so a misbehaving subscriber can't deadlock the dispatcher.
	subMu sync.Mutex
	// subs is the registry of internal subscribers keyed by an
	// engine-assigned token so unsubscribe is O(1). Callback identity
	// alone wouldn't work — Go closures aren't comparable.
	subs map[uint64]subscriberEntry
	// subSeq is the monotonic counter used to mint subscriber tokens.
	subSeq uint64
}

// subscriberEntry is one registered internal subscriber. Name is a
// human-readable tag carried in dispatcher panic logs so the operator
// can pin a runaway subscriber to its owning subsystem.
type subscriberEntry struct {
	name string
	cb   func(EventEnvelope)
}

// eventQueueSize is the buffered capacity of the Engine.events channel.
// Picked from the cycle spec (1000). Saturation drops the oldest event
// to make room — see Emit.
const eventQueueSize = 1000

// DefaultBackoff is the production retry schedule: 1s before retry 1,
// 5s before retry 2, 15s before the third (final) attempt. Tests pass
// a shorter slice via SetBackoffSchedule.
var DefaultBackoff = []time.Duration{
	1 * time.Second,
	5 * time.Second,
	15 * time.Second,
}

// NewEngine constructs an unstarted Engine. Passing nil for audit
// installs a noop logger so callers that don't wire audit still get a
// working engine; production main.go always passes the real FileLogger.
func NewEngine(store Store, audit audit.Logger, logger *slog.Logger) *Engine {
	if audit == nil {
		audit = noopAudit{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		store:           store,
		audit:           audit,
		logger:          logger,
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		events:          make(chan EventEnvelope, eventQueueSize),
		backoffSchedule: DefaultBackoff,
		nowFn:           func() time.Time { return time.Now().UTC() },
		idFn:            func() string { return uuid.New().String() },
		quit:            make(chan struct{}),
		subs:            make(map[uint64]subscriberEntry),
	}
}

// SetHTTPClient overrides the transport used to POST event payloads.
// Tests substitute a client backed by httptest.Server so they can
// assert request shape + control response codes without going on the
// network. Must be called before Start.
func (e *Engine) SetHTTPClient(c *http.Client) {
	if c != nil {
		e.httpClient = c
	}
}

// SetBackoffSchedule overrides the retry delay slice. Tests pass a
// short ([]time.Duration{1*time.Millisecond, 1*time.Millisecond, ...})
// to keep test runs fast. Must be called before Start. An empty slice
// is interpreted as "one attempt, no retry".
func (e *Engine) SetBackoffSchedule(s []time.Duration) {
	if s != nil {
		// Defensive copy so the caller can't mutate engine state by
		// reaching back through the slice header.
		e.backoffSchedule = append([]time.Duration(nil), s...)
	}
}

// SetNowFunc overrides the engine's notion of "now" for Emit
// envelopes. Used by tests that need deterministic OccurredAt
// timestamps in assertions.
func (e *Engine) SetNowFunc(f func() time.Time) {
	if f != nil {
		e.nowFn = f
	}
}

// SetIDFunc overrides the engine's ID generator for Emit envelopes.
// Tests pass a counter so assertions can name events by an ordinal.
func (e *Engine) SetIDFunc(f func() string) {
	if f != nil {
		e.idFn = f
	}
}

// Start spawns the dispatcher goroutine. Idempotent — calling Start
// twice is a no-op.
//
// If store is nil (engine was never wired) the dispatcher logs and
// returns immediately, leaving Emit a silent drop. We deliberately
// don't error here because the API server's handlers nil-check the
// engine and silently skip Emit calls when it's absent.
func (e *Engine) Start(ctx context.Context) {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	if e.store == nil {
		e.logger.Warn("webhook engine: no store wired — engine inert")
		return
	}
	e.inflight.Add(1)
	go e.runDispatcher(ctx)
}

// Stop signals the dispatcher to exit and waits for in-flight
// deliveries to complete. Safe to call multiple times.
func (e *Engine) Stop() {
	if !e.stopped.CompareAndSwap(false, true) {
		return
	}
	close(e.quit)
	e.inflight.Wait()
}

// Emit pushes an envelope onto the dispatcher queue. Non-blocking:
// when the queue is saturated we drop the oldest event to make room
// (chosen over blocking because every Emit caller is on a request
// hot path — object handlers must not stall on webhook backpressure).
//
// Assigns ID + OccurredAt if the caller left them zero, so emission
// sites can fire-and-forget the envelope shape.
func (e *Engine) Emit(env EventEnvelope) {
	if env.ID == "" {
		env.ID = e.idFn()
	}
	if env.OccurredAt.IsZero() {
		env.OccurredAt = e.nowFn()
	}
	e.emitCount.Add(1)
	// Non-blocking send. On saturation, pop one and try again — the
	// oldest event is the least likely to still be useful. We drop at
	// most one event per Emit to keep this O(1).
	select {
	case e.events <- env:
	default:
		select {
		case <-e.events:
			e.logger.Warn("webhook engine: queue saturated — dropped oldest envelope to make room")
		default:
		}
		select {
		case e.events <- env:
		default:
			// Even after the drop the channel is somehow still full
			// (concurrent emitter raced us). Give up rather than
			// looping — basement is the canary, not the bus.
			e.logger.Error("webhook engine: queue saturated, dropping envelope", "type", env.Type)
		}
	}
}

// EmitCount returns the total number of Emit calls observed by the
// engine. Exposed for tests so they can wait for the dispatcher to
// drain without sleeping.
func (e *Engine) EmitCount() int64 { return e.emitCount.Load() }

// DeliveryCount returns the total number of completed delivery
// attempts across every webhook. Tests use this to wait for the
// engine to settle after Emit.
func (e *Engine) DeliveryCount() int64 { return e.deliveryCount.Load() }

// Subscribe registers a callback that fires for every emitted event,
// BEFORE per-webhook delivery. Used by internal subsystems (e.g.
// federation) that want to react to bucket-level events without
// configuring external HTTP webhooks. Returns an unsubscribe func.
//
// Callbacks run in the dispatcher goroutine — they MUST NOT block.
// Long work should be queued to a separate channel + drained by
// the subscriber's own workers. The federation engine does exactly
// this in v1.7.0f: the callback enqueues a single-object replicate
// task onto a per-federation buffered channel and returns immediately.
//
// Name is a human-readable tag (e.g. "federation") carried in panic
// logs so a runaway subscriber pins to its owning subsystem. The
// returned function is idempotent — calling it twice is a no-op.
func (e *Engine) Subscribe(name string, cb func(EventEnvelope)) func() {
	if cb == nil {
		return func() {}
	}
	e.subMu.Lock()
	e.subSeq++
	token := e.subSeq
	e.subs[token] = subscriberEntry{name: name, cb: cb}
	e.subMu.Unlock()

	return func() {
		e.subMu.Lock()
		delete(e.subs, token)
		e.subMu.Unlock()
	}
}

// SubscriberCount reports the number of currently-registered internal
// subscribers. Tests use this to assert Subscribe / unsubscribe state
// without peeking at engine internals.
func (e *Engine) SubscriberCount() int {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	return len(e.subs)
}

// fanOutSubscribers invokes every registered subscriber callback for
// one envelope. Runs synchronously inside the dispatcher goroutine —
// subscribers are documented as "MUST NOT block". A panic in any one
// callback is logged + recovered so a buggy subscriber can't kill the
// dispatcher.
//
// Snapshot under the lock then release before invoking: this lets a
// callback call Subscribe / unsubscribe without deadlocking on the
// dispatcher's hold of subMu.
func (e *Engine) fanOutSubscribers(env EventEnvelope) {
	e.subMu.Lock()
	if len(e.subs) == 0 {
		e.subMu.Unlock()
		return
	}
	snapshot := make([]subscriberEntry, 0, len(e.subs))
	for _, s := range e.subs {
		snapshot = append(snapshot, s)
	}
	e.subMu.Unlock()

	for _, s := range snapshot {
		func(entry subscriberEntry) {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("webhook engine: panic in subscriber",
						"subscriber", entry.name, "envelopeId", env.ID, "panic", r)
				}
			}()
			entry.cb(env)
		}(s)
	}
}

// runDispatcher is the central event loop. Reads from e.events,
// looks up matching webhooks, and spawns one delivery goroutine per
// (envelope, webhook) pair. Recover-shielded so a panic in the
// lookup path can't kill the loop.
func (e *Engine) runDispatcher(ctx context.Context) {
	defer e.inflight.Done()
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("webhook engine: panic in dispatcher", "panic", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.quit:
			return
		case env := <-e.events:
			e.dispatch(ctx, env)
		}
	}
}

// dispatch resolves matching webhooks for one envelope and spawns a
// delivery goroutine per match. Skipped silently if no webhook matches
// — that's the normal case when an operator hasn't subscribed.
//
// Internal subscribers (registered via Subscribe) fire FIRST and run
// synchronously inside the dispatcher. They MUST NOT block: callbacks
// that need to do real work are required to queue onto their own
// channel + drain via dedicated workers. v1.7.0f added this path so
// the federation engine can react to writes immediately instead of
// waiting for its 10s polling tick.
func (e *Engine) dispatch(ctx context.Context, env EventEnvelope) {
	e.fanOutSubscribers(env)

	hooks, err := e.store.ListForBucket(ctx, env.RegionID, env.Bucket)
	if err != nil {
		e.logger.Warn("webhook engine: ListForBucket failed", "error", err)
		return
	}
	for _, w := range hooks {
		if !w.Matches(env) {
			continue
		}
		e.inflight.Add(1)
		go e.deliver(ctx, w, env)
	}
}

// deliver attempts to POST one envelope to one webhook with retries.
// Records the outcome via store.RecordDelivery + an audit event per
// terminal result (success or final failure). Panic-shielded.
func (e *Engine) deliver(ctx context.Context, w Webhook, env EventEnvelope) {
	defer e.inflight.Done()
	defer e.deliveryCount.Add(1)
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("webhook engine: panic in delivery",
				"webhookId", w.ID, "envelopeId", env.ID, "panic", r)
		}
	}()

	body, err := json.Marshal(env)
	if err != nil {
		e.logger.Error("webhook engine: marshal envelope failed",
			"webhookId", w.ID, "error", err)
		return
	}
	signature := signBody(w.Secret, body)

	attempts := len(e.backoffSchedule) + 1
	var lastResult DeliveryResult
	for attempt := 0; attempt < attempts; attempt++ {
		// Honour Stop / context cancellation between attempts so a
		// shutdown doesn't have to wait a 15s backoff.
		if attempt > 0 {
			delay := e.backoffSchedule[attempt-1]
			select {
			case <-ctx.Done():
				return
			case <-e.quit:
				return
			case <-time.After(delay):
			}
		}
		lastResult = e.attemptDelivery(ctx, w, env, body, signature)
		if lastResult.Success {
			break
		}
	}

	post, err := e.store.RecordDelivery(ctx, w.ID, lastResult)
	if err != nil {
		// Webhook might have been deleted mid-delivery; that's fine
		// — drop the result quietly.
		e.logger.Warn("webhook engine: RecordDelivery failed",
			"webhookId", w.ID, "error", err)
		return
	}

	resource := "webhook:" + w.ID
	if lastResult.Success {
		e.audit.Log(audit.Event{
			Actor:    w.OwnerUserID,
			Action:   "webhook:fired_success",
			Resource: resource,
			Result:   audit.ResultSuccess,
			Detail: fmt.Sprintf("event=%s key=%s status=%d duration_ms=%d",
				env.Type, env.Key, lastResult.HTTPStatus, lastResult.DurationMs),
		})
	} else {
		e.audit.Log(audit.Event{
			Actor:    w.OwnerUserID,
			Action:   "webhook:fired_failure",
			Resource: resource,
			Result:   audit.ResultFailure,
			Detail: fmt.Sprintf("event=%s key=%s status=%d attempts=%d error=%s",
				env.Type, env.Key, lastResult.HTTPStatus, attempts, lastResult.Error),
		})
	}

	// Detect the auto-disable transition: webhook was enabled before
	// this delivery (we read w pre-record), and the store flipped it
	// off because FailureCount crossed the threshold.
	if w.Enabled && !post.Enabled {
		e.audit.Log(audit.Event{
			Actor:    w.OwnerUserID,
			Action:   "webhook:auto_disabled",
			Resource: resource,
			Result:   audit.ResultFailure,
			Detail: fmt.Sprintf("consecutive_failures=%d threshold=%d",
				post.FailureCount, AutoDisableThreshold),
		})
		e.logger.Warn("webhook engine: auto-disabled webhook after consecutive failures",
			"webhookId", w.ID, "failures", post.FailureCount)
	}
}

// attemptDelivery performs one POST and returns the captured result.
// Always returns a non-zero DeliveredAt + DurationMs so the store has
// a useful record even on transport failure.
func (e *Engine) attemptDelivery(ctx context.Context, w Webhook, env EventEnvelope, body []byte, signature string) DeliveryResult {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.TargetURL, bytes.NewReader(body))
	if err != nil {
		return DeliveryResult{
			DeliveredAt: e.nowFn(),
			DurationMs:  int(time.Since(started).Milliseconds()),
			Success:     false,
			Error:       "build request: " + err.Error(),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, signature)
	req.Header.Set("User-Agent", "basement-webhook/1.7")

	resp, err := e.httpClient.Do(req)
	durationMs := int(time.Since(started).Milliseconds())
	if err != nil {
		return DeliveryResult{
			DeliveredAt: e.nowFn(),
			DurationMs:  durationMs,
			Success:     false,
			Error:       err.Error(),
		}
	}
	// Drain + close so the underlying connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	result := DeliveryResult{
		DeliveredAt: e.nowFn(),
		HTTPStatus:  resp.StatusCode,
		DurationMs:  durationMs,
		Success:     resp.StatusCode >= 200 && resp.StatusCode < 300,
	}
	if !result.Success {
		result.Error = fmt.Sprintf("non-2xx status: %d", resp.StatusCode)
	}
	return result
}

// signBody computes HMAC-SHA256(secret, body) and renders the
// X-Basement-Signature header value. Returned format: "sha256=<hex>"
// so a recipient that supports multiple hash algorithms can route on
// the prefix.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature is a recipient-side helper: returns true when the
// provided header value is a valid HMAC-SHA256 over body using
// secret. Exported so tests can exercise the signing contract from
// the receiver's perspective without re-implementing the hex compare.
func VerifySignature(secret string, body []byte, header string) bool {
	expected := signBody(secret, body)
	return hmac.Equal([]byte(expected), []byte(header))
}

// noopAudit is the silent audit.Logger installed when the caller
// passes nil into NewEngine. Mirrors audit.NewNoop() but lives in-
// package so the webhook package doesn't have a hard dependency on
// the audit package's exported noop helper.
type noopAudit struct{}

func (noopAudit) Log(audit.Event)                                                  {}
func (noopAudit) Query(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, error) { return nil, nil }
func (noopAudit) QueryWithTotal(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, int, error) {
	return nil, 0, nil
}
