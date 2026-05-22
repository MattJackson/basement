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
