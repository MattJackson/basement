package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastBackoff is the retry schedule used by the delivery tests so a
// "retry-until-success" path completes in milliseconds rather than
// the production 21s total.
var fastBackoff = []time.Duration{
	time.Millisecond,
	time.Millisecond,
	time.Millisecond,
}

// captureServer is an httptest.Server-backed recorder that captures
// every request body + headers and lets each test configure a status
// code (potentially varying across calls).
type captureServer struct {
	mu       sync.Mutex
	requests []capturedRequest
	server   *httptest.Server
	// statusFor returns the HTTP status the server should send for
	// call number n (1-indexed). Defaults to 200.
	statusFor func(n int) int
}

type capturedRequest struct {
	body      []byte
	signature string
	header    http.Header
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	cs := &captureServer{
		statusFor: func(int) int { return http.StatusOK },
	}
	cs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cs.mu.Lock()
		n := len(cs.requests) + 1
		cs.requests = append(cs.requests, capturedRequest{
			body:      body,
			signature: r.Header.Get(SignatureHeader),
			header:    r.Header.Clone(),
		})
		statusFn := cs.statusFor
		cs.mu.Unlock()
		w.WriteHeader(statusFn(n))
	}))
	t.Cleanup(cs.server.Close)
	return cs
}

func (cs *captureServer) URL() string { return cs.server.URL }

func (cs *captureServer) count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.requests)
}

// waitForDeliveries blocks until the engine reports at least n
// completed deliveries, or t.Fatals on a 2s deadline. Used in lieu of
// time.Sleep to drain the dispatcher deterministically.
func waitForDeliveries(t *testing.T, e *Engine, n int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e.DeliveryCount() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d deliveries, got %d", n, e.DeliveryCount())
}

// newTestEngine wires an Engine against a fresh in-memory store with
// the fast backoff schedule. Caller can stop it via t.Cleanup.
func newTestEngine(t *testing.T) (*Engine, Store, *captureServer) {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	engine := NewEngine(store, nil, nil)
	engine.SetBackoffSchedule(fastBackoff)
	cs := newCaptureServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	engine.Start(ctx)
	t.Cleanup(func() {
		cancel()
		engine.Stop()
	})
	return engine, store, cs
}

// TestEngineEmitDeliver: a happy-path delivery POSTs the envelope to
// the target with the right signature header and a successful result
// is recorded.
func TestEngineEmitDeliver(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()
	w, _ := store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "happy",
		TargetURL:   cs.URL(),
		Events:      []EventType{EventObjectCreated},
		Secret:      "secret-shared-1234567890abcdef",
		Enabled:     true,
	})

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "photos/2026/dog.jpg",
		Size:     12345,
		ETag:     "\"abcd\"",
	})

	waitForDeliveries(t, engine, 1)

	if cs.count() != 1 {
		t.Fatalf("expected 1 captured request, got %d", cs.count())
	}
	cs.mu.Lock()
	req := cs.requests[0]
	cs.mu.Unlock()

	if !VerifySignature(w.Secret, req.body, req.signature) {
		t.Fatalf("signature %q did not verify against body", req.signature)
	}
	if got := req.header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", got)
	}

	post, _ := store.Get(ctx, w.ID)
	if post.LastDelivery == nil {
		t.Fatalf("expected LastDelivery to be set")
	}
	if !post.LastDelivery.Success {
		t.Fatalf("expected Success=true, got %+v", post.LastDelivery)
	}
	if post.LastDelivery.HTTPStatus != http.StatusOK {
		t.Fatalf("expected HTTPStatus=200, got %d", post.LastDelivery.HTTPStatus)
	}
	if post.FailureCount != 0 {
		t.Fatalf("expected FailureCount=0, got %d", post.FailureCount)
	}
}

// TestEngineRetryOnTransientFailure: two 500s followed by a 200 within
// the retry schedule is recorded as a success — failure count stays at 0.
func TestEngineRetryOnTransientFailure(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()

	// First two attempts 503, third 200.
	cs.mu.Lock()
	cs.statusFor = func(n int) int {
		if n < 3 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	}
	cs.mu.Unlock()

	w, _ := store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "retry",
		TargetURL:   cs.URL(),
		Events:      []EventType{EventObjectDeleted},
		Secret:      "secret-shared-1234567890abcdef",
		Enabled:     true,
	})

	engine.Emit(EventEnvelope{
		Type:     EventObjectDeleted,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "photos/2026/cat.jpg",
	})
	waitForDeliveries(t, engine, 1)

	if cs.count() != 3 {
		t.Fatalf("expected 3 POSTs (2 fail + 1 success), got %d", cs.count())
	}
	post, _ := store.Get(ctx, w.ID)
	if !post.LastDelivery.Success {
		t.Fatalf("expected Success=true after retry, got %+v", post.LastDelivery)
	}
	if post.FailureCount != 0 {
		t.Fatalf("expected FailureCount=0 on eventual success, got %d", post.FailureCount)
	}
}

// TestEngineAutoDisableAfterRepeatedFailure: AutoDisableThreshold
// consecutive deliveries that fail every retry flip Enabled=false.
func TestEngineAutoDisableAfterRepeatedFailure(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()

	// Every attempt 500 — the engine exhausts its retries every time.
	cs.mu.Lock()
	cs.statusFor = func(int) int { return http.StatusInternalServerError }
	cs.mu.Unlock()

	w, _ := store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "doomed",
		TargetURL:   cs.URL(),
		Events:      []EventType{EventObjectCreated},
		Secret:      "secret-shared-1234567890abcdef",
		Enabled:     true,
	})

	for i := 0; i < AutoDisableThreshold; i++ {
		engine.Emit(EventEnvelope{
			Type:     EventObjectCreated,
			RegionID: "region-a",
			Bucket:   "lsi",
			Key:      "k",
		})
	}
	waitForDeliveries(t, engine, int64(AutoDisableThreshold))

	post, _ := store.Get(ctx, w.ID)
	if post.Enabled {
		t.Fatalf("expected webhook auto-disabled after %d failures, still enabled", AutoDisableThreshold)
	}
	if post.FailureCount < AutoDisableThreshold {
		t.Fatalf("expected FailureCount>=%d, got %d", AutoDisableThreshold, post.FailureCount)
	}
}

// TestEngineFilterEventType: an envelope whose Type isn't in the
// webhook's Events list never fires.
func TestEngineFilterEventType(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()
	_, _ = store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "deletes-only",
		TargetURL:   cs.URL(),
		Events:      []EventType{EventObjectDeleted},
		Enabled:     true,
	})

	// Created event is filtered out.
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k",
	})
	// Delete event matches.
	engine.Emit(EventEnvelope{
		Type:     EventObjectDeleted,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k",
	})
	waitForDeliveries(t, engine, 1)
	if cs.count() != 1 {
		t.Fatalf("expected 1 captured POST (delete only), got %d", cs.count())
	}
}

// TestEngineFilterBucket: a webhook scoped to a different bucket
// doesn't fire on the envelope.
func TestEngineFilterBucket(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()
	_, _ = store.Create(ctx, Webhook{
		OwnerUserID:  "matthew",
		Name:         "lsi-only",
		TargetURL:    cs.URL(),
		Events:       []EventType{EventObjectCreated},
		BucketFilter: &BucketFilter{RegionID: "region-a", Bucket: "lsi"},
		Enabled:      true,
	})

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "other",
		Key:      "k",
	})
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k",
	})
	waitForDeliveries(t, engine, 1)
	if cs.count() != 1 {
		t.Fatalf("expected 1 captured POST (matching bucket only), got %d", cs.count())
	}
}

// TestEngineFilterPrefix: a webhook scoped to prefix "photos/" only
// fires for matching keys.
func TestEngineFilterPrefix(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()
	_, _ = store.Create(ctx, Webhook{
		OwnerUserID:  "matthew",
		Name:         "photos-only",
		TargetURL:    cs.URL(),
		Events:       []EventType{EventObjectCreated},
		PrefixFilter: "photos/",
		Enabled:      true,
	})

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "videos/cat.mp4",
	})
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "photos/dog.jpg",
	})
	waitForDeliveries(t, engine, 1)
	if cs.count() != 1 {
		t.Fatalf("expected 1 captured POST (prefix match only), got %d", cs.count())
	}
}

// TestEngineDisabledWebhookNeverFires: an Enabled=false webhook is
// silently skipped even when other filters match.
func TestEngineDisabledWebhookNeverFires(t *testing.T) {
	engine, store, cs := newTestEngine(t)
	ctx := context.Background()
	_, _ = store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "disabled",
		TargetURL:   cs.URL(),
		Events:      []EventType{EventObjectCreated},
		Enabled:     false,
	})
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k",
	})
	// Wait briefly to confirm no delivery happens.
	time.Sleep(20 * time.Millisecond)
	if cs.count() != 0 {
		t.Fatalf("expected 0 POSTs for disabled webhook, got %d", cs.count())
	}
}

// TestEngineSignatureRoundTrip: VerifySignature accepts a signature
// signed with the same secret and rejects one signed with a different
// secret.
func TestEngineSignatureRoundTrip(t *testing.T) {
	body := []byte(`{"foo":"bar"}`)
	good := signBody("the-right-secret", body)
	if !VerifySignature("the-right-secret", body, good) {
		t.Fatalf("expected VerifySignature to accept matching signature")
	}
	if VerifySignature("the-wrong-secret", body, good) {
		t.Fatalf("expected VerifySignature to reject signature from different secret")
	}
}

// TestEngineRobustToPanicInDelivery: a panicking transport must not
// kill the engine — subsequent emits still get processed.
//
// We inject panic via a custom RoundTripper that fires once then
// reverts to a passthrough.
func TestEngineRobustToPanicInDelivery(t *testing.T) {
	store, _ := Open(t.TempDir())
	cs := newCaptureServer(t)

	engine := NewEngine(store, nil, nil)
	engine.SetBackoffSchedule(fastBackoff)

	var panicked atomic.Bool
	rt := &panicOnceRT{
		panicked: &panicked,
		inner:    http.DefaultTransport,
	}
	engine.SetHTTPClient(&http.Client{Transport: rt, Timeout: 5 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)
	defer engine.Stop()

	ctx2 := context.Background()
	_, _ = store.Create(ctx2, Webhook{
		OwnerUserID: "matthew",
		Name:        "panicky",
		TargetURL:   cs.URL(),
		Events:      []EventType{EventObjectCreated},
		Enabled:     true,
	})

	// First emit triggers the panic in the transport.
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k1",
	})
	// Second emit should still be processed (engine survived).
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k2",
	})

	waitForDeliveries(t, engine, 2)
	if cs.count() < 1 {
		t.Fatalf("expected at least one POST after panic, got %d", cs.count())
	}
}

// panicOnceRT is a RoundTripper that panics on its first call, then
// delegates to inner. Used by TestEngineRobustToPanicInDelivery.
type panicOnceRT struct {
	panicked *atomic.Bool
	inner    http.RoundTripper
}

func (r *panicOnceRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.panicked.CompareAndSwap(false, true) {
		panic("synthetic transport panic for test")
	}
	return r.inner.RoundTrip(req)
}

// waitForSubscriberCalls blocks until n >= want or the deadline expires.
// Used by the Subscribe tests to drain the dispatcher without a fixed
// sleep that races slow CI runners.
func waitForSubscriberCalls(t *testing.T, got *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d subscriber calls, got %d", want, got.Load())
}

// TestEngine_SubscribeCallback: registering a Subscribe callback fires
// for every Emit even when no external webhook is configured. The
// fan-out runs BEFORE per-webhook delivery so subscribers see the
// envelope synchronously inside the dispatcher.
func TestEngine_SubscribeCallback(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	var calls atomic.Int64
	var lastBucket atomic.Value // string
	unsub := engine.Subscribe("test-sub", func(env EventEnvelope) {
		calls.Add(1)
		lastBucket.Store(env.Bucket)
	})
	defer unsub()

	if got := engine.SubscriberCount(); got != 1 {
		t.Fatalf("expected 1 subscriber registered, got %d", got)
	}

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k1",
	})
	engine.Emit(EventEnvelope{
		Type:     EventObjectDeleted,
		RegionID: "region-a",
		Bucket:   "photos",
		Key:      "k2",
	})

	waitForSubscriberCalls(t, &calls, 2)

	if got := lastBucket.Load(); got != "photos" {
		t.Fatalf("expected last bucket = photos, got %v", got)
	}
}

// TestEngine_UnsubscribeStopsCallback: calling the returned unsubscribe
// func stops further callback invocations. Emits after unsubscribe go
// to no-one.
func TestEngine_UnsubscribeStopsCallback(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	var calls atomic.Int64
	unsub := engine.Subscribe("test-sub", func(env EventEnvelope) {
		calls.Add(1)
	})

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "before-unsub",
	})
	waitForSubscriberCalls(t, &calls, 1)

	unsub()
	if got := engine.SubscriberCount(); got != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", got)
	}

	// Calling unsubscribe again is a no-op (must not panic).
	unsub()

	// Emit after unsubscribe — callback must NOT fire.
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "after-unsub",
	})

	// Wait a beat so the dispatcher would have run the callback if it
	// were still registered. Then assert counter unchanged.
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 call (pre-unsub only), got %d", got)
	}
}

// TestEngine_SubscribeMultiple: N subscribers all fire on every emit
// and stay independent — unsubscribing one doesn't affect the others.
func TestEngine_SubscribeMultiple(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	var calls1, calls2, calls3 atomic.Int64
	unsub1 := engine.Subscribe("sub-1", func(EventEnvelope) { calls1.Add(1) })
	unsub2 := engine.Subscribe("sub-2", func(EventEnvelope) { calls2.Add(1) })
	unsub3 := engine.Subscribe("sub-3", func(EventEnvelope) { calls3.Add(1) })
	defer unsub1()
	defer unsub3()

	if got := engine.SubscriberCount(); got != 3 {
		t.Fatalf("expected 3 subscribers, got %d", got)
	}

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k1",
	})
	waitForSubscriberCalls(t, &calls1, 1)
	waitForSubscriberCalls(t, &calls2, 1)
	waitForSubscriberCalls(t, &calls3, 1)

	// Drop sub-2; sub-1 + sub-3 should still receive the next emit.
	unsub2()
	if got := engine.SubscriberCount(); got != 2 {
		t.Fatalf("expected 2 subscribers after one unsubscribe, got %d", got)
	}

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k2",
	})
	waitForSubscriberCalls(t, &calls1, 2)
	waitForSubscriberCalls(t, &calls3, 2)

	// Give the dispatcher a beat to (incorrectly) call the dropped
	// subscriber, then assert it didn't.
	time.Sleep(20 * time.Millisecond)
	if got := calls2.Load(); got != 1 {
		t.Fatalf("dropped subscriber should still be at 1 call, got %d", got)
	}
}

// TestEngine_SubscribePanicSafe: a panicking subscriber must not kill
// the dispatcher — subsequent subscribers + emits still get processed.
func TestEngine_SubscribePanicSafe(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	var goodCalls atomic.Int64
	unsubBad := engine.Subscribe("bad", func(EventEnvelope) {
		panic("synthetic subscriber panic")
	})
	defer unsubBad()
	unsubGood := engine.Subscribe("good", func(EventEnvelope) {
		goodCalls.Add(1)
	})
	defer unsubGood()

	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k",
	})

	waitForSubscriberCalls(t, &goodCalls, 1)

	// Second emit should still flow even though the bad subscriber
	// panicked on the first.
	engine.Emit(EventEnvelope{
		Type:     EventObjectCreated,
		RegionID: "region-a",
		Bucket:   "lsi",
		Key:      "k2",
	})
	waitForSubscriberCalls(t, &goodCalls, 2)
}
