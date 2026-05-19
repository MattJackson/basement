package garage_v1

import (
	"context"
	"fmt"
	"net/url"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListBuckets returns all buckets in the cluster.
// Endpoint: GET /v1/bucket?list (garage-admin-v1.yml:575-631).
//
// OPEN: the v1 ListBuckets response items do NOT include a "created"
// timestamp (garage-admin-v1.yml:611-630), so Bucket.Created is left as the
// zero time. Callers that need creation time must fetch the bucket
// individually via GetBucket — but GetBucket via the v1 BucketInfo schema
// (garage-admin-v1.yml:1277-1328) also doesn't include `created`, so this
// field is currently always zero for the v1 driver.
func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	var resp []listBucketsItemV1
	if err := d.client.do(ctx, "GET", "/v1/bucket?list", nil, &resp); err != nil {
		return nil, err
	}

	buckets := make([]driverpkg.Bucket, 0, len(resp))
	for _, b := range resp {
		bucket := driverpkg.Bucket{
			ID:      b.ID,
			Aliases: b.GlobalAliases,
		}
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

// GetBucket fetches a single bucket by its id (32-byte hex string).
// Endpoint: GET /v1/bucket?id={id} (garage-admin-v1.yml:684-723).
//
// The "id" query parameter is documented at garage-admin-v1.yml:695-703.
// (The endpoint also accepts ?globalAlias; this driver only uses ?id since
// the Driver interface takes a string id parameter.)
func (d *driver) GetBucket(ctx context.Context, id string) (driverpkg.Bucket, error) {
	path := fmt.Sprintf("/v1/bucket?id=%s", url.QueryEscape(id))

	var resp bucketInfoV1
	if err := d.client.do(ctx, "GET", path, nil, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	return bucketFromInfo(resp), nil
}

// CreateBucket creates a new bucket with the given global alias.
// Endpoint: POST /v1/bucket (garage-admin-v1.yml:633-683).
//
// Request body shape: garage-admin-v1.yml:646-672. We only set globalAlias;
// localAlias is left unset since BucketSpec doesn't carry an access-key id.
func (d *driver) CreateBucket(ctx context.Context, spec driverpkg.BucketSpec) (driverpkg.Bucket, error) {
	body := createBucketRequestV1{GlobalAlias: spec.Alias}

	var resp bucketInfoV1
	if err := d.client.do(ctx, "POST", "/v1/bucket", body, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	return bucketFromInfo(resp), nil
}

// UpdateBucket updates a bucket's quotas and/or website settings.
// Endpoint: PUT /v1/bucket?id={id} (garage-admin-v1.yml:755-828).
//
// Body shape: garage-admin-v1.yml:786-814. Both websiteAccess and quotas
// are optional; only the quotas branch is wired here since BucketUpdate
// doesn't model website settings.
//
// OPEN: the v1 PUT /bucket endpoint does NOT modify globalAliases — alias
// management goes through the dedicated /bucket/alias/global PUT/DELETE
// endpoints (garage-admin-v1.yml:950-1017). This implementation therefore
// IGNORES update.Aliases. A future change should call PutBucketGlobalAlias
// / DeleteBucketGlobalAlias to reconcile the alias set against the current
// bucket info. For now we leave Aliases unhandled and only honor Quotas.
//
// Spec note (garage-admin-v1.yml:769-771): "In `quotas`: new values of
// `maxSize` and `maxObjects` must both be specified, or set to `null` to
// remove the quotas." We follow this — if Quotas is non-nil we always send
// both fields (using JSON null for ones the caller left as nil).
func (d *driver) UpdateBucket(ctx context.Context, id string, update driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	body := updateBucketRequestV1{}

	if update.Quotas != nil {
		body.Quotas = &updateBucketQuotasV1{
			MaxSize:    update.Quotas.MaxSize,
			MaxObjects: update.Quotas.MaxObjects,
		}
	}

	path := fmt.Sprintf("/v1/bucket?id=%s", url.QueryEscape(id))

	var resp bucketInfoV1
	if err := d.client.do(ctx, "PUT", path, body, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	return bucketFromInfo(resp), nil
}

// DeleteBucket deletes a bucket. The bucket must be empty.
// Endpoint: DELETE /v1/bucket?id={id} (garage-admin-v1.yml:726-752).
//
// Returns 204 on success (garage-admin-v1.yml:750-751); the client treats
// any 2xx as success.
func (d *driver) DeleteBucket(ctx context.Context, id string) error {
	path := fmt.Sprintf("/v1/bucket?id=%s", url.QueryEscape(id))
	return d.client.do(ctx, "DELETE", path, nil, nil)
}

// bucketFromInfo converts a BucketInfo response into a driver.Bucket.
// BucketInfo schema: garage-admin-v1.yml:1277-1328.
func bucketFromInfo(resp bucketInfoV1) driverpkg.Bucket {
	bucket := driverpkg.Bucket{
		ID:                resp.ID,
		Aliases:           resp.GlobalAliases,
		Objects:           resp.Objects,
		Bytes:             resp.Bytes,
		UnfinishedUploads: resp.UnfinishedUploads,
		// BucketInfo (garage-admin-v1.yml:1277-1328) has no "created" field.
		Created: time.Time{},
	}
	if resp.Quotas != nil && (resp.Quotas.MaxSize != nil || resp.Quotas.MaxObjects != nil) {
		bucket.Quotas = &driverpkg.Quotas{
			MaxSize:    resp.Quotas.MaxSize,
			MaxObjects: resp.Quotas.MaxObjects,
		}
	}
	keys := make([]driverpkg.BucketKeyAccess, 0, len(resp.Keys))
	for _, k := range resp.Keys {
		keys = append(keys, driverpkg.BucketKeyAccess{
			KeyID: k.AccessKeyID,
			Name:  k.Name,
			Read:  k.Permissions.Read,
			Write: k.Permissions.Write,
			Owner: k.Permissions.Owner,
		})
	}
	bucket.Keys = keys
	return bucket
}

// ===== v1 wire types =====

// listBucketsItemV1 mirrors the items returned by GET /bucket?list
// (garage-admin-v1.yml:611-630).
type listBucketsItemV1 struct {
	ID            string                  `json:"id"`
	GlobalAliases []string                `json:"globalAliases,omitempty"`
	LocalAliases  []listBucketsLocalAlias `json:"localAliases,omitempty"`
}

type listBucketsLocalAlias struct {
	Alias       string `json:"alias"`
	AccessKeyID string `json:"accessKeyId"`
}

// bucketInfoV1 mirrors BucketInfo (garage-admin-v1.yml:1277-1328).
type bucketInfoV1 struct {
	ID            string             `json:"id"`
	GlobalAliases []string           `json:"globalAliases"`
	WebsiteAccess bool               `json:"websiteAccess"`
	WebsiteConfig *websiteConfigV1   `json:"websiteConfig,omitempty"`
	Keys          []bucketKeyInfoV1  `json:"keys"`
	Objects       int64              `json:"objects"`
	Bytes         int64              `json:"bytes"`
	UnfinishedUploads int64          `json:"unfinishedUploads"`
	Quotas        *bucketQuotasV1    `json:"quotas,omitempty"`
}

type websiteConfigV1 struct {
	IndexDocument string `json:"indexDocument"`
	ErrorDocument string `json:"errorDocument,omitempty"`
}

// bucketKeyInfoV1 mirrors BucketKeyInfo (garage-admin-v1.yml:1331-1354).
type bucketKeyInfoV1 struct {
	AccessKeyID        string             `json:"accessKeyId"`
	Name               string             `json:"name"`
	Permissions        bucketKeyPermV1    `json:"permissions"`
	BucketLocalAliases []string           `json:"bucketLocalAliases,omitempty"`
}

type bucketKeyPermV1 struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
	Owner bool `json:"owner"`
}

type bucketQuotasV1 struct {
	MaxSize    *int64 `json:"maxSize"`
	MaxObjects *int64 `json:"maxObjects"`
}

// createBucketRequestV1 is the body of POST /v1/bucket
// (garage-admin-v1.yml:646-672).
type createBucketRequestV1 struct {
	GlobalAlias string `json:"globalAlias,omitempty"`
}

// updateBucketRequestV1 is the body of PUT /v1/bucket
// (garage-admin-v1.yml:786-814). Both fields are optional; an absent
// websiteAccess and quotas means "leave unchanged".
type updateBucketRequestV1 struct {
	WebsiteAccess *updateBucketWebsiteV1 `json:"websiteAccess,omitempty"`
	Quotas        *updateBucketQuotasV1  `json:"quotas,omitempty"`
}

type updateBucketWebsiteV1 struct {
	Enabled       bool   `json:"enabled"`
	IndexDocument string `json:"indexDocument,omitempty"`
	ErrorDocument string `json:"errorDocument,omitempty"`
}

// updateBucketQuotasV1 mirrors the quotas sub-object in the PUT /bucket
// body (garage-admin-v1.yml:802-814). maxSize and maxObjects are nullable;
// setting both to null clears the quotas.
type updateBucketQuotasV1 struct {
	MaxSize    *int64 `json:"maxSize"`
	MaxObjects *int64 `json:"maxObjects"`
}
