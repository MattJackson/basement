// Package gateway: Backend is the data-access interface every Gateway
// implementation calls. It wraps basement's existing primitives
// (Auth, UserRegions, ServiceAccounts, Driver Registry) behind a
// single contract so:
//
//   - protocol code never reaches into internal/driver or
//     internal/store directly,
//   - the gateway tests can pass a small mock instead of a full driver
//     registry + UserRegions store,
//   - adding a new gateway in v1.10+ doesn't require new wiring
//     between protocol code and storage code.
//
// The Backend interface intentionally mirrors S3 verbs (List, Head,
// Get, Put, Delete, Copy) for the data plane plus a thin auth surface
// covering the three credential shapes basement supports today
// (Basic, Bearer/BMNT, SigV4 — though SigV4 is reserved for the v2.0
// S3 gateway).

package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// Backend is the data-access contract. Concrete implementations live
// in this package (ProductionBackend) or in tests as small fakes.
type Backend interface {
	// --- Auth ---------------------------------------------------------

	// AuthBasic verifies a username/password pair. Recognises the
	// env-admin account and any persisted store.User with a non-empty
	// PasswordHash. OIDC-only accounts (no password) fail with
	// ErrUnauthenticated.
	AuthBasic(ctx context.Context, user, pass string) (*UserContext, error)

	// AuthBearer verifies a basement-issued service-account
	// credential pair presented in the "AKID:secret" BMNT-prefixed
	// shape. The colon-separated payload mirrors what /api/v1's
	// bearer middleware accepts. Returns ErrUnauthenticated when the
	// key is unknown, revoked, or expired.
	AuthBearer(ctx context.Context, akidSecret string) (*UserContext, error)

	// AuthSigV4 reserves the SigV4 verification slot for the v2.0 S3
	// gateway. v1.9.0c's production Backend returns ErrUnsupported.
	// Sketched in here so the interface doesn't shift later — the S3
	// gateway already implements the call shape it expects.
	AuthSigV4(ctx context.Context, signedRequest *http.Request) (*UserContext, error)

	// --- Data plane ---------------------------------------------------

	// ListRegions returns the caller's region keychain (ADR-0002).
	// For gateways that don't carry a region concept (a future
	// SMB share that bridges directly to one bucket) the entries
	// may be ignored — but the interface keeps the listing path
	// uniform across gateways so the surface tests don't fork.
	ListRegions(ctx context.Context, uctx *UserContext) ([]Region, error)

	// ListBuckets returns the buckets visible at the given region,
	// applying the Garage admin bridge when the region's endpoint
	// matches an admin Connection. Mirrors what the /webdav/{alias}
	// PROPFIND path does today.
	ListBuckets(ctx context.Context, uctx *UserContext, regionID string) ([]Bucket, error)

	// ListObjects walks a bucket with the supplied prefix +
	// delimiter, returning a single page. limit==0 means "driver
	// default", which is typically 1000.
	ListObjects(ctx context.Context, uctx *UserContext, regionID, bucket, prefix, delimiter, continuationToken string, limit int) (ObjectPage, error)

	// HeadObject returns metadata for one object. Returns
	// ErrNotFound when the key doesn't exist.
	HeadObject(ctx context.Context, uctx *UserContext, regionID, bucket, key string) (ObjectMeta, error)

	// GetObject streams the object body back. Caller MUST Close the
	// returned ReadCloser.
	GetObject(ctx context.Context, uctx *UserContext, regionID, bucket, key string) (io.ReadCloser, ObjectMeta, error)

	// PutObject uploads `size` bytes from `body` to (region, bucket,
	// key). When size is unknown (e.g. WebDAV PUT that buffers in
	// memory before the call) callers pass the exact buffer length —
	// drivers refuse streamed unknown-length uploads.
	PutObject(ctx context.Context, uctx *UserContext, regionID, bucket, key string, body io.Reader, size int64, contentType string) error

	// DeleteObject removes one object. Returns nil for a missing
	// key — most clients (Finder, rclone) tolerate idempotent
	// deletes and a "404 mapped to ok" matches S3 default semantics.
	DeleteObject(ctx context.Context, uctx *UserContext, regionID, bucket, key string) error

	// CopyObject performs a server-side copy. Same-region only in
	// v1.9.0c; cross-region copy would require a download/upload
	// bridge that the WebDAV refactor never needed and the spec
	// explicitly excludes.
	CopyObject(ctx context.Context, uctx *UserContext, srcRegionID, srcBucket, srcKey, dstRegionID, dstBucket, dstKey string) error

	// --- Bucket lifecycle --------------------------------------------
	//
	// CreateBucket / DeleteBucket are NOT in the spec's minimum
	// Backend shape, but the WebDAV gateway invokes them via MKCOL
	// at /{alias}/{bucket} and DELETE at /{alias}/{bucket}/ today.
	// Preserving that behaviour post-refactor (per the cycle's "must
	// continue working identically" constraint) is cheaper than
	// teaching WebDAV to refuse + asking the operator to drop into
	// the admin UI for what feels like a basic FS verb.

	// CreateBucket creates a bucket at the region. Returns
	// ErrConflict when the bucket already exists.
	CreateBucket(ctx context.Context, uctx *UserContext, regionID, bucket string) error

	// DeleteBucket drops an empty bucket. Most backends refuse
	// non-empty buckets; the driver's error surfaces unchanged.
	DeleteBucket(ctx context.Context, uctx *UserContext, regionID, bucket string) error
}

// UserContext is the resolved identity after AuthBasic / AuthBearer /
// AuthSigV4. Threaded into every data-plane call so per-user filtering
// (e.g. ListRegions returning only the user's regions) stays in the
// Backend impl instead of leaking into protocol code.
type UserContext struct {
	// UserID is the basement user the call is scoped to. For env-
	// admin Basic auth this is the admin username; for a store.User
	// it's the user's Username; for a service-account it's the SA's
	// OwnerUserID — so SA-authed requests share the owner's regions.
	UserID string

	// ServiceAccountID is non-empty when the call was authenticated
	// via a service account. Lets audit log lines distinguish "alice
	// directly" from "alice's ci-prod SA" without a second lookup.
	ServiceAccountID string

	// Capabilities is the policy capability list granted to this
	// caller. Empty for env-admin (everything) and for password-auth
	// (handled by the JWT path); populated for SA-authed calls so the
	// gateway can perform per-call enforcement when relevant. The
	// initial v1.9.0c data-plane impl does not consult this list
	// (delegates to the backend's own ACL); reserved for v1.10+
	// gateways that need finer-grained checks before dispatch.
	Capabilities []string
}

// Region is the gateway-shaped view of a store.UserRegion. We don't
// re-export the store type so gateway impls don't grow a transitive
// dep on internal/store — keeps the package boundary clean.
type Region struct {
	ID              string    `json:"id"`
	Alias           string    `json:"alias"`
	Endpoint        string    `json:"endpoint"`
	AccessKeyID     string    `json:"accessKeyId"`
	Region          string    `json:"region"`
	AddressingStyle string    `json:"addressingStyle,omitempty"`
	CreatedAt       time.Time `json:"createdAt,omitempty"`
}

// Bucket mirrors driver.Bucket but lives in this package so gateway
// impls don't need to import internal/driver.
type Bucket struct {
	ID        string    `json:"id"`
	Aliases   []string  `json:"aliases,omitempty"`
	Created   time.Time `json:"created,omitempty"`
	Objects   int64     `json:"objects,omitempty"`
	Bytes     int64     `json:"bytes,omitempty"`
}

// ObjectMeta is the gateway-shaped object metadata.
type ObjectMeta struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ContentType  string    `json:"contentType,omitempty"`
}

// ObjectPage is one page of a ListObjects call. Mirrors
// driver.ObjectPage's shape (Objects + CommonPrefixes + continuation).
type ObjectPage struct {
	Objects          []ObjectMeta `json:"objects"`
	CommonPrefixes   []string     `json:"commonPrefixes,omitempty"`
	IsTruncated      bool         `json:"isTruncated,omitempty"`
	NextContinuation string       `json:"nextContinuation,omitempty"`
}

// Sentinel errors gateways and backends pass through. Mirrors the
// shape of internal/driver's sentinels but lives in this package so
// gateway code can errors.Is against the gateway-tier names without
// importing internal/driver.
var (
	ErrUnauthenticated = errors.New("gateway: unauthenticated")
	ErrUnauthorized    = errors.New("gateway: not authorized")
	ErrNotFound        = errors.New("gateway: not found")
	ErrConflict        = errors.New("gateway: conflict")
	ErrUnsupported    = errors.New("gateway: operation not supported")
)
