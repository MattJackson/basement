// Package api — bucket lifecycle handlers (v0.9.0i LIFECYCLE.WIZARD).
//
// Surfaces two endpoints under uiAdminG:
//
//	GET    /api/v1/admin/clusters/{cid}/buckets/{bid}/lifecycle
//	       → {rules: [...], capabilities: LifecycleCapabilities}
//	PUT    /api/v1/admin/clusters/{cid}/buckets/{bid}/lifecycle
//	       → body {rules: [...]}
//
// The handlers go through the connection-scoped Registry (s.reg.For)
// to honour the multi-cluster pivot. Capability gating: bucket:view
// for the read, bucket:edit_alias for the write — re-using existing
// capabilities per the cycle prompt rather than minting a fresh
// bucket:edit_lifecycle that nobody's role currently carries.
//
// The combined GET response shape (rules + capabilities together)
// saves the UI a second round-trip — the bucket-detail page already
// loads the bucket itself + cluster info; folding lifecycle's two
// queries into one keeps the per-page request count flat.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/driver"
)

// lifecycleGetResponse is the GET shape — rules + the driver's
// per-call capability snapshot. Capabilities are part of the same
// payload so the UI can render disabled fields without a second
// request to /capabilities — and so the gating reflects THIS
// cluster's driver, not whichever driver the legacy global was.
type lifecycleGetResponse struct {
	Rules        []driver.LifecycleRule      `json:"rules"`
	Capabilities driver.LifecycleCapabilities `json:"capabilities"`
}

// lifecyclePutRequest is the PUT body. An empty Rules slice CLEARS
// the policy (drivers translate that to DeleteBucketLifecycle on S3
// and an empty-array UpdateBucket on Garage).
type lifecyclePutRequest struct {
	Rules []driver.LifecycleRule `json:"rules"`
}

// getBucketLifecycleHandler handles GET on the lifecycle path. Per
// the cycle prompt: gated on bucket:view (the cheapest read-side
// capability — anyone allowed to see the bucket should be allowed
// to see its rules).
func (s *Server) getBucketLifecycleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	bid := chi.URLParam(r, "bid")
	if cid == "" || bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id and bucket id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "bucket:view", scopeBucket(cid, bid)); !ok {
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	caps := drv.LifecycleSupport()

	resp := lifecycleGetResponse{
		Rules:        []driver.LifecycleRule{},
		Capabilities: caps,
	}

	if !caps.Supported {
		// Don't even attempt GetLifecycle on unsupported drivers —
		// they all return ErrUnsupported, and the UI's "not supported"
		// branch only cares about the capability flag. Return 200
		// with empty rules so the screen always renders.
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rules, err := drv.GetLifecycle(r.Context(), bid)
	if err != nil {
		// On a supported driver that nevertheless returns
		// ErrUnsupported (e.g. an SDK version mismatch), surface as
		// 200 with an empty-rules + capabilities payload — the wizard
		// will be inert but the screen renders. For real errors
		// (404, 403, 500) we use the standard driver-error mapping.
		var de *driver.Error
		if errors.As(err, &de) && errors.Is(err, driver.ErrUnsupported) {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		writeDriverError(w, "GetLifecycle", err)
		return
	}
	if rules == nil {
		rules = []driver.LifecycleRule{}
	}
	resp.Rules = rules
	writeJSON(w, http.StatusOK, resp)
}

// putBucketLifecycleHandler handles PUT on the lifecycle path. Per
// the cycle prompt: gated on bucket:edit_alias — re-using an existing
// bucket-mutation capability rather than minting bucket:edit_lifecycle
// for v0.9.0i. If a future cycle adds finer mutation capabilities
// the gate here moves to bucket:edit_lifecycle.
func (s *Server) putBucketLifecycleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	cid := chi.URLParam(r, "cid")
	bid := chi.URLParam(r, "bid")
	if cid == "" || bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id and bucket id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "bucket:edit_alias", scopeBucket(cid, bid)); !ok {
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	caps := drv.LifecycleSupport()
	if !caps.Supported {
		// Refuse the write loudly — a 409 conveys "this is a state
		// problem, not a request problem" better than 501. The UI's
		// edit screen is gated on caps.Supported so reaching here
		// implies either a stale UI or a direct API caller.
		writeErrorSimple(w, http.StatusConflict, "LIFECYCLE_UNSUPPORTED",
			"This driver does not support lifecycle policies.")
		return
	}

	var req lifecyclePutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	// Validate each rule against the driver's capability flags and
	// the documented status enum. This is the only place that
	// enforces the rule grammar (drivers themselves trust the API
	// layer to have validated), so the checks here are deliberately
	// belt-and-braces.
	for i := range req.Rules {
		// Normalize whitespace on the user-typed strings.
		req.Rules[i].ID = strings.TrimSpace(req.Rules[i].ID)
		req.Rules[i].Status = strings.TrimSpace(req.Rules[i].Status)
		req.Rules[i].Prefix = strings.TrimSpace(req.Rules[i].Prefix)
		req.Rules[i].TransitionTier = strings.TrimSpace(req.Rules[i].TransitionTier)

		if req.Rules[i].Status != "Enabled" && req.Rules[i].Status != "Disabled" {
			writeError(w, http.StatusBadRequest, "INVALID_RULE_STATUS",
				`rule.status must be "Enabled" or "Disabled"`,
				map[string]any{"index": i})
			return
		}
		if req.Rules[i].TransitionDays != nil && !caps.Transition {
			writeError(w, http.StatusBadRequest, "TRANSITION_UNSUPPORTED",
				"This driver does not support tier transitions.",
				map[string]any{"index": i})
			return
		}
		if req.Rules[i].NoncurrentDays != nil && !caps.NoncurrentDays {
			writeError(w, http.StatusBadRequest, "NONCURRENT_UNSUPPORTED",
				"This driver does not support noncurrent-version expiration.",
				map[string]any{"index": i})
			return
		}
		if req.Rules[i].AbortMultipartDays != nil && !caps.AbortMultipartDays {
			writeError(w, http.StatusBadRequest, "ABORT_MPU_UNSUPPORTED",
				"This driver does not support aborting incomplete multipart uploads via lifecycle.",
				map[string]any{"index": i})
			return
		}
		if req.Rules[i].ExpirationDays != nil && !caps.Expiration {
			writeError(w, http.StatusBadRequest, "EXPIRATION_UNSUPPORTED",
				"This driver does not support object expiration.",
				map[string]any{"index": i})
			return
		}
		// Tier validation: if a tier was specified, it must be in the
		// driver's allow-list. This blocks typos before the SDK sees
		// them — operators get a clean 400 instead of an opaque S3
		// XML error.
		if req.Rules[i].TransitionTier != "" {
			tierOK := false
			for _, t := range caps.TransitionTiers {
				if t == req.Rules[i].TransitionTier {
					tierOK = true
					break
				}
			}
			if !tierOK {
				writeError(w, http.StatusBadRequest, "TRANSITION_TIER_INVALID",
					"transitionTier is not in the driver's supported list",
					map[string]any{
						"index":      i,
						"tier":       req.Rules[i].TransitionTier,
						"validTiers": caps.TransitionTiers,
					})
				return
			}
		}
	}

	if err := drv.PutLifecycle(r.Context(), bid, req.Rules); err != nil {
		writeDriverError(w, "PutLifecycle", err)
		return
	}

	// Round-trip: GET so the client sees the persisted state. This
	// also confirms the write landed (some drivers eventually-consistent
	// the policy; the GET surfaces that).
	rules, err := drv.GetLifecycle(r.Context(), bid)
	if err != nil {
		// The PUT succeeded; if GET fails we still report success
		// with the rules-as-submitted. The UI re-fetches anyway.
		rules = req.Rules
	}
	if rules == nil {
		rules = []driver.LifecycleRule{}
	}

	writeJSON(w, http.StatusOK, lifecycleGetResponse{
		Rules:        rules,
		Capabilities: caps,
	})
}
