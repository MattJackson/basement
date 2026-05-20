// Package driver defines the interface for backend storage drivers.
package driver

import (
	"context"
	"time"
)

// Caps represents driver capability flags.
type Caps struct {
	Driver         string           `json:"driver"`
	Layout         LayoutCapability `json:"layout"`
	Quotas         bool             `json:"quotas"`
	BucketAliases  bool             `json:"bucketAliases"`
	KeyModel       KeyModel         `json:"keyModel"`
	Presign        bool             `json:"presign"`
	Multipart      bool             `json:"multipart"`
	Versioning     bool             `json:"versioning"`
	ObjectBrowse   bool             `json:"objectBrowse"`
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
type ObjectPage struct {
	Objects          []ObjectInfo `json:"objects"`
	NextContinuation string       `json:"nextContinuation,omitempty"`
	IsTruncated      bool         `json:"isTruncated"`
	Prefixes         []string     `json:"prefixes,omitempty"`
}

// ObjectInfo represents metadata about an object.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	ContentType  string    `json:"contentType,omitempty"`
	IsDir        bool      `json:"isDir,omitempty"`
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
