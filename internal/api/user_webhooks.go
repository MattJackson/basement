// Package api: user-persona webhook subscription endpoints (v1.7.0d).
//
// Webhooks let an operator subscribe to bucket events ("POST to my CI
// when object X is uploaded into bucket Y"). Storage + delivery live
// in internal/webhook; the handlers here are pure CRUD plus a /test
// endpoint that emits a synthetic event so an operator can validate
// their target URL + signing secret without waiting for real traffic.
//
// Authorization: USER tier only. Webhooks are first-class user-
// property — the operator who set one up is the only one who can see,
// edit, delete, or fire them. Ownership is verified via OwnerUserID; a
// mismatch collapses to 404 (never 403) so the API never leaks the
// existence of other users' webhooks. Same convention as
// user_backups.go, user_federated_buckets.go, user_regions.go.
//
// Secret handling: identical to v1.7.0a service-account secrets — the
// shared HMAC secret is returned ONLY on the initial mint response
// (Create + Update when the caller supplied a new secret). List and
// Get redact the field so a stale tab or a shoulder-surf can't leak
// it. The on-disk store keeps the secret in cleartext (it must, to
// sign outbound bodies), but the wire never re-exposes it.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/webhook"
)

// webhookEmitter is the narrow slice of *webhook.Engine the API
// handlers need. Defined as an interface so user_webhooks_test.go can
// substitute a recording mock without standing up the real
// dispatcher.
type webhookEmitter interface {
	Emit(env webhook.EventEnvelope)
}

// webhookCreateRequest is the wire shape for POST /user/webhooks and
// the body of PUT /user/webhooks/{id} (same fields are mutable).
//
// Secret is optional on create — when omitted (or shorter than the
// minimum) the server generates a fresh 32-character hex token. On
// update, an omitted secret preserves the existing one; supplying a
// new value rotates it.
type webhookCreateRequest struct {
	Name         string                `json:"name"`
	TargetURL    string                `json:"targetUrl"`
	Events       []webhook.EventType   `json:"events"`
	BucketFilter *webhook.BucketFilter `json:"bucketFilter,omitempty"`
	PrefixFilter string                `json:"prefixFilter,omitempty"`
	Secret       string                `json:"secret,omitempty"`
	Enabled      *bool                 `json:"enabled,omitempty"`
}

// webhookResponse is what handlers return on every read path. The
// Secret field is deliberately redacted; the initial-mint response
// uses webhookMintResponse to surface it exactly once.
type webhookResponse struct {
	webhook.Webhook
	// HasSecret is true whenever the stored Secret is non-empty.
	// Redacted secrets render as the empty string on the wire; this
	// flag lets the FE tell "no secret configured" apart from
	// "redacted from this response" without re-fetching.
	HasSecret bool `json:"hasSecret"`
}

// webhookMintResponse is the Create / rotated-update response shape:
// the full webhook plus the cleartext secret. ONLY returned on these
// two paths; every subsequent read goes through webhookResponse.
type webhookMintResponse struct {
	webhookResponse
	Secret string `json:"secret"`
}

// Name policy: alphanumeric + dash + underscore, 3-64 chars. Same
// shape as federation names so an operator picks up the same
// conventions across the product.
var webhookNameRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

// Minimum secret length when the operator supplies their own. Below
// this we fall back to a generated 32-char hex token so the engine
// never signs with a weak shared key.
const webhookMinSecretLength = 16

// generatedSecretLength is the byte length of the random source for
// auto-generated secrets — hex-encoded that's a 32-char string,
// comfortably above webhookMinSecretLength.
const generatedSecretLength = 16

// validateWebhookRequest returns the first user-visible reason a
// create / update body should be rejected, or "" if everything looks
// well-formed. Returns the typed error code so the caller can map it
// to the matching HTTP status.
func validateWebhookRequest(req webhookCreateRequest, ownedRegions map[string]bool) (code, msg string) {
	name := strings.TrimSpace(req.Name)
	if !webhookNameRegex.MatchString(name) {
		return "INVALID_NAME", "Name must be 3-64 characters of letters, digits, dashes, or underscores"
	}
	target := strings.TrimSpace(req.TargetURL)
	if target == "" {
		return "INVALID_TARGET_URL", "Target URL is required"
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "INVALID_TARGET_URL", "Target URL must be a fully-qualified URL"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "INVALID_TARGET_URL", "Target URL must use http or https"
	}
	if len(req.Events) == 0 {
		return "INVALID_EVENTS", "At least one event type is required"
	}
	for _, e := range req.Events {
		if !webhook.IsKnownEvent(e) {
			return "INVALID_EVENTS", fmt.Sprintf("Unknown event type %q", e)
		}
	}
	if req.BucketFilter != nil && req.BucketFilter.RegionID != "" {
		if ownedRegions != nil && !ownedRegions[strings.TrimSpace(req.BucketFilter.RegionID)] {
			return "INVALID_REGION", "Bucket filter region must be one of your own regions"
		}
	}
	return "", ""
}

// generateSecret returns a fresh hex-encoded random token used when
// the operator declines to supply one (or supplies a token that's too
// short). Uses crypto/rand so the secret is genuinely unguessable.
func generateSecret() (string, error) {
	buf := make([]byte, generatedSecretLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// resolveSecret picks the secret to persist: the operator's choice
// when it meets the minimum length, otherwise a freshly generated
// token. Returns (secret, wasGenerated, error).
func resolveSecret(supplied string) (string, bool, error) {
	supplied = strings.TrimSpace(supplied)
	if len(supplied) >= webhookMinSecretLength {
		return supplied, false, nil
	}
	gen, err := generateSecret()
	if err != nil {
		return "", false, err
	}
	return gen, true, nil
}

// userOwnedRegionSet returns a set of regionIDs owned by the caller —
// used by validateWebhookRequest to refuse a BucketFilter pointing at
// someone else's region. nil result means the regions store isn't
// wired, in which case the BucketFilter ownership check is skipped
// (handlers higher up have already 503'd if the store is missing).
func (s *Server) userOwnedRegionSet(ctx context.Context, userID string) map[string]bool {
	regions := s.regionsStore()
	if regions == nil {
		return nil
	}
	list, err := regions.ListForUser(ctx, userID)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(list))
	for _, r := range list {
		out[r.ID] = true
	}
	return out
}

// requireOwnedWebhook is the shared "load + ownership check" used by
// every per-record handler. 404 on missing OR not-owner so the
// existence of other users' webhooks never leaks.
func (s *Server) requireOwnedWebhook(w http.ResponseWriter, r *http.Request) (webhook.Webhook, string, bool) {
	if s.webhooks == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "WEBHOOKS_NOT_WIRED",
			"Webhook subsystem is not enabled on this server")
		return webhook.Webhook{}, "", false
	}
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return webhook.Webhook{}, "", false
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Webhook ID required")
		return webhook.Webhook{}, "", false
	}
	wh, err := s.webhooks.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, webhook.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "Webhook not found")
			return webhook.Webhook{}, "", false
		}
		writeErrorSimple(w, http.StatusInternalServerError, "WEBHOOK_STORE_ERROR", err.Error())
		return webhook.Webhook{}, "", false
	}
	if wh.OwnerUserID != claims.UserID {
		writeErrorSimple(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "Webhook not found")
		return webhook.Webhook{}, "", false
	}
	return wh, claims.UserID, true
}

// redactWebhook builds a webhookResponse with the Secret stripped.
// HasSecret records whether the underlying row HAS a secret so the
// FE can distinguish "no secret yet" from "secret redacted".
func redactWebhook(wh webhook.Webhook) webhookResponse {
	hasSecret := wh.Secret != ""
	wh.Secret = ""
	return webhookResponse{Webhook: wh, HasSecret: hasSecret}
}

// resourceWebhook builds the canonical audit Resource string.
func resourceWebhook(id string) string { return "webhook:" + id }

// userCreateWebhookHandler handles POST /api/v1/user/webhooks.
//
// On success returns 201 with the full record AND the cleartext
// secret (this is the mint-and-only-return-once moment). Subsequent
// reads redact the secret.
func (s *Server) userCreateWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "WEBHOOKS_NOT_WIRED",
			"Webhook subsystem is not enabled on this server")
		return
	}
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req webhookCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	owned := s.userOwnedRegionSet(r.Context(), claims.UserID)
	if code, msg := validateWebhookRequest(req, owned); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}

	secret, _, err := resolveSecret(req.Secret)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "SECRET_GEN_FAILED",
			"Failed to generate signing secret: "+err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	wh := webhook.Webhook{
		OwnerUserID:  claims.UserID,
		Name:         strings.TrimSpace(req.Name),
		TargetURL:    strings.TrimSpace(req.TargetURL),
		Events:       req.Events,
		BucketFilter: req.BucketFilter,
		PrefixFilter: req.PrefixFilter,
		Secret:       secret,
		Enabled:      enabled,
	}
	created, err := s.webhooks.Create(r.Context(), wh)
	if err != nil {
		if errors.Is(err, webhook.ErrDuplicateName) {
			s.auditFailure(r, "webhook:create", resourceWebhook(""), err)
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_NAME",
				"You already have a webhook with this name. Pick a different name.")
			return
		}
		s.auditFailure(r, "webhook:create", resourceWebhook(""), err)
		writeErrorSimple(w, http.StatusInternalServerError, "WEBHOOK_STORE_ERROR", err.Error())
		return
	}
	s.auditSuccess(r, "webhook:create", resourceWebhook(created.ID))

	// Mint response: redact + reattach the cleartext secret exactly
	// once so the operator can copy it into their target.
	resp := webhookMintResponse{
		webhookResponse: redactWebhook(created),
		Secret:          secret,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// userListWebhooksHandler handles GET /api/v1/user/webhooks.
func (s *Server) userListWebhooksHandler(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		// Empty list is the safe degraded response — clients render
		// "no webhooks" rather than a 5xx.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]webhookResponse{})
		return
	}
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}
	rows, err := s.webhooks.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "WEBHOOK_STORE_ERROR", err.Error())
		return
	}
	out := make([]webhookResponse, 0, len(rows))
	for _, wh := range rows {
		out = append(out, redactWebhook(wh))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// userGetWebhookHandler handles GET /api/v1/user/webhooks/{id}.
func (s *Server) userGetWebhookHandler(w http.ResponseWriter, r *http.Request) {
	wh, _, ok := s.requireOwnedWebhook(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(redactWebhook(wh))
}

// userUpdateWebhookHandler handles PUT /api/v1/user/webhooks/{id}.
//
// Treats the request body as a full replacement of the mutable
// fields. An empty / short Secret preserves the existing one; a
// rotated Secret is returned (mint-style) in the response.
func (s *Server) userUpdateWebhookHandler(w http.ResponseWriter, r *http.Request) {
	existing, userID, ok := s.requireOwnedWebhook(w, r)
	if !ok {
		return
	}
	var req webhookCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	owned := s.userOwnedRegionSet(r.Context(), userID)
	if code, msg := validateWebhookRequest(req, owned); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}

	// Secret handling: empty -> preserve, supplied + long enough ->
	// rotate to the operator's value, supplied + too short -> generate.
	patchSecret := strings.TrimSpace(req.Secret)
	secretRotated := false
	if patchSecret != "" {
		newSecret, generated, err := resolveSecret(patchSecret)
		if err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "SECRET_GEN_FAILED",
				"Failed to generate signing secret: "+err.Error())
			return
		}
		patchSecret = newSecret
		secretRotated = true
		_ = generated // generated path is opaque to the caller; the rotated bit is what matters
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	patch := webhook.Webhook{
		Name:         strings.TrimSpace(req.Name),
		TargetURL:    strings.TrimSpace(req.TargetURL),
		Events:       req.Events,
		BucketFilter: req.BucketFilter,
		PrefixFilter: req.PrefixFilter,
		Secret:       patchSecret, // "" preserves; non-"" rotates
		Enabled:      enabled,
	}
	updated, err := s.webhooks.Update(r.Context(), existing.ID, patch)
	if err != nil {
		if errors.Is(err, webhook.ErrDuplicateName) {
			s.auditFailure(r, "webhook:update", resourceWebhook(existing.ID), err)
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_NAME",
				"You already have a webhook with this name. Pick a different name.")
			return
		}
		s.auditFailure(r, "webhook:update", resourceWebhook(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "WEBHOOK_STORE_ERROR", err.Error())
		return
	}
	s.auditSuccess(r, "webhook:update", resourceWebhook(updated.ID))

	w.Header().Set("Content-Type", "application/json")
	if secretRotated {
		_ = json.NewEncoder(w).Encode(webhookMintResponse{
			webhookResponse: redactWebhook(updated),
			Secret:          patchSecret,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(redactWebhook(updated))
}

// userDeleteWebhookHandler handles DELETE /api/v1/user/webhooks/{id}.
func (s *Server) userDeleteWebhookHandler(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := s.requireOwnedWebhook(w, r)
	if !ok {
		return
	}
	if err := s.webhooks.Delete(r.Context(), existing.ID); err != nil {
		s.auditFailure(r, "webhook:delete", resourceWebhook(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "WEBHOOK_STORE_ERROR", err.Error())
		return
	}
	s.auditSuccess(r, "webhook:delete", resourceWebhook(existing.ID))
	w.WriteHeader(http.StatusNoContent)
}

// userTestWebhookHandler handles POST /api/v1/user/webhooks/{id}/test.
//
// Emits a synthetic envelope so the operator can validate the target
// URL + secret without waiting for real traffic. The envelope's
// Type defaults to the FIRST event in the webhook's subscription
// list so the filter check passes by construction.
func (s *Server) userTestWebhookHandler(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := s.requireOwnedWebhook(w, r)
	if !ok {
		return
	}
	if s.webhookEngine == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "WEBHOOK_ENGINE_NOT_WIRED",
			"Webhook delivery engine is not enabled on this server")
		return
	}
	if !existing.Enabled {
		writeErrorSimple(w, http.StatusBadRequest, "WEBHOOK_DISABLED",
			"Webhook is disabled — enable it before sending a test event")
		return
	}
	if len(existing.Events) == 0 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_EVENTS",
			"Webhook has no subscribed events to test against")
		return
	}

	// Build a synthetic envelope that passes the webhook's filter by
	// construction. If a BucketFilter is set we use those values;
	// otherwise we tag a "test/" region/bucket so the audit detail
	// makes the synthetic nature obvious.
	regionID := "test-region"
	bucket := "test-bucket"
	if existing.BucketFilter != nil {
		if existing.BucketFilter.RegionID != "" {
			regionID = existing.BucketFilter.RegionID
		}
		if existing.BucketFilter.Bucket != "" {
			bucket = existing.BucketFilter.Bucket
		}
	}
	key := "test/synthetic-event.txt"
	if existing.PrefixFilter != "" {
		key = existing.PrefixFilter + "synthetic-event.txt"
	}
	env := webhook.EventEnvelope{
		Type:       existing.Events[0],
		OccurredAt: time.Now().UTC(),
		RegionID:   regionID,
		Bucket:     bucket,
		Key:        key,
		Size:       42,
		ETag:       "\"synthetic-test-event\"",
	}
	s.webhookEngine.Emit(env)
	s.auditSuccess(r, "webhook:test", resourceWebhook(existing.ID))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     existing.ID,
		"status": "queued",
		"event":  env.Type,
	})
}

// userEnableWebhookHandler handles POST /api/v1/user/webhooks/{id}/enable.
func (s *Server) userEnableWebhookHandler(w http.ResponseWriter, r *http.Request) {
	s.toggleWebhook(w, r, true, "webhook:enable")
}

// userDisableWebhookHandler handles POST /api/v1/user/webhooks/{id}/disable.
func (s *Server) userDisableWebhookHandler(w http.ResponseWriter, r *http.Request) {
	s.toggleWebhook(w, r, false, "webhook:disable")
}

// toggleWebhook is the shared body for enable + disable. Both audit
// distinct actions so an operator can grep for either one in the log.
func (s *Server) toggleWebhook(w http.ResponseWriter, r *http.Request, enable bool, action string) {
	existing, _, ok := s.requireOwnedWebhook(w, r)
	if !ok {
		return
	}
	patch := webhook.Webhook{
		Name:         existing.Name,
		TargetURL:    existing.TargetURL,
		Events:       existing.Events,
		BucketFilter: existing.BucketFilter,
		PrefixFilter: existing.PrefixFilter,
		// Secret="" preserves the existing one (per store.Update contract).
		Enabled: enable,
	}
	updated, err := s.webhooks.Update(r.Context(), existing.ID, patch)
	if err != nil {
		s.auditFailure(r, action, resourceWebhook(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "WEBHOOK_STORE_ERROR", err.Error())
		return
	}
	s.auditSuccess(r, action, resourceWebhook(updated.ID))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(redactWebhook(updated))
}
