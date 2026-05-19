// Package driver defines the interface for backend storage drivers.
package driver

import (
	"context"
	"time"
)

// Caps represents driver capability flags.
type Caps struct {
	Driver        string         // human-readable: "Garage 1.0.1"
	Layout        LayoutCapability // "stage-apply-revert" | "atomic" | "readonly"
	Quotas        bool
	BucketAliases bool
	KeyModel      KeyModel       // "garage" | "iam" | "none"
	Presign       bool
	Multipart     bool
	Versioning    bool
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
	Status  string            // e.g., "healthy", "degraded", "unavailable"
	Details map[string]any    // backend-specific details
}

// Node represents a cluster node.
type Node struct {
	ID       string   // unique node identifier
	Hostname string   // hostname
	Address  string   // network address
	Zone     string   // availability zone
	Role     string   // current role in layout
	Capacity int64    // capacity in bytes
	Tags     []string // tags/labels
	Status   string   // "connected" | "unreachable"
	Version  string   // Garage version
}

// Layout represents the cluster layout.
type Layout struct {
	Version int     // layout version number
	Nodes   []Node  // nodes in this layout
	Staged  *Layout // staged but not yet applied layout (if any)
}

// LayoutChange represents a single node change for staging.
type LayoutChange struct {
	NodeID string   // node ID to modify
	Role   *string  // new role (nil = don't change)
	Zone   *string  // new zone (nil = don't change)
	Capacity *int64 // new capacity in bytes (nil = don't change)
	Tags   []string // replace tags with this list
}

// LayoutDiff represents the diff between current and staged layout.
type LayoutDiff struct {
	Adds      []Node // nodes to add
	Removes   []Node // nodes to remove
	Modifies  []Node // nodes to modify (by ID)
}

// Bucket represents a storage bucket.
type Bucket struct {
	ID        string    // unique bucket identifier
	Aliases   []string  // alias names
	Quotas    *Quotas   // quota settings (nil if unlimited)
	Created   time.Time // creation timestamp
}

// Quotas represents bucket quota limits.
type Quotas struct {
	MaxSize    *int64 // max size in bytes (nil = unlimited)
	MaxObjects *int64 // max object count (nil = unlimited)
}

// BucketSpec is the specification for creating a bucket.
type BucketSpec struct {
	Alias string // alias name
}

// BucketUpdate represents fields to update on a bucket.
type BucketUpdate struct {
	Aliases *[]string // new aliases list (nil = don't change)
	Quotas  *Quotas   // new quotas (nil = don't change)
}

// Key represents an access key.
type Key struct {
	ID                string    // unique key identifier
	Name              string    // human-readable name
	AccessKeyID       string    // the actual access key ID
	Created           time.Time // creation timestamp
	AllowCreateBucket bool      // whether this key can create buckets
}

// KeySpec is the specification for creating a key.
type KeySpec struct {
	Name string // human-readable name
}

// BucketPermission represents permissions granted to a key on a bucket.
type BucketPermission struct {
	BucketID string // bucket ID
	Read     bool   // read permission
	Write    bool   // write permission
	Owner    bool   // owner permission (full access)
}

// ObjectPage represents a page of objects in a bucket.
type ObjectPage struct {
	Objects        []ObjectInfo // objects on this page
	NextContinuation string      // token for next page (empty if last page)
	IsTruncated    bool         // whether results were truncated
	Prefixes       []string     // common prefixes (for prefix grouping)
}

// ObjectInfo represents metadata about an object.
type ObjectInfo struct {
	Key          string    // object key/name
	Size         int64     // size in bytes
	LastModified time.Time // last modification timestamp
	ETag         string    // entity tag (MD5 for simple uploads)
	ContentType  string    // MIME content type
	IsDir        bool      // true if this is a marker for a "directory"
}

// PresignedURL represents a presigned URL.
type PresignedURL struct {
	URL     string    // the presigned URL
	Expires time.Time // expiration timestamp
	Method  string    // HTTP method (GET, PUT, etc.)
}

// MultipartUpload represents an in-progress multipart upload.
type MultipartUpload struct {
	UploadID    string // multipart upload ID
	Bucket      string // bucket name
	Key         string // object key
	ContentType string // content type for the final object
}

// CompletedPart represents a completed part in a multipart upload.
type CompletedPart struct {
	PartNumber int    // part number (1-based)
	ETag       string // entity tag from the put-part response
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

	// S3 data plane (admin object browser + end-user UI)
	ListObjects(ctx context.Context, bucket, prefix, continuation string, limit int) (ObjectPage, error)
	StatObject(ctx context.Context, bucket, key string) (ObjectInfo, error)
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (PresignedURL, error)
	PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (PresignedURL, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	CreateMultipart(ctx context.Context, bucket, key, contentType string) (MultipartUpload, error)
	PresignUploadPart(ctx context.Context, upload MultipartUpload, partNum int) (PresignedURL, error)
	CompleteMultipart(ctx context.Context, upload MultipartUpload, parts []CompletedPart) error
	AbortMultipart(ctx context.Context, upload MultipartUpload) error
}
