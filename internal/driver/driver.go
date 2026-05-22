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
}
