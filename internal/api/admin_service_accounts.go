// Package api: admin endpoints for the v1.7.0a service-account store.
//
// Six routes under /api/v1/admin/service-accounts, all gated by
// host:manage_users (the SA is a powerful credential — it can
// authenticate INTO basement carrying whatever capabilities the
// minter selected, so handing one out has the same trust gravity as
// creating a host admin user). Per the cycle prompt:
//
//   POST   /admin/service-accounts          — mint, returns plaintext ONCE
//   GET    /admin/service-accounts          — list SAs the caller owns
//   GET    /admin/service-accounts/{id}     — detail (no secret)
//   PUT    /admin/service-accounts/{id}     — update name/caps/expiry (NOT secret)
//   DELETE /admin/service-accounts/{id}     — soft-delete (RevokedAt)
//   POST   /admin/service-accounts/{id}/rotate — new secret returned ONCE
//
// Cross-user GETs collapse to 404 — basement doesn't leak
// "this ID exists but isn't yours" through the wire shape. Audit
// events fire on every mutation per the cycle's audit table.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/serviceaccount"
)

// serviceAccountPublic is the wire-side shape of a ServiceAccount. The
// bcrypt hash never leaves the server. Plaintext rides as a separate
// top-level field on Create + Rotate responses (see
// serviceAccountWithSecret) and never appears on read paths.
type serviceAccountPublic struct {
	ID           string                       `json:"id"`
	OwnerUserID  string                       `json:"ownerUserId"`
	Name         string                       `json:"name"`
	AccessKeyID  string                       `json:"accessKeyId"`
	Capabilities []serviceAccountCapabilityIO `json:"capabilities"`
	Scopes       []string                     `json:"scopes"`
	CreatedAt    time.Time                    `json:"createdAt"`
	ExpiresAt    *time.Time                   `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time                   `json:"lastUsedAt,omitempty"`
	RevokedAt    *time.Time                   `json:"revokedAt,omitempty"`
}

// serviceAccountCapabilityIO mirrors serviceaccount.Capability on the
// wire; named separately so the JSON tag layout is independent of any
// future package-internal refactor.
type serviceAccountCapabilityIO struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

// serviceAccountWithSecret is the response shape for Create + Rotate.
// The Secret field is the plaintext — present exactly once per
// credential lifecycle, never re-derivable. The doc comment on the
// struct is the canonical "warn-the-caller" surface for the FE
// contract.
type serviceAccountWithSecret struct {
	ServiceAccount serviceAccountPublic `json:"serviceAccount"`
	// Secret is the plaintext — returned exactly once on mint or
	// rotate. The server never echoes it back on any subsequent call.
	// FE must surface it to the operator immediately + drop the value
	// from memory once they've copied it.
	Secret string `json:"secret"`
}

func toServiceAccountPublic(sa serviceaccount.ServiceAccount) serviceAccountPublic {
	caps := make([]serviceAccountCapabilityIO, 0, len(sa.Capabilities))
	for _, c := range sa.Capabilities {
		caps = append(caps, serviceAccountCapabilityIO{ID: c.ID, Scope: c.Scope})
	}
	scopes := make([]string, 0, len(sa.Scopes))
	scopes = append(scopes, sa.Scopes...)
	return serviceAccountPublic{
		ID:           sa.ID,
		OwnerUserID:  sa.OwnerUserID,
		Name:         sa.Name,
		AccessKeyID:  sa.AccessKeyID,
		Capabilities: caps,
		Scopes:       scopes,
		CreatedAt:    sa.CreatedAt,
		ExpiresAt:    sa.ExpiresAt,
		LastUsedAt:   sa.LastUsedAt,
		RevokedAt:    sa.RevokedAt,
	}
}

// createServiceAccountRequest is the body for POST
// /admin/service-accounts. The plaintext secret is server-generated;
// the caller picks Name + the capability bundle and (optionally) an
// expiry.
type createServiceAccountRequest struct {
	Name         string                       `json:"name"`
	Capabilities []serviceAccountCapabilityIO `json:"capabilities"`
	Scopes       []string                     `json:"scopes"`
	ExpiresAt    *time.Time                   `json:"expiresAt,omitempty"`
}

// updateServiceAccountRequest is the body for PUT
// /admin/service-accounts/{id}. The fields are all optional — only
// non-empty fields apply. Secret rotation is a separate endpoint so
// PUT is a pure metadata mutator + can never accidentally surface
// plaintext.
type updateServiceAccountRequest struct {
	Name         string                       `json:"name,omitempty"`
	Capabilities []serviceAccountCapabilityIO `json:"capabilities,omitempty"`
	Scopes       []string                     `json:"scopes,omitempty"`
	ExpiresAt    *time.Time                   `json:"expiresAt,omitempty"`
}

// resourceServiceAccount is the canonical audit resource for SA
// events. {id} only — the SA name + access key live alongside in the
// SA record, and audit consumers join on resource ID.
func resourceServiceAccount(id string) string { return "service_account:" + id }

// validateScope rejects free-form scope strings that don't fit the
// policy scope grammar. The same six forms used by RoleAssignment
// (see internal/auth/policy/types.go) are accepted; everything else
// returns an error the handler surfaces as 400 INVALID_SCOPE.
//
// Forms:
//
//	host:*
//	cluster:*               cluster:{cid}
//	bucket:{cid}:*          bucket:{cid}:{bid}
//	key:{cid}:*             key:{cid}:{kid}
//
// The empty string is rejected — an SA grant with no scope is almost
// certainly a UI bug, and "the empty scope" doesn't satisfy any gate
// either.
func validateServiceAccountScope(scope string) error {
	if scope == "" {
		return errors.New("scope is required")
	}
	parts := strings.Split(scope, ":")
	switch parts[0] {
	case "host":
		if len(parts) != 2 || parts[1] != "*" {
			return errors.New("host scope must be host:*")
		}
		return nil
	case "cluster":
		if len(parts) != 2 {
			return errors.New("cluster scope must be cluster:* or cluster:{cid}")
		}
		// cluster:* or cluster:{cid} (must be non-empty)
		if parts[1] == "" {
			return errors.New("cluster scope must be cluster:* or cluster:{cid}")
		}
		return nil
	case "bucket", "key":
		if len(parts) != 3 {
			return errors.New(parts[0] + " scope must be " + parts[0] + ":{cid}:* or " + parts[0] + ":{cid}:{id}")
		}
		if parts[1] == "" || parts[2] == "" {
			return errors.New(parts[0] + " scope segments must be non-empty")
		}
		return nil
	default:
		return errors.New("unknown scope domain: " + parts[0])
	}
}

// validateServiceAccountCapabilities returns an error if any
// capability ID isn't in the policy registry, or if any scope fails
// the grammar check.
func validateServiceAccountCapabilities(caps []serviceAccountCapabilityIO) error {
	for _, c := range caps {
		if err := policy.Validate(c.ID); err != nil {
			return err
		}
		if err := validateServiceAccountScope(c.Scope); err != nil {
			return err
		}
	}
	return nil
}

// validateServiceAccountScopes runs the grammar check across the
// top-level Scopes list (used for SA-wide scope hints — distinct from
// the per-capability scope on the Capabilities list).
func validateServiceAccountScopes(scopes []string) error {
	for _, s := range scopes {
		if err := validateServiceAccountScope(s); err != nil {
			return err
		}
	}
	return nil
}

// createServiceAccountHandler — POST /api/v1/admin/service-accounts.
// Returns 201 + the plaintext secret on the first call; the caller
// stores the secret and is responsible for handing it to the
// downstream client. No subsequent read path exposes the plaintext.
func (s *Server) createServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	userID, ok := s.requireCapability(w, r, "host:manage_users", "host:*")
	if !ok {
		return
	}
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SERVICE_ACCOUNTS_NOT_WIRED",
			"Service-account store is not configured on this deployment.")
		return
	}

	var req createServiceAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	if err := validateServiceAccountCapabilities(req.Capabilities); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_CAPABILITY", err.Error())
		return
	}
	if err := validateServiceAccountScopes(req.Scopes); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_SCOPE", err.Error())
		return
	}

	caps := make([]serviceaccount.Capability, 0, len(req.Capabilities))
	for _, c := range req.Capabilities {
		caps = append(caps, serviceaccount.Capability{ID: c.ID, Scope: c.Scope})
	}

	sa, secret, err := s.store.ServiceAccounts().Create(r.Context(), serviceaccount.ServiceAccount{
		OwnerUserID:  userID,
		Name:         req.Name,
		Capabilities: caps,
		Scopes:       req.Scopes,
		ExpiresAt:    req.ExpiresAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, serviceaccount.ErrDuplicateName):
			s.auditFailure(r, "service_account:create", "service_account:"+req.Name, err)
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_NAME",
				"A service account with that name already exists for this owner.")
			return
		case errors.Is(err, serviceaccount.ErrInvalidName):
			s.auditFailure(r, "service_account:create", "service_account:"+req.Name, err)
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_NAME",
				"Name must match ^[A-Za-z0-9_-]{3,64}$.")
			return
		default:
			s.auditFailure(r, "service_account:create", "service_account:"+req.Name, err)
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
	}

	s.auditSuccess(r, "service_account:create", resourceServiceAccount(sa.ID))

	writeJSON(w, http.StatusCreated, serviceAccountWithSecret{
		ServiceAccount: toServiceAccountPublic(sa),
		Secret:         secret,
	})
}

// listServiceAccountsHandler — GET /api/v1/admin/service-accounts.
// Returns every SA owned by the calling user, including revoked ones
// (the FE renders a "Revoked" pill so they show up in audit-like
// flows). Cross-user listing is not supported.
func (s *Server) listServiceAccountsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	userID, ok := s.requireCapability(w, r, "host:manage_users", "host:*")
	if !ok {
		return
	}
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeJSON(w, http.StatusOK, []serviceAccountPublic{})
		return
	}

	rows, err := s.store.ServiceAccounts().ListForUser(r.Context(), userID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list service accounts")
		return
	}
	out := make([]serviceAccountPublic, 0, len(rows))
	for _, sa := range rows {
		out = append(out, toServiceAccountPublic(sa))
	}
	writeJSON(w, http.StatusOK, out)
}

// getServiceAccountHandler — GET /api/v1/admin/service-accounts/{id}.
// Cross-user access collapses to 404 so the wire shape doesn't leak
// IDs across tenants. Same 404 for genuinely-missing rows.
func (s *Server) getServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	userID, ok := s.requireCapability(w, r, "host:manage_users", "host:*")
	if !ok {
		return
	}
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SERVICE_ACCOUNTS_NOT_WIRED",
			"Service-account store is not configured on this deployment.")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "service account id required")
		return
	}

	sa, err := s.store.ServiceAccounts().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, serviceaccount.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to fetch service account")
		return
	}
	if sa.OwnerUserID != userID {
		// Per the cycle prompt: cross-user GET collapses to 404 so we
		// don't leak the existence of IDs the caller can't reach.
		writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
		return
	}

	writeJSON(w, http.StatusOK, toServiceAccountPublic(sa))
}

// updateServiceAccountHandler — PUT /api/v1/admin/service-accounts/{id}.
// Mutates Name / Capabilities / Scopes / ExpiresAt. Secret rotation
// goes through /rotate; PUT can never touch the secret. Cross-user
// updates collapse to 404 (same wire shape as GET).
func (s *Server) updateServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	userID, ok := s.requireCapability(w, r, "host:manage_users", "host:*")
	if !ok {
		return
	}
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SERVICE_ACCOUNTS_NOT_WIRED",
			"Service-account store is not configured on this deployment.")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "service account id required")
		return
	}

	// Ownership check first, then mutate. Two reads where one might
	// do — but the second read is the store mutation, and an extra
	// O(1) Get is the right price for guaranteeing the cross-user
	// 404 wire-shape behaviour.
	existing, err := s.store.ServiceAccounts().Get(r.Context(), id)
	if err != nil || existing.OwnerUserID != userID {
		writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
		return
	}

	var req updateServiceAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	if req.Capabilities != nil {
		if err := validateServiceAccountCapabilities(req.Capabilities); err != nil {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_CAPABILITY", err.Error())
			return
		}
	}
	if req.Scopes != nil {
		if err := validateServiceAccountScopes(req.Scopes); err != nil {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_SCOPE", err.Error())
			return
		}
	}

	patch := serviceaccount.ServiceAccount{
		Name:      req.Name,
		Scopes:    req.Scopes,
		ExpiresAt: req.ExpiresAt,
	}
	if req.Capabilities != nil {
		caps := make([]serviceaccount.Capability, 0, len(req.Capabilities))
		for _, c := range req.Capabilities {
			caps = append(caps, serviceaccount.Capability{ID: c.ID, Scope: c.Scope})
		}
		patch.Capabilities = caps
	}

	sa, err := s.store.ServiceAccounts().Update(r.Context(), id, patch)
	if err != nil {
		switch {
		case errors.Is(err, serviceaccount.ErrNotFound):
			writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
			return
		case errors.Is(err, serviceaccount.ErrDuplicateName):
			s.auditFailure(r, "service_account:update", resourceServiceAccount(id), err)
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_NAME",
				"A service account with that name already exists for this owner.")
			return
		case errors.Is(err, serviceaccount.ErrInvalidName):
			s.auditFailure(r, "service_account:update", resourceServiceAccount(id), err)
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_NAME",
				"Name must match ^[A-Za-z0-9_-]{3,64}$.")
			return
		default:
			s.auditFailure(r, "service_account:update", resourceServiceAccount(id), err)
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
	}

	s.auditSuccess(r, "service_account:update", resourceServiceAccount(id))
	writeJSON(w, http.StatusOK, toServiceAccountPublic(sa))
}

// deleteServiceAccountHandler — DELETE
// /api/v1/admin/service-accounts/{id}. Soft-delete: RevokedAt is set,
// VerifySecret starts returning false immediately, the row stays on
// disk for audit forensics. Cross-user delete collapses to 404.
func (s *Server) deleteServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	userID, ok := s.requireCapability(w, r, "host:manage_users", "host:*")
	if !ok {
		return
	}
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SERVICE_ACCOUNTS_NOT_WIRED",
			"Service-account store is not configured on this deployment.")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "service account id required")
		return
	}

	existing, err := s.store.ServiceAccounts().Get(r.Context(), id)
	if err != nil || existing.OwnerUserID != userID {
		writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
		return
	}

	if err := s.store.ServiceAccounts().Delete(r.Context(), id); err != nil {
		if errors.Is(err, serviceaccount.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
			return
		}
		s.auditFailure(r, "service_account:delete", resourceServiceAccount(id), err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete service account")
		return
	}

	s.auditSuccess(r, "service_account:delete", resourceServiceAccount(id))
	w.WriteHeader(http.StatusNoContent)
}

// rotateServiceAccountHandler — POST
// /api/v1/admin/service-accounts/{id}/rotate. Replaces the secret
// (bcrypt hash on disk + a fresh plaintext in the response). The
// AccessKeyID is preserved so any client config keyed off the
// access-key keeps resolving — only the secret changes, mirroring
// the AWS IAM rotation model.
func (s *Server) rotateServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	userID, ok := s.requireCapability(w, r, "host:manage_users", "host:*")
	if !ok {
		return
	}
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SERVICE_ACCOUNTS_NOT_WIRED",
			"Service-account store is not configured on this deployment.")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "service account id required")
		return
	}

	existing, err := s.store.ServiceAccounts().Get(r.Context(), id)
	if err != nil || existing.OwnerUserID != userID {
		writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
		return
	}

	sa, secret, err := s.store.ServiceAccounts().Rotate(r.Context(), id)
	if err != nil {
		if errors.Is(err, serviceaccount.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", "Service account not found")
			return
		}
		s.auditFailure(r, "service_account:rotate", resourceServiceAccount(id), err)
		writeErrorSimple(w, http.StatusConflict, "ROTATE_FAILED", err.Error())
		return
	}

	s.auditSuccess(r, "service_account:rotate", resourceServiceAccount(id))
	writeJSON(w, http.StatusOK, serviceAccountWithSecret{
		ServiceAccount: toServiceAccountPublic(sa),
		Secret:         secret,
	})
}
