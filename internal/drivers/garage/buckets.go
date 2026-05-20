package garage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListBuckets returns all buckets in the cluster.
// Endpoint: GET /v2/ListBuckets (garage-admin-v2.json:1239-1262).
func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	var resp []listBucketsResponseItem
	if err := d.client.do(ctx, "GET", "/v2/ListBuckets", nil, &resp); err != nil {
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

// GetBucket fetches a single bucket by its id.
// Endpoint: GET /v2/GetBucketInfo (garage-admin-v2.json:649-701).
func (d *driver) GetBucket(ctx context.Context, id string) (driverpkg.Bucket, error) {
	path := fmt.Sprintf("/v2/GetBucketInfo?id=%s", url.QueryEscape(id))

	var resp getBucketInfoResponse
	if err := d.client.do(ctx, "GET", path, nil, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	return bucketFromInfo(resp), nil
}

// CreateBucket creates a new bucket with the given global alias.
// Endpoint: POST /v2/CreateBucket (garage-admin-v2.json:333-366).
func (d *driver) CreateBucket(ctx context.Context, spec driverpkg.BucketSpec) (driverpkg.Bucket, error) {
	body := createBucketRequest{GlobalAlias: &spec.Alias}

	var resp getBucketInfoResponse
	if err := d.client.do(ctx, "POST", "/v2/CreateBucket", body, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	return bucketFromInfo(resp), nil
}

// UpdateBucket updates a bucket's quotas and/or website settings.
// Endpoint: POST /v2/UpdateBucket (garage-admin-v2.json:1594-1641).
func (d *driver) UpdateBucket(ctx context.Context, id string, update driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	body := updateBucketRequestBody{}

	if update.Quotas != nil {
		body.Quotas = &apiBucketQuotas{
			MaxSize:    update.Quotas.MaxSize,
			MaxObjects: update.Quotas.MaxObjects,
		}
	}

	path := fmt.Sprintf("/v2/UpdateBucket?id=%s", url.QueryEscape(id))

	var resp getBucketInfoResponse
	if err := d.client.do(ctx, "POST", path, body, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	return bucketFromInfo(resp), nil
}

// DeleteBucket deletes a bucket. The bucket must be empty.
// Endpoint: POST /v2/DeleteBucket (garage-admin-v2.json:463-497).
func (d *driver) DeleteBucket(ctx context.Context, id string) error {
	path := fmt.Sprintf("/v2/DeleteBucket?id=%s", url.QueryEscape(id))
	return d.client.do(ctx, "POST", path, nil, nil)
}

// bucketFromInfo converts a GetBucketInfoResponse into a driver.Bucket.
// GetBucketInfoResponse schema: garage-admin-v2.json:2506-2617.
func bucketFromInfo(resp getBucketInfoResponse) driverpkg.Bucket {
	bucket := driverpkg.Bucket{
		ID:                resp.ID,
		Aliases:           resp.GlobalAliases,
		Created:           resp.Created,
		Objects:           resp.Objects,
		Bytes:             resp.Bytes,
		UnfinishedUploads: resp.UnfinishedUploads,
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
			KeyID:   k.AccessKeyID,
			Name:    k.Name,
			Read:    k.Permissions.Read,
			Write:   k.Permissions.Write,
			Owner:   k.Permissions.Owner,
		})
	}
	bucket.Keys = keys

	return bucket
}

// ===== v2 wire types for buckets =====

// listBucketsResponseItem mirrors ListBucketsResponseItem (garage-admin-v2.json:3208-3237).
type listBucketsResponseItem struct {
	ID            string                 `json:"id"`
	Created       time.Time              `json:"created"`
	GlobalAliases []string               `json:"globalAliases"`
	LocalAliases  []bucketLocalAlias     `json:"localAliases"`
}

// bucketLocalAlias mirrors BucketLocalAlias (garage-admin-v2.json:2357-2374).
type bucketLocalAlias struct {
	Alias       string `json:"alias"`
	AccessKeyID string `json:"accessKeyId"`
}

// getBucketInfoResponse mirrors GetBucketInfoResponse (garage-admin-v2.json:2506-2617).
type getBucketInfoResponse struct {
	ID                          string             `json:"id"`
	Created                     time.Time          `json:"created"`
	GlobalAliases               []string           `json:"globalAliases"`
	WebsiteAccess               bool               `json:"websiteAccess"`
	Keys                        []getBucketInfoKey `json:"keys"`
	Objects                     int64              `json:"objects"`
	Bytes                       int64              `json:"bytes"`
	UnfinishedUploads           int64              `json:"unfinishedUploads"`
	UnfinishedMultipartUploads  int64              `json:"unfinishedMultipartUploads"`
	UnfinishedMultipartUploadParts int64             `json:"unfinishedMultipartUploadParts"`
	UnfinishedMultipartUploadBytes int64              `json:"unfinishedMultipartUploadBytes"`
	Quotas                      *apiBucketQuotas   `json:"quotas"`
}

// getBucketInfoKey mirrors GetBucketInfoKey (garage-admin-v2.json:2480-2505).
type getBucketInfoKey struct {
	AccessKeyID       string            `json:"accessKeyId"`
	Name              string            `json:"name"`
	Permissions       apiBucketKeyPerm  `json:"permissions"`
	BucketLocalAliases []string         `json:"bucketLocalAliases"`
}

// bucketKeyPerm is used in tests for key permissions.
type bucketKeyPerm struct {
	BucketID string `json:"bucketId"`
	Read     bool   `json:"read"`
	Write    bool   `json:"write"`
	Owner    bool   `json:"owner"`
}

// apiBucketKeyPerm mirrors ApiBucketKeyPerm (garage-admin-v2.json:3095-3106).
type apiBucketKeyPerm struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
	Owner bool `json:"owner"`
}

// createBucketRequest mirrors CreateBucketRequest (garage-admin-v2.json:2396-2415).
type createBucketRequest struct {
	GlobalAlias *string           `json:"globalAlias,omitempty"`
	LocalAlias  *createBucketLocalAlias `json:"localAlias,omitempty"`
}

// createBucketLocalAlias mirrors CreateBucketLocalAlias (garage-admin-v2.json:2357-2374).
type createBucketLocalAlias struct {
	AccessKeyID string `json:"accessKeyId"`
	Alias       string `json:"alias"`
	Allow       bool   `json:"allow"`
}

// updateBucketRequestBody mirrors UpdateBucketRequestBody (garage-admin-v2.json:4576-4608).
type updateBucketRequestBody struct {
	Quotas      *apiBucketQuotas `json:"quotas,omitempty"`
	WebsiteAccess *updateBucketWebsiteAccess `json:"websiteAccess,omitempty"`
}

// apiBucketQuotas mirrors ApiBucketQuotas (garage-admin-v2.json:1748-1765).
type apiBucketQuotas struct {
	MaxSize    *int64 `json:"maxSize,omitempty"`
	MaxObjects *int64 `json:"maxObjects,omitempty"`
}

// updateBucketWebsiteAccess mirrors UpdateBucketWebsiteAccess (garage-admin-v2.json:4608-4635).
type updateBucketWebsiteAccess struct {
	Enabled       bool   `json:"enabled"`
	ErrorDocument string `json:"errorDocument,omitempty"`
	IndexDocument string `json:"indexDocument,omitempty"`
}
