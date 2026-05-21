package garage

import (
	"context"
	"fmt"
	"net/url"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// LifecycleSupport reports Garage v2's lifecycle capabilities. Garage
// v2's admin API surfaces lifecycle via UpdateBucket.lifecycleRules
// (garage-admin-v2.json:4546-4587 + schema lifecycle.Rule lines
// 4962-5001), but the rule shape is intentionally narrower than
// AWS / MinIO:
//
//   - Expiration is supported (`Expiration.Days`)
//   - Aborting incomplete multipart uploads is supported
//     (`AbortIncompleteMultipartUpload.DaysAfterInitiation`)
//   - Filtering by Prefix is supported via `Filter.Prefix`
//   - Transitions to other storage tiers are NOT supported — Garage
//     has no tiering concept.
//   - Noncurrent-version expiration is NOT exposed by the admin API
//     (Garage's versioning story is also evolving), so we don't claim
//     it here.
//
// The UI gates each field by these flags rather than driver-name
// checks (per `feedback_generic_driver_middleman` doctrine).
func (d *driver) LifecycleSupport() driverpkg.LifecycleCapabilities {
	return driverpkg.LifecycleCapabilities{
		Supported:          true,
		Expiration:         true,
		Transition:         false,
		TransitionTiers:    []string{},
		NoncurrentDays:     false,
		AbortMultipartDays: true,
	}
}

// GetLifecycle returns the bucket's current lifecycle rules by reading
// the lifecycleRules field on the GetBucketInfo response. Garage's
// response shape is whatever was last PUT through UpdateBucket; we
// flatten it back into the driver's LifecycleRule shape.
func (d *driver) GetLifecycle(ctx context.Context, bucketID string) ([]driverpkg.LifecycleRule, error) {
	path := fmt.Sprintf("/v2/GetBucketInfo?id=%s", url.QueryEscape(bucketID))

	var resp lifecycleAwareBucketInfo
	if err := d.client.do(ctx, "GET", path, nil, &resp); err != nil {
		return nil, err
	}

	rules := make([]driverpkg.LifecycleRule, 0, len(resp.LifecycleRules))
	for _, gr := range resp.LifecycleRules {
		rules = append(rules, garageRuleToDriver(gr))
	}
	return rules, nil
}

// PutLifecycle replaces the bucket's lifecycle policy by issuing an
// UpdateBucket with the lifecycleRules field set. An empty rules slice
// clears the policy — Garage accepts an empty array for
// `lifecycleRules` to mean "no rules".
//
// Note: the v2 UpdateBucket endpoint REPLACES every field that's
// present in the body (and only the present fields), but lifecycleRules
// is a single field, so this PUT doesn't disturb quotas / website /
// cors. The wire shape uses the same updateBucketRequestBody struct
// the existing UpdateBucket path uses, with the lifecycleRules slot
// freshly populated.
func (d *driver) PutLifecycle(ctx context.Context, bucketID string, rules []driverpkg.LifecycleRule) error {
	garageRules := make([]garageLifecycleRule, 0, len(rules))
	for _, r := range rules {
		garageRules = append(garageRules, driverRuleToGarage(r))
	}

	body := lifecycleUpdateBucketRequest{
		LifecycleRules: garageRules,
	}

	path := fmt.Sprintf("/v2/UpdateBucket?id=%s", url.QueryEscape(bucketID))

	// We discard the response body — UpdateBucket returns the bucket
	// info but the UI re-fetches via GetLifecycle on success so we
	// don't need to round-trip the parsed rules here.
	if err := d.client.do(ctx, "POST", path, body, nil); err != nil {
		return err
	}
	return nil
}

// garageRuleToDriver flattens a Garage lifecycle rule into our shape.
// Transition / NoncurrentDays slots are always nil here — Garage's
// admin API doesn't carry those fields.
func garageRuleToDriver(gr garageLifecycleRule) driverpkg.LifecycleRule {
	out := driverpkg.LifecycleRule{
		ID:     gr.ID,
		Status: gr.Status,
	}
	if gr.Filter != nil {
		out.Prefix = gr.Filter.Prefix
	}
	if gr.Expiration != nil && gr.Expiration.Days != nil {
		v := int(*gr.Expiration.Days)
		out.ExpirationDays = &v
	}
	if gr.AbortIncompleteMultipartUpload != nil {
		v := int(gr.AbortIncompleteMultipartUpload.DaysAfterInitiation)
		out.AbortMultipartDays = &v
	}
	return out
}

// driverRuleToGarage inflates a flat driver rule into Garage's wire
// shape. We drop fields Garage doesn't support (TransitionDays,
// TransitionTier, NoncurrentDays) silently — the UI gates them out via
// LifecycleSupport, so anything that reaches here either matches the
// supported subset or was sent by a direct API caller who chose to
// pass unsupported fields. Silently dropping is consistent with the
// rest of the Garage driver's "ignore unsupported PATCH fields" stance.
func driverRuleToGarage(r driverpkg.LifecycleRule) garageLifecycleRule {
	out := garageLifecycleRule{
		ID:     r.ID,
		Status: r.Status,
		Filter: &garageLifecycleFilter{Prefix: r.Prefix},
	}
	if r.ExpirationDays != nil {
		d := int64(*r.ExpirationDays)
		out.Expiration = &garageLifecycleExpiration{Days: &d}
	}
	if r.AbortMultipartDays != nil {
		out.AbortIncompleteMultipartUpload = &garageLifecycleAbortMpu{
			DaysAfterInitiation: int64(*r.AbortMultipartDays),
		}
	}
	return out
}

// ===== Garage v2 lifecycle wire types =====
//
// Mirrors garage-admin-v2.json schemas lifecycle.Rule (4962-5001),
// lifecycle.Expiration (4910-4925), lifecycle.AbortIncompleteMpu
// (4899-4909), lifecycle.Filter (4926-4961). Garage uses XML-cased
// JSON field names ("ID", "Status", "Days", "Prefix") so we tag
// accordingly.

// lifecycleAwareBucketInfo is a narrow GetBucketInfo response that
// only deserializes the fields we care about for lifecycle reads —
// the existing getBucketInfoResponse type doesn't include
// lifecycleRules. We keep this struct separate so quota / object
// stats reads continue through the original path.
type lifecycleAwareBucketInfo struct {
	LifecycleRules []garageLifecycleRule `json:"lifecycleRules"`
}

// lifecycleUpdateBucketRequest is the narrow UpdateBucket request
// body for lifecycle-only updates. Built separately from
// updateBucketRequestBody so that adding lifecycle to the bucket
// update path didn't risk wire breakage on the existing quota PATCH.
type lifecycleUpdateBucketRequest struct {
	LifecycleRules []garageLifecycleRule `json:"lifecycleRules"`
}

type garageLifecycleRule struct {
	ID                             string                     `json:"ID"`
	Status                         string                     `json:"Status"`
	Filter                         *garageLifecycleFilter     `json:"Filter,omitempty"`
	Expiration                     *garageLifecycleExpiration `json:"Expiration,omitempty"`
	AbortIncompleteMultipartUpload *garageLifecycleAbortMpu   `json:"AbortIncompleteMultipartUpload,omitempty"`
}

type garageLifecycleFilter struct {
	Prefix string `json:"Prefix"`
}

type garageLifecycleExpiration struct {
	Days *int64 `json:"Days,omitempty"`
}

type garageLifecycleAbortMpu struct {
	DaysAfterInitiation int64 `json:"DaysAfterInitiation"`
}
