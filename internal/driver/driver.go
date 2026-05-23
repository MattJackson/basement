// Package driver defines the interface for backend storage drivers.
package driver

import (
	"context"
	"io"
	"time"
)

// Caps represents driver capability flags.
type Caps struct {
	Driver          string           `json:"driver"`
	Layout          LayoutCapability `json:"layout"`
	Quotas          bool             `json:"quotas"`
	BucketAliases   bool             `json:"bucketAliases"`
	KeyModel        KeyModel         `json:"keyModel"`
	Presign         bool             `json:"presign"`
	Multipart       bool             `json:"multipart"`
	Versioning      bool             `json:"versioning"`
	ObjectBrowse    bool             `json:"objectBrowse"`
	Streaming       bool             `json:"streaming"`
	ServerSideCopy  bool             `json:"serverSideCopy"`
}

// LayoutCapability is the layout management mode supported by the driver.
type LayoutCapability string

const (
	// LayoutApplyRevert indicates the driver supports stage-apply-revert workflow.
	LayoutApplyRevert LayoutCapability = "stage-apply-revert"
	// LayoutAtomic indicates the driver supports atomic layout application.
	LayoutAtomic LayoutCapability = "atomic"
	// LayoutReadonly indicates the driver only supports reading layout.
	LayoutReadonly LayoutCapability = "readonly"
)

// KeyModel is the key management model supported by the driver.
type KeyModel string

const (
	// KeyModelGarage indicates the driver uses Garage's key management model.
	KeyModelGarage KeyModel = "garage"
	// KeyModelIAM indicates the driver uses IAM-style key management.
	KeyModelIAM KeyModel = "iam"
	// KeyModelNone indicates the driver has no key management.
	KeyModelNone KeyModel = "none"
)

// HealthReport represents health check status.
type HealthReport struct {
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

// Node represents a cluster node.
type Node struct {
	ID       string   `json:"id"`
	Hostname string   `json:"hostname,omitempty"`
	Address  string   `json:"address,omitempty"`
	Zone     string   `json:"zone,omitempty"`
	Role     string   `json:"role,omitempty"`
	Capacity int64    `json:"capacity,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Status   string   `json:"status,omitempty"`
	Version  string   `json:"version,omitempty"`
}

// Layout represents the cluster layout.
type Layout struct {
	Version int     `json:"version"`
	Nodes   []Node  `json:"nodes"`
	Staged  *Layout `json:"staged,omitempty"`
}

// LayoutChange represents a single node change for staging.
type LayoutChange struct {
	NodeID   string   `json:"nodeId"`
	Role     *string  `json:"role,omitempty"`
	Zone     *string  `json:"zone,omitempty"`
	Capacity *int64   `json:"capacity,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// LayoutDiff represents the diff between current and staged layout.
type LayoutDiff struct {
	Adds     []Node `json:"adds"`
	Removes  []Node `json:"removes"`
	Modifies []Node `json:"modifies"`
}

// Bucket represents a storage bucket.
type Bucket struct {
	ID                string            `json:"id"`
	Aliases           []string          `json:"aliases"`
	Quotas            *Quotas           `json:"quotas,omitempty"`
	Created           time.Time         `json:"created,omitempty"`
	Objects           int64             `json:"objects"`
	Bytes             int64             `json:"bytes"`
	UnfinishedUploads int64             `json:"unfinishedUploads"`
	Keys              []BucketKeyAccess `json:"keys,omitempty"`
}

// Quotas represents bucket quota limits.
type Quotas struct {
	MaxSize    *int64 `json:"maxSize,omitempty"`
	MaxObjects *int64 `json:"maxObjects,omitempty"`
}

// BucketKeyAccess represents the per-bucket view of a key's access permissions.
// This data mirrors what is also accessible from the Key side via Key.BucketsPermissions,
// which will be exposed through a future driver method not yet on the interface.
type BucketKeyAccess struct {
	KeyID string `json:"keyId"`
	Name  string `json:"name"`
	Read  bool   `json:"read"`
	Write bool   `json:"write"`
	Owner bool   `json:"owner"`
}

// KeyBucketAccess represents the per-key view of bucket access permissions.
// This is symmetric to Bucket.Keys[] (the per-bucket view). Each entry contains
// the bucket ID, optional global and local aliases, and the key's permissions
// on that bucket (read, write, owner). Present only on GetKey/UpdateKeyPermissions
// detail responses, not on ListKeys.
type KeyBucketAccess struct {
	BucketID      string   `json:"bucketId"`
	GlobalAliases []string `json:"globalAliases,omitempty"`
	LocalAliases  []string `json:"localAliases,omitempty"`
	Read          bool     `json:"read"`
	Write         bool     `json:"write"`
	Owner         bool     `json:"owner"`
}

// BucketSpec is the specification for creating a bucket.
type BucketSpec struct {
	Alias string `json:"alias"`
}

// BucketUpdate represents fields to update on a bucket.
type BucketUpdate struct {
	Aliases *[]string `json:"aliases,omitempty"`
	Quotas  *Quotas   `json:"quotas,omitempty"`
}

// Key represents an access key.
type Key struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessKeyID string `json:"accessKeyId"`
	// SecretAccessKey is populated only on the CreateKey response.
	// Backends (Garage included) return the secret exactly once at
	// creation and never again — drivers MUST pass it through on the
	// create path so the UI's shown-once dialog can surface it.
	// ListKeys / GetKey responses leave it nil.
	SecretAccessKey   *string           `json:"secretAccessKey,omitempty"`
	Created           time.Time         `json:"created,omitempty"`
	AllowCreateBucket bool              `json:"allowCreateBucket"`
	Buckets           []KeyBucketAccess `json:"buckets,omitempty"`
}

// KeySpec is the specification for creating a key.
type KeySpec struct {
	Name string `json:"name"`
}

// BucketPermission represents permissions granted to a key on a bucket.
type BucketPermission struct {
	BucketID string `json:"bucketId"`
	Read     bool   `json:"read"`
	Write    bool   `json:"write"`
	Owner    bool   `json:"owner"`
}

// ObjectPage represents a page of objects in a bucket.
//
// CommonPrefixes is the S3 ListObjectsV2 sub-folder list — populated
// when ListObjects is called with a non-empty delimiter (typically "/").
// Each entry is a full prefix string ending in the delimiter
// (e.g. "raw/", "index/broadcom-docid/"); the UI renders these as
// clickable folder rows that drill in by re-listing with prefix=entry.
// Empty when delimiter="" (the flat-list mode the sync engine uses).
type ObjectPage struct {
	Objects          []ObjectInfo `json:"objects"`
	NextContinuation string       `json:"nextContinuation,omitempty"`
	IsTruncated      bool         `json:"isTruncated"`
	CommonPrefixes   []string     `json:"commonPrefixes,omitempty"`
}

// ObjectInfo represents metadata about an object.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	IsDir        bool      `json:"is_dir,omitempty"`
}

// PresignedURL represents a presigned URL.
type PresignedURL struct {
	URL     string    `json:"url"`
	Expires time.Time `json:"expires"`
	Method  string    `json:"method"`
}

// MultipartUpload represents an in-progress multipart upload.
type MultipartUpload struct {
	UploadID    string `json:"uploadId"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	ContentType string `json:"contentType,omitempty"`
}

// CompletedPart represents a completed part in a multipart upload.
type CompletedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

// StreamResult contains the result of a StreamObject call.
type StreamResult struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ETag          string
	LastModified  time.Time
}

// PutResult contains the result of a PutObjectStream call.
type PutResult struct {
	ETag string
}

// LifecycleRule represents one rule in a bucket lifecycle policy
// (S3-compatible). Drivers translate between this and their native
// representation. Per LIFECYCLE.WIZARD (v0.9.0i): the wire shape is
// flat + S3-shaped so the UI can treat all four drivers uniformly;
// per-backend differences surface via LifecycleCapabilities, not via
// rule-shape divergence.
type LifecycleRule struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`                       // "Enabled" | "Disabled"
	Prefix             string `json:"prefix,omitempty"`             // applies to objects with this prefix
	ExpirationDays     *int   `json:"expirationDays,omitempty"`     // delete after N days
	TransitionDays     *int   `json:"transitionDays,omitempty"`     // move to lower-tier after N days
	TransitionTier     string `json:"transitionTier,omitempty"`     // e.g. "GLACIER", "STANDARD_IA"
	NoncurrentDays     *int   `json:"noncurrentDays,omitempty"`     // for versioned buckets
	AbortMultipartDays *int   `json:"abortMultipartDays,omitempty"` // cancel incomplete uploads after N days
}

// LifecycleCapabilities tells the UI what this driver supports.
// Per the operator's driver-parity doctrine the UI gates rule
// editing on these flags, not on the driver name — Garage v1 sets
// Supported=false today and the rule editor renders disabled fields
// uniformly across backends as a result.
type LifecycleCapabilities struct {
	Supported          bool     `json:"supported"`
	Expiration         bool     `json:"expiration"`         // delete after N days
	Transition         bool     `json:"transition"`         // tier migration
	TransitionTiers    []string `json:"transitionTiers"`    // valid TransitionTier values
	NoncurrentDays     bool     `json:"noncurrentDays"`     // versioned bucket support
	AbortMultipartDays bool     `json:"abortMultipartDays"`
}

// ScrubCapability advertises whether this driver exposes block-scrub
// controls (v1.4.0c). Garage owns its own block store and can perform
// durability scans on demand; S3-compatible backends (AWS, MinIO) hide
// the durability machinery from operators, so the UI hides the Run
// button rather than pretending. Reason is the operator-facing text
// rendered in the maintenance card when Supported=false.
type ScrubCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// VersioningStatus reports the current versioning state of a bucket
// (v1.10.0a). The "disabled" state is distinct from "suspended":
// disabled means versioning has never been turned on (the S3
// GetBucketVersioning response has no Status field), while suspended
// means versioning was previously enabled and then turned off (Status
// is explicitly "Suspended" — existing versions remain queryable but
// no new versions are created for subsequent writes).
type VersioningStatus string

const (
	// VersioningEnabled indicates the bucket actively versions objects.
	VersioningEnabled VersioningStatus = "enabled"
	// VersioningSuspended indicates the bucket retains existing
	// versions but no longer creates new ones.
	VersioningSuspended VersioningStatus = "suspended"
	// VersioningDisabled indicates the bucket has never had versioning
	// turned on.
	VersioningDisabled VersioningStatus = "disabled"
)

// ObjectVersion represents one historical version of an object in a
// versioned bucket (v1.10.0a). Returned by ListObjectVersions and
// surfaced to the UI's version-history view.
//
// IsDeleteMarker is true for "tombstone" rows S3 inserts when an
// object is DELETEd in a versioned bucket — the row records the
// delete event but carries no payload. The UI renders these
// distinctly from real versions so an operator can spot "this version
// was deleted on T" vs "this version was overwritten on T".
type ObjectVersion struct {
	VersionID      string    `json:"versionId"`
	Key            string    `json:"key"`
	Size           int64     `json:"size"`
	ETag           string    `json:"etag,omitempty"`
	LastModified   time.Time `json:"lastModified,omitempty"`
	IsLatest       bool      `json:"isLatest"`
	IsDeleteMarker bool      `json:"isDeleteMarker,omitempty"`
}

// ObjectLockMode names the two S3 Object Lock retention modes
// (v1.10.0c). Governance is the "operator can override with a
// bypass" mode; Compliance is the strict mode where NO ONE — not
// even the bucket owner — can reduce or remove the retention until
// the date expires. The UI treats these distinctly: governance
// surfaces a "Bypass governance" confirmation toggle on destructive
// actions; compliance hides destructive controls entirely while the
// retention is active.
type ObjectLockMode string

const (
	// ObjectLockGovernance allows users with the
	// s3:BypassGovernanceRetention permission to delete or reduce
	// retention. Useful for "approval-gated" workflows.
	ObjectLockGovernance ObjectLockMode = "GOVERNANCE"
	// ObjectLockCompliance is strict — retention can only expire
	// naturally. Used by regulators (SEC 17a-4, FINRA, etc).
	ObjectLockCompliance ObjectLockMode = "COMPLIANCE"
)

// ObjectLockRetention captures a single per-object retention policy.
// Mode controls override semantics (see ObjectLockMode); RetainUntilDate
// is the wall-clock deadline at which the object becomes deletable
// again. Both fields are required when setting a retention — the API
// layer rejects partial requests at the wire shape.
type ObjectLockRetention struct {
	Mode            ObjectLockMode `json:"mode"`
	RetainUntilDate time.Time      `json:"retainUntilDate"`
}

// ObjectLockConfig is the bucket-level Object Lock configuration.
// Enabled mirrors S3's ObjectLockEnabled flag. DefaultRetention is
// optional — when present, every new object inherits this retention
// at PUT time (the UI surfaces this as "default retention" in the
// bucket settings card).
//
// NOTE: S3 only allows ObjectLockEnabled to be set TRUE — there is
// no S3 contract for turning it back off on a bucket that already
// has it enabled. The API layer rejects attempts to flip it from
// true to false with a 400; the FE never surfaces a "disable" button
// for the same reason.
type ObjectLockConfig struct {
	Enabled          bool                 `json:"enabled"`
	DefaultRetention *ObjectLockRetention `json:"defaultRetention,omitempty"`
}

// ScrubState carries the live state of a block-scrub operation.
// Running flips to true the moment a scrub kicks off and back to
// false once Garage flushes the completion record; the UI polls
// every 5s while it's true. ProgressPercent is 0..100; backends
// that don't expose progress leave it at 0 even while running.
// Message is the free-form text Garage emits ("scanning blocks",
// "complete: 3 corrupt blocks repaired") — surfaced verbatim so the
// operator sees the backend's own diagnostic rather than a basement-
// ui paraphrase.
type ScrubState struct {
	Running         bool      `json:"running"`
	LastCompleted   time.Time `json:"lastCompleted,omitempty"`
	BlocksScanned   int64     `json:"blocksScanned,omitempty"`
	BlocksCorrupt   int64     `json:"blocksCorrupt,omitempty"`
	ProgressPercent int       `json:"progressPercent,omitempty"`
	Message         string    `json:"message,omitempty"`
}

// Driver is the interface that all backend drivers must implement.
type Driver interface {
	// Identity
	Capabilities(ctx context.Context) (Caps, error)
	HealthCheck(ctx context.Context) (HealthReport, error)

	// Cluster
	ListNodes(ctx context.Context) ([]Node, error)
	GetLayout(ctx context.Context) (Layout, error)
	StageLayout(ctx context.Context, change LayoutChange) (LayoutDiff, error)
	ApplyLayout(ctx context.Context) error
	RevertLayout(ctx context.Context) error

	// Buckets
	ListBuckets(ctx context.Context) ([]Bucket, error)
	GetBucket(ctx context.Context, id string) (Bucket, error)
	CreateBucket(ctx context.Context, spec BucketSpec) (Bucket, error)
	UpdateBucket(ctx context.Context, id string, update BucketUpdate) (Bucket, error)
	DeleteBucket(ctx context.Context, id string) error

	// Keys
	ListKeys(ctx context.Context) ([]Key, error)
	GetKey(ctx context.Context, id string) (Key, error)
	CreateKey(ctx context.Context, spec KeySpec) (Key, error)
	UpdateKeyPermissions(ctx context.Context, keyID string, perms []BucketPermission) error
	DeleteKey(ctx context.Context, id string) error

	// S3 data plane (admin object browser + end-user UI).
	//
	// ListObjects: an empty delimiter performs a recursive flat list
	// (every key under prefix, the shape the sync engine + scripts
	// want). delimiter="/" performs folder-tier browsing: Objects is
	// only the keys directly under prefix and CommonPrefixes carries
	// the distinct sub-folder prefixes (e.g. "raw/", "index/").
	ListObjects(ctx context.Context, bucket, prefix, continuation, delimiter string, limit int) (ObjectPage, error)
	StatObject(ctx context.Context, bucket, key string) (ObjectInfo, error)
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (PresignedURL, error)
	PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (PresignedURL, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	CreateMultipart(ctx context.Context, bucket, key, contentType string) (MultipartUpload, error)
	PresignUploadPart(ctx context.Context, upload MultipartUpload, partNum int) (PresignedURL, error)
	CompleteMultipart(ctx context.Context, upload MultipartUpload, parts []CompletedPart) error
	AbortMultipart(ctx context.Context, upload MultipartUpload) error

	// Streaming object operations for sync primitives.
	StreamObject(ctx context.Context, bucket, key, rng string) (StreamResult, error)
	PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) (PutResult, error)

	// ServerSideCopy copies an object from (srcBucket, srcKey) to
	// (dstBucket, dstKey) within the same backend. Drivers that can't
	// (or for cross-driver pairs the sync engine never calls this on)
	// return ErrUnsupported.
	ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// Bucket lifecycle (v0.9.0i LIFECYCLE.WIZARD). LifecycleSupport()
	// is the UI's gate — drivers without lifecycle return
	// {Supported: false} and the rule editor renders disabled. Get
	// and Put return ErrUnsupported on those drivers; UI never sees
	// the error because it short-circuits on the capability flag, but
	// the wrapped sentinel makes a direct API caller's error path sane.
	LifecycleSupport() LifecycleCapabilities
	GetLifecycle(ctx context.Context, bucketID string) ([]LifecycleRule, error)
	PutLifecycle(ctx context.Context, bucketID string, rules []LifecycleRule) error

	// PerBucketStatsAvailable reports whether ListBuckets / GetBucket
	// populates the Objects + Bytes fields on a Bucket reliably. The
	// UI uses this to hide the Size / Objects columns on the per-region
	// bucket list when the driver can't provide them — fewer dashes,
	// less "is this broken or empty?" confusion (v1.4.0a).
	//
	// Doctrine: this is a per-DRIVER capability flag, not a per-call
	// "did we populate it this time" — the UI consults it once to
	// decide column visibility and stays consistent across reloads.
	//
	// Garage v1 returns false today: the v1 admin API does not expose
	// per-bucket stats on its public ListBuckets endpoint at the
	// user-region tier. AWS S3 / MinIO / Garage v2 advertise true
	// because their respective admin / bucket-metrics surfaces can be
	// wrapped into a stats-populating ListBuckets variant (the FE just
	// renders whatever the bucket carries — zero is a real "this bucket
	// is empty", not the "we don't know" sentinel).
	PerBucketStatsAvailable() bool

	// Block-scrub maintenance (v1.4.0c). ScrubSupport is the UI's
	// gate — drivers that don't advertise scrub (AWS S3, MinIO) hide
	// the Run button and surface the Reason string instead. ScrubState
	// / StartScrub return ErrUnsupported on those drivers; the UI's
	// short-circuit on the capability flag means a direct API caller
	// is the only path that sees the error.
	ScrubSupport() ScrubCapability
	ScrubState(ctx context.Context) (ScrubState, error)
	StartScrub(ctx context.Context) error

	// Bucket versioning (v1.10.0a). VersioningSupport is the UI's
	// gate — drivers that don't advertise versioning (Garage v1 / v2
	// today) hide the toggle entirely and the methods below return
	// ErrUnsupported. AWS S3 + MinIO implement the full set.
	//
	// GetVersioningStatus reports the current state per
	// VersioningStatus (enabled / suspended / disabled). Toggling
	// from any state to Enabled flips to enabled; from enabled to
	// suspended preserves existing versions but stops creating new
	// ones. There is no S3-side "un-suspend back to never-enabled" —
	// disabled is observable only on buckets that were never enabled
	// in the first place.
	//
	// ListObjectVersions surfaces all versions (current +
	// noncurrent) AND delete markers, ordered by key then by version
	// (newest first within each key). versionIDMarker is the
	// continuation token returned in the previous page's
	// nextVersionIDMarker; pass empty for the first call. The second
	// return value is nextVersionIDMarker — empty when the result is
	// not truncated.
	//
	// GetObjectVersion streams a specific version of a key (the same
	// shape as StreamObject but pinned to versionID). Required for
	// the UI's "restore this version" preview + download flows.
	//
	// DeleteObjectVersion permanently deletes a single version row,
	// including delete markers. This is distinct from a regular
	// DeleteObject on a versioned bucket — that inserts a delete
	// marker; this removes a specific historical version forever.
	VersioningSupport() bool
	GetVersioningStatus(ctx context.Context, bucket string) (VersioningStatus, error)
	EnableVersioning(ctx context.Context, bucket string) error
	SuspendVersioning(ctx context.Context, bucket string) error
	ListObjectVersions(ctx context.Context, bucket, prefix, versionIDMarker string, limit int) ([]ObjectVersion, string, error)
	GetObjectVersion(ctx context.Context, bucket, key, versionID string) (StreamResult, error)
	DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error

	// Object Lock (v1.10.0c). Layered on top of versioning per the
	// S3 spec — Object Lock REQUIRES versioning to be enabled on
	// the bucket. The API layer enforces that ordering; drivers
	// that don't advertise Object Lock (Garage v1 / v2 today)
	// return ErrUnsupported from all six ops and the FE hides the
	// settings card entirely.
	//
	// GetObjectLockConfig returns the bucket's current config. On
	// buckets that have never had Object Lock enabled, S3 returns
	// an "ObjectLockConfigurationNotFoundError" — drivers normalize
	// this to ObjectLockConfig{Enabled: false} so the FE can render
	// the off-state UI without an error banner. Buckets with the
	// flag set but no default retention come back as {Enabled: true,
	// DefaultRetention: nil}.
	//
	// PutObjectLockConfig writes the bucket-level config. S3's
	// contract is one-way — once Enabled is true, the only valid
	// PUT is another true (with or without a DefaultRetention
	// change). Drivers reject attempts to flip back to false with
	// ErrInvalid so a UI bug or direct caller doesn't silently leak
	// a compliance violation.
	//
	// GetObjectRetention / PutObjectRetention read and write the
	// per-version retention policy. versionID is required — the
	// per-object surface always pins to a specific version row.
	// bypassGovernance is honoured only when the existing mode is
	// GOVERNANCE; with COMPLIANCE, ANY attempt to reduce or remove
	// retention rejects regardless of the bypass flag.
	//
	// GetObjectLegalHold / PutObjectLegalHold toggle the per-version
	// legal-hold flag. Independent of retention — a legal hold can
	// be on or off regardless of retention state, and it can persist
	// past the retain-until date. Removal requires the
	// s3:PutObjectLegalHold permission; the driver surfaces backend
	// errors verbatim via the standard error mapping.
	ObjectLockSupport() bool
	GetObjectLockConfig(ctx context.Context, bucket string) (*ObjectLockConfig, error)
	PutObjectLockConfig(ctx context.Context, bucket string, cfg ObjectLockConfig) error
	GetObjectRetention(ctx context.Context, bucket, key, versionID string) (*ObjectLockRetention, error)
	PutObjectRetention(ctx context.Context, bucket, key, versionID string, retention ObjectLockRetention, bypassGovernance bool) error
	GetObjectLegalHold(ctx context.Context, bucket, key, versionID string) (bool, error)
	PutObjectLegalHold(ctx context.Context, bucket, key, versionID string, on bool) error
}
