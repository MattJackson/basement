// Package webhook implements user-owned bucket-event webhook
// subscriptions (v1.7.0d).
//
// Each Webhook is an operator-declared "POST to this URL when an
// object is created / modified / deleted in bucket X" rule. The engine
// fans out per-webhook delivery with HMAC-signed bodies, retry +
// auto-disable on chronic failure, and audit logging on every fire.
//
// Scope of v1.7.0d:
//
//   - Types + JSON store + delivery engine + user-tier CRUD API.
//   - Emission sites: synthetic via /test endpoint and DELETE handlers
//     for user-region objects (the only server-side mutation path
//     basement still observes; PUTs go through presigned URLs that
//     bypass us). Real coverage for every backend mutation lands with
//     the v2.0 gateway.
//   - Federation upgrade from polling → event-driven lands in v1.7.0f
//     and is intentionally not wired here.
package webhook

import "time"

// EventType is the kind of bucket event the operator can subscribe to.
// Kept as a string so JSON marshalling stays human-readable in the
// webhooks.json file the operator may need to grep on disk.
type EventType string

const (
	// EventObjectCreated fires once an object has been confirmed
	// uploaded to a bucket. Reserved for the v2.0 gateway path —
	// v1.7.0d only emits via the explicit /test endpoint because
	// presigned-URL PUTs bypass basement entirely.
	EventObjectCreated EventType = "object.created"
	// EventObjectDeleted fires after a successful object delete.
	// Wired through the user-region delete handler in v1.7.0d, so
	// this is the one event type the engine sees from real traffic
	// before v2.0.
	EventObjectDeleted EventType = "object.deleted"
	// EventObjectModified fires on overwrite. Same gateway-only
	// caveat as EventObjectCreated for v1.7.0d.
	EventObjectModified EventType = "object.modified"
)

// KnownEventTypes is the full enumeration of legal EventType values.
// Used by validation to reject typos before they reach the engine.
var KnownEventTypes = []EventType{
	EventObjectCreated,
	EventObjectDeleted,
	EventObjectModified,
}

// IsKnownEvent reports whether t is one of the enumerated EventType
// constants. Used by both validation and the engine's filter check.
func IsKnownEvent(t EventType) bool {
	for _, k := range KnownEventTypes {
		if k == t {
			return true
		}
	}
	return false
}

// BucketFilter narrows a Webhook to a specific (region, bucket) pair.
// Empty Bucket with a non-empty RegionID matches every bucket in that
// region (operator opt-in firehose for a single backend). Both empty
// means the webhook matches every event the engine sees — that's the
// default when no filter block is supplied.
type BucketFilter struct {
	RegionID string `json:"regionId"`
	Bucket   string `json:"bucket"`
}

// DeliveryResult is the outcome of one delivery attempt (or the last
// retry, in the engine's retry loop). Stored on Webhook.LastDelivery
// so the operator can see "this fired 30s ago, got a 200, took 84ms"
// in the detail page without scanning the audit log.
type DeliveryResult struct {
	DeliveredAt time.Time `json:"deliveredAt"`
	HTTPStatus  int       `json:"httpStatus"`
	DurationMs  int       `json:"durationMs"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
}

// Webhook is one operator-defined subscription. Owned by a single
// user — webhooks are first-class user-property the same way
// backups and federations are.
//
// Secret is the shared key used to HMAC-SHA256 the request body
// before delivery. Operators either supply their own (16+ chars) or
// let the engine generate one on create; thereafter Secret is
// returned only on the initial mint response and is redacted from
// every List / Get / Update payload by the API layer (same mint-only
// pattern as service-account secrets in v1.7.0a).
//
// JSON tags use camelCase to match the rest of the v1.x user API
// surface.
type Webhook struct {
	ID            string          `json:"id"`
	OwnerUserID   string          `json:"ownerUserId"`
	Name          string          `json:"name"`
	TargetURL     string          `json:"targetUrl"`
	Events        []EventType     `json:"events"`
	BucketFilter  *BucketFilter   `json:"bucketFilter,omitempty"`
	PrefixFilter  string          `json:"prefixFilter,omitempty"`
	Secret        string          `json:"secret,omitempty"`
	Enabled       bool            `json:"enabled"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	LastDelivery  *DeliveryResult `json:"lastDelivery,omitempty"`
	FailureCount  int             `json:"failureCount,omitempty"`
}

// EventEnvelope is the engine's internal representation of one bucket
// event, before any per-webhook filtering. Wire-equivalent shape
// (camelCase JSON) so the engine can serialise it directly into the
// outbound POST body.
//
// ID is per-event (UUID assigned by Emit) so a receiver can idempotency-
// guard on it across retries. OccurredAt is when basement observed
// the event; the underlying backend's own timestamp is not threaded
// through because most drivers don't surface it.
type EventEnvelope struct {
	ID         string    `json:"id"`
	Type       EventType `json:"type"`
	OccurredAt time.Time `json:"occurredAt"`
	RegionID   string    `json:"regionId"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	Size       int64     `json:"size,omitempty"`
	ETag       string    `json:"etag,omitempty"`
}

// Matches reports whether the webhook should fire for this envelope.
// Pure function over the webhook's filter fields:
//
//   - Disabled webhooks never match.
//   - Events: the envelope's Type must appear in the webhook's
//     Events list.
//   - BucketFilter: if set, RegionID must match exactly. Bucket
//     matches when the filter's Bucket is empty (firehose for the
//     region) or equals the envelope's Bucket exactly.
//   - PrefixFilter: if non-empty, the envelope's Key must start
//     with it.
//
// Used by the engine and exposed for tests so the filter contract
// stays in one place.
func (w Webhook) Matches(e EventEnvelope) bool {
	if !w.Enabled {
		return false
	}
	if !eventInList(e.Type, w.Events) {
		return false
	}
	if w.BucketFilter != nil {
		if w.BucketFilter.RegionID != "" && w.BucketFilter.RegionID != e.RegionID {
			return false
		}
		if w.BucketFilter.Bucket != "" && w.BucketFilter.Bucket != e.Bucket {
			return false
		}
	}
	if w.PrefixFilter != "" {
		if len(e.Key) < len(w.PrefixFilter) || e.Key[:len(w.PrefixFilter)] != w.PrefixFilter {
			return false
		}
	}
	return true
}

// eventInList reports whether t appears in the list. Linear scan —
// Events lists are tiny (at most 3 entries in v1.7.0d).
func eventInList(t EventType, list []EventType) bool {
	for _, e := range list {
		if e == t {
			return true
		}
	}
	return false
}

// AutoDisableThreshold is the consecutive-failure count after which
// the engine flips Enabled=false and emits webhook:auto_disabled to
// the audit log. Picked to be small enough that a misconfigured target
// stops drowning audit, large enough that a transient outage (DNS
// flap, target restart) doesn't auto-disable a healthy webhook.
const AutoDisableThreshold = 10

// SignatureHeader is the HTTP header that carries the body's
// HMAC-SHA256 signature on every outbound delivery. Format:
// "sha256=<hex>" so a recipient that supports multiple hash
// algorithms can route on the prefix.
const SignatureHeader = "X-Basement-Signature"
