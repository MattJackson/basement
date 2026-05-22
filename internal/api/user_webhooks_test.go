// Package api: user-tier webhook handler tests (v1.7.0d).
//
// Same shape as user_federated_buckets_test.go — each test stands up
// a Server wired with an in-memory webhook store + a recording mock
// engine that captures Emit calls. The production *webhook.Engine
// satisfies the same webhookEmitter interface, so production wiring
// stays one assignment in main.go.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/store"
	"github.com/mattjackson/basement/internal/webhook"
)

// mockWebhookEngine records every Emit so tests can assert "engine
// fired for envelope X" without standing up the real dispatcher.
type mockWebhookEngine struct {
	mu       sync.Mutex
	envelopes []webhook.EventEnvelope
}

func (m *mockWebhookEngine) Emit(env webhook.EventEnvelope) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envelopes = append(m.envelopes, env)
}

func (m *mockWebhookEngine) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.envelopes)
}

func (m *mockWebhookEngine) last() (webhook.EventEnvelope, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.envelopes) == 0 {
		return webhook.EventEnvelope{}, false
	}
	return m.envelopes[len(m.envelopes)-1], true
}

// webhookTestEnv carries the assembled server + dependencies for
// one test. Cleanup is registered via t.Cleanup in newWebhookTestEnv.
type webhookTestEnv struct {
	srv    *Server
	engine *mockWebhookEngine
	store  webhook.Store
}

// newWebhookTestEnv builds a Server with the webhook store + recording
// mock engine wired in. UserRegions store is wired so the
// BucketFilter-region ownership check has somewhere to look.
func newWebhookTestEnv(t *testing.T) *webhookTestEnv {
	t.Helper()
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir

	st, err := store.Open(dataDir, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireUserRegions(cfg.JWT.Secret); err != nil {
		t.Fatalf("WireUserRegions: %v", err)
	}

	whStore, err := webhook.Open(dataDir)
	if err != nil {
		t.Fatalf("webhook.Open: %v", err)
	}

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	engine := &mockWebhookEngine{}
	srv.SetWebhooks(whStore, engine)

	return &webhookTestEnv{
		srv:    srv,
		engine: engine,
		store:  whStore,
	}
}

// validWebhookBody returns a known-good create body. Tests mutate it
// as needed before posting.
func validWebhookBody(name, target string) map[string]interface{} {
	return map[string]interface{}{
		"name":      name,
		"targetUrl": target,
		"events":    []string{string(webhook.EventObjectDeleted)},
		"secret":    "this-is-a-shared-secret-1234567890",
	}
}

// postJSON posts a JSON body to the given URL with a USER cookie for
// the given user. Returns the recorder.
func postJSON(t *testing.T, srv *Server, userID, urlPath string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(urlPath, body)
	req.AddCookie(userCookie(t, userID))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	return rr
}

// TestCreateWebhook_Happy: a well-formed POST returns 201 with an ID,
// returns the cleartext secret exactly once, and persists into the
// store.
func TestCreateWebhook_Happy(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("ci-hook", "https://ci.example.com/h"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got webhookMintResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Fatalf("expected non-empty ID")
	}
	if got.Secret != "this-is-a-shared-secret-1234567890" {
		t.Fatalf("expected mint response to include cleartext secret, got %q", got.Secret)
	}
	if got.HasSecret != true {
		t.Fatalf("expected HasSecret=true on mint response")
	}
	// GET should redact the secret.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/webhooks/"+got.ID, nil)
	getReq.AddCookie(userCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}
	var read webhookResponse
	_ = json.NewDecoder(getRR.Body).Decode(&read)
	if read.Secret != "" {
		t.Fatalf("expected GET to redact secret, got %q", read.Secret)
	}
	if !read.HasSecret {
		t.Fatalf("expected HasSecret=true on GET")
	}
}

// TestCreateWebhook_AutoGeneratesSecret: when the operator omits a
// secret (or supplies one shorter than the minimum), the server
// generates a fresh one and returns it on the mint response.
func TestCreateWebhook_AutoGeneratesSecret(t *testing.T) {
	env := newWebhookTestEnv(t)
	body := validWebhookBody("auto-secret", "https://ci.example.com/h")
	delete(body, "secret")
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Secret) < webhookMinSecretLength {
		t.Fatalf("expected auto-generated secret of at least %d chars, got %q", webhookMinSecretLength, got.Secret)
	}
}

// TestCreateWebhook_InvalidName: special characters reject with
// 400 INVALID_NAME.
func TestCreateWebhook_InvalidName(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("not allowed!", "https://ci.example.com/h"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_NAME" {
		t.Fatalf("expected code=INVALID_NAME, got %q", resp.Error.Code)
	}
}

// TestCreateWebhook_InvalidURL: a non-http URL rejects with
// 400 INVALID_TARGET_URL.
func TestCreateWebhook_InvalidURL(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("badurl", "not a url"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_TARGET_URL" {
		t.Fatalf("expected code=INVALID_TARGET_URL, got %q", resp.Error.Code)
	}
}

// TestCreateWebhook_InvalidEvent: an unknown event type rejects with
// 400 INVALID_EVENTS.
func TestCreateWebhook_InvalidEvent(t *testing.T) {
	env := newWebhookTestEnv(t)
	body := validWebhookBody("badevent", "https://ci.example.com/h")
	body["events"] = []string{"not.a.real.event"}
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_EVENTS" {
		t.Fatalf("expected code=INVALID_EVENTS, got %q", resp.Error.Code)
	}
}

// TestCreateWebhook_DuplicateName: same user creates two with the
// same name -> second one is 409 DUPLICATE_NAME.
func TestCreateWebhook_DuplicateName(t *testing.T) {
	env := newWebhookTestEnv(t)
	for i := 0; i < 2; i++ {
		rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
			validWebhookBody("ci-hook", "https://ci.example.com/h"))
		if i == 0 {
			if rr.Code != http.StatusCreated {
				t.Fatalf("first create: expected 201, got %d", rr.Code)
			}
			continue
		}
		if rr.Code != http.StatusConflict {
			t.Fatalf("second create: expected 409, got %d body=%s", rr.Code, rr.Body.String())
		}
	}
}

// TestGetWebhook_Ownership: user A's webhook is 404 to user B.
func TestGetWebhook_Ownership(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "alice", "/api/v1/user/webhooks",
		validWebhookBody("alice-hook", "https://ci.example.com/h"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create as alice: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var created webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/webhooks/"+created.ID, nil)
	getReq.AddCookie(userCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404 cross-user, got %d body=%s", getRR.Code, getRR.Body.String())
	}
}

// TestListWebhooks_Scoped: List only returns the caller's webhooks.
func TestListWebhooks_Scoped(t *testing.T) {
	env := newWebhookTestEnv(t)
	_ = postJSON(t, env.srv, "alice", "/api/v1/user/webhooks",
		validWebhookBody("alice-hook", "https://ci.example.com/h"))
	_ = postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("matt-hook", "https://ci.example.com/h"))

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/webhooks", nil)
	listReq.AddCookie(userCookie(t, "matthew"))
	listRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRR.Code)
	}
	var got []webhookResponse
	_ = json.NewDecoder(listRR.Body).Decode(&got)
	if len(got) != 1 {
		t.Fatalf("expected 1 webhook for matthew, got %d", len(got))
	}
	if got[0].Name != "matt-hook" {
		t.Fatalf("expected name=matt-hook, got %q", got[0].Name)
	}
	if got[0].Secret != "" {
		t.Fatalf("expected redacted secret on list, got %q", got[0].Secret)
	}
}

// TestUpdateWebhook_PreservesSecret: an update body with an empty
// secret preserves the existing one.
func TestUpdateWebhook_PreservesSecret(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("preserve", "https://ci.example.com/h"))
	var created webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	patch := map[string]interface{}{
		"name":      "renamed",
		"targetUrl": "https://ci2.example.com/h",
		"events":    []string{string(webhook.EventObjectDeleted)},
	}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/user/webhooks/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	putRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(putRR, req)
	if putRR.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d body=%s", putRR.Code, putRR.Body.String())
	}

	// Confirm secret survived in storage.
	got, err := env.store.Get(req.Context(), created.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Secret != created.Secret {
		t.Fatalf("expected secret preserved across update, got %q", got.Secret)
	}
}

// TestUpdateWebhook_RotatesSecret: an update body with a new secret
// rotates the stored value and returns the new cleartext on the
// response.
func TestUpdateWebhook_RotatesSecret(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("rotate", "https://ci.example.com/h"))
	var created webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	newSecret := "freshly-rotated-secret-9876543210"
	patch := map[string]interface{}{
		"name":      "rotate",
		"targetUrl": "https://ci.example.com/h",
		"events":    []string{string(webhook.EventObjectDeleted)},
		"secret":    newSecret,
	}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/user/webhooks/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	putRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(putRR, req)
	if putRR.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d body=%s", putRR.Code, putRR.Body.String())
	}

	var minted webhookMintResponse
	_ = json.NewDecoder(putRR.Body).Decode(&minted)
	if minted.Secret != newSecret {
		t.Fatalf("expected response to mint new secret %q, got %q", newSecret, minted.Secret)
	}
	got, _ := env.store.Get(req.Context(), created.ID)
	if got.Secret != newSecret {
		t.Fatalf("expected store to keep rotated secret, got %q", got.Secret)
	}
}

// TestTestWebhook_EmitsEnvelope: POST /test fires an envelope via
// the engine using the webhook's first subscribed event.
func TestTestWebhook_EmitsEnvelope(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("test-fire", "https://ci.example.com/h"))
	var created webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/webhooks/"+created.ID+"/test", nil)
	req.AddCookie(userCookie(t, "matthew"))
	testRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(testRR, req)
	if testRR.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", testRR.Code, testRR.Body.String())
	}
	if env.engine.count() != 1 {
		t.Fatalf("expected engine to receive 1 envelope, got %d", env.engine.count())
	}
	got, _ := env.engine.last()
	if got.Type != webhook.EventObjectDeleted {
		t.Fatalf("expected synthetic event type to be the first subscribed event, got %q", got.Type)
	}
}

// TestEnableDisableWebhook: the toggle endpoints flip Enabled and the
// stored row reflects the change. Disabled webhook can't be /test.
func TestEnableDisableWebhook(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("toggle", "https://ci.example.com/h"))
	var created webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	disable := httptest.NewRequest(http.MethodPost, "/api/v1/user/webhooks/"+created.ID+"/disable", nil)
	disable.AddCookie(userCookie(t, "matthew"))
	disableRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(disableRR, disable)
	if disableRR.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d body=%s", disableRR.Code, disableRR.Body.String())
	}
	got, _ := env.store.Get(disable.Context(), created.ID)
	if got.Enabled {
		t.Fatalf("expected Enabled=false after disable")
	}

	// Test on a disabled webhook is 400 WEBHOOK_DISABLED.
	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/user/webhooks/"+created.ID+"/test", nil)
	testReq.AddCookie(userCookie(t, "matthew"))
	testRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(testRR, testReq)
	if testRR.Code != http.StatusBadRequest {
		t.Fatalf("test-on-disabled: expected 400, got %d body=%s", testRR.Code, testRR.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(testRR.Body).Decode(&resp)
	if resp.Error.Code != "WEBHOOK_DISABLED" {
		t.Fatalf("expected code=WEBHOOK_DISABLED, got %q", resp.Error.Code)
	}

	enable := httptest.NewRequest(http.MethodPost, "/api/v1/user/webhooks/"+created.ID+"/enable", nil)
	enable.AddCookie(userCookie(t, "matthew"))
	enableRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(enableRR, enable)
	if enableRR.Code != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d body=%s", enableRR.Code, enableRR.Body.String())
	}
	got, _ = env.store.Get(enable.Context(), created.ID)
	if !got.Enabled {
		t.Fatalf("expected Enabled=true after enable")
	}
}

// TestDeleteWebhook: a deleted webhook is gone from the store and
// returns 404 on a subsequent GET.
func TestDeleteWebhook(t *testing.T) {
	env := newWebhookTestEnv(t)
	rr := postJSON(t, env.srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("delete-me", "https://ci.example.com/h"))
	var created webhookMintResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/user/webhooks/"+created.ID, nil)
	delReq.AddCookie(userCookie(t, "matthew"))
	delRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("DELETE: expected 204, got %d body=%s", delRR.Code, delRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/webhooks/"+created.ID, nil)
	getReq.AddCookie(userCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	env.srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("post-delete GET: expected 404, got %d", getRR.Code)
	}
}

// TestWebhooksNotWired: 503 WEBHOOKS_NOT_WIRED on POST when the
// subsystem was never set up.
func TestWebhooksNotWired(t *testing.T) {
	cfg := newTestConfig()
	cfg.DataDir = t.TempDir()
	st, _ := store.Open(cfg.DataDir, 90*24*time.Hour)
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)

	rr := postJSON(t, srv, "matthew", "/api/v1/user/webhooks",
		validWebhookBody("nope", "https://ci.example.com/h"))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "WEBHOOKS_NOT_WIRED" {
		t.Fatalf("expected code=WEBHOOKS_NOT_WIRED, got %q", resp.Error.Code)
	}
}

// TestCreateWebhook_NoAuth: unauthenticated POST returns 401.
func TestCreateWebhook_NoAuth(t *testing.T) {
	env := newWebhookTestEnv(t)
	req := newJSONRequest("/api/v1/user/webhooks",
		validWebhookBody("nope", "https://ci.example.com/h"))
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}
