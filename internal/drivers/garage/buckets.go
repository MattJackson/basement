// Package garage implements the garage device driver.
package garage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListBuckets returns all buckets in the cluster.
// Endpoint: GET /v2/ListBuckets (docs/garage-admin-api.md lines 252-259)
func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	var resp []listBucketsResponseItem
	if err := d.client.do(ctx, "GET", "/v2/ListBuckets", nil, &resp); err != nil {
		return nil, err
	}

	buckets := make([]driverpkg.Bucket, 0, len(resp))
	for _, b := range resp {
		createdTime, _ := time.Parse(time.RFC3339, b.Created)

		bucket := driverpkg.Bucket{
			ID:      b.ID,
			Aliases: b.GlobalAliases,
			Created: createdTime,
		}

		buckets = append(buckets, bucket)
	}

	return buckets, nil
}

// GetBucket returns details for a specific bucket by ID or globalAlias.
// Endpoint: GET /v2/GetBucketInfo?id={id}&globalAlias={alias} (docs/garage-admin-api.md lines 224-232)
func (d *driver) GetBucket(ctx context.Context, _ string) (driverpkg.Bucket, error) {
	var resp getBucketInfoResponse
	if err := d.client.do(ctx, "GET", "/v2/GetBucketInfo", nil, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	bucket := driverpkg.Bucket{
		ID:                resp.ID,
		Aliases:           resp.GlobalAliases,
		Created:           resp.Created,
		Objects:           resp.Objects,
		Bytes:             resp.Bytes,
		UnfinishedUploads: resp.UnfinishedUploads,
	}

	if resp.Quotas != nil {
		var quotas driverpkg.Quotas
		if resp.Quotas.MaxSize != nil {
			quotas.MaxSize = resp.Quotas.MaxSize
		}
		if resp.Quotas.MaxObjects != nil {
			quotas.MaxObjects = resp.Quotas.MaxObjects
		}
		bucket.Quotas = &quotas
	}

	keys := make([]driverpkg.BucketKeyAccess, 0, len(resp.Keys))
	for _, k := range resp.Keys {
		for _, p := range k.Permissions {
			if p.BucketID == bucket.ID || p.BucketID == "" {
				keys = append(keys, driverpkg.BucketKeyAccess{
					KeyID: k.AccessKeyID,
					Name:  k.Name,
					Read:  p.Read,
					Write: p.Write,
					Owner: p.Owner,
				})
				break
			}
		}
	}
	bucket.Keys = keys

	return bucket, nil
}

// CreateBucket creates a new bucket with the specified alias.
// Endpoint: POST /v2/CreateBucket (docs/garage-admin-api.md lines 205-212)
func (d *driver) CreateBucket(ctx context.Context, spec driverpkg.BucketSpec) (driverpkg.Bucket, error) {
	reqBody := createBucketRequest{
		GlobalAlias: &spec.Alias,
	}

	var resp getBucketInfoResponse
	if err := d.client.do(ctx, "POST", "/v2/CreateBucket", reqBody, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	bucket := driverpkg.Bucket{
		ID:                resp.ID,
		Aliases:           resp.GlobalAliases,
		Created:           resp.Created,
		Objects:           resp.Objects,
		Bytes:             resp.Bytes,
		UnfinishedUploads: resp.UnfinishedUploads,
	}

	if resp.Quotas != nil {
		var quotas driverpkg.Quotas
		if resp.Quotas.MaxSize != nil {
			quotas.MaxSize = resp.Quotas.MaxSize
		}
		if resp.Quotas.MaxObjects != nil {
			quotas.MaxObjects = resp.Quotas.MaxObjects
		}
		bucket.Quotas = &quotas
	}

	keys := make([]driverpkg.BucketKeyAccess, 0, len(resp.Keys))
	for _, k := range resp.Keys {
		for _, p := range k.Permissions {
			if p.BucketID == bucket.ID || p.BucketID == "" {
				keys = append(keys, driverpkg.BucketKeyAccess{
					KeyID: k.AccessKeyID,
					Name:  k.Name,
					Read:  p.Read,
					Write: p.Write,
					Owner: p.Owner,
				})
				break
			}
		}
	}
	bucket.Keys = keys

	return bucket, nil
}

// UpdateBucket updates a bucket's aliases and/or quotas.
// Endpoint: POST /v2/UpdateBucket?id={id} (docs/garage-admin-api.md lines 261-269)
func (d *driver) UpdateBucket(ctx context.Context, id string, update driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	reqBody := updateBucketRequestBody{}

	if update.Aliases != nil {
		reqBody.GlobalAliases = *update.Aliases
	}

	if update.Quotas != nil {
		var quotas apiBucketQuotas
		if update.Quotas.MaxSize != nil {
			val := *update.Quotas.MaxSize
			quotas.MaxSize = &val
		} else {
			quotas.MaxSize = nil
		}
		if update.Quotas.MaxObjects != nil {
			val := *update.Quotas.MaxObjects
			quotas.MaxObjects = &val
		} else {
			quotas.MaxObjects = nil
		}
		reqBody.Quotas = &quotas
	}

	var resp getBucketInfoResponse
	path := fmt.Sprintf("/v2/UpdateBucket?id=%s", url.QueryEscape(id))
	if err := d.client.do(ctx, "POST", path, reqBody, &resp); err != nil {
		return driverpkg.Bucket{}, err
	}

	bucket := driverpkg.Bucket{
		ID:                resp.ID,
		Aliases:           resp.GlobalAliases,
		Created:           resp.Created,
		Objects:           resp.Objects,
		Bytes:             resp.Bytes,
		UnfinishedUploads: resp.UnfinishedUploads,
	}

	if resp.Quotas != nil {
		var quotas driverpkg.Quotas
		if resp.Quotas.MaxSize != nil {
			val := *resp.Quotas.MaxSize
			quotas.MaxSize = &val
		}
		if resp.Quotas.MaxObjects != nil {
			val := *resp.Quotas.MaxObjects
			quotas.MaxObjects = &val
		}
		bucket.Quotas = &quotas
	}

	keys := make([]driverpkg.BucketKeyAccess, 0, len(resp.Keys))
	for _, k := range resp.Keys {
		for _, p := range k.Permissions {
			if p.BucketID == bucket.ID || p.BucketID == "" {
				keys = append(keys, driverpkg.BucketKeyAccess{
					KeyID: k.AccessKeyID,
					Name:  k.Name,
					Read:  p.Read,
					Write: p.Write,
					Owner: p.Owner,
				})
				break
			}
		}
	}
	bucket.Keys = keys

	return bucket, nil
}

// DeleteBucket deletes a bucket by ID.
// Endpoint: POST /v2/DeleteBucket?id={id} (docs/garage-admin-api.md lines 214-222)
func (d *driver) DeleteBucket(ctx context.Context, id string) error {
	path := fmt.Sprintf("/v2/DeleteBucket?id=%s", url.QueryEscape(id))
	return d.client.do(ctx, "POST", path, nil, nil)
}

// Response types for ListBuckets endpoint

type listBucketsResponseItem struct {
	ID             string   `json:"id"`
	Created        string   `json:"created"`
	GlobalAliases  []string `json:"globalAliases"`
	LocalAliases   []string `json:"localAliases"`
}

// Request/Response types for GetBucket, CreateBucket, UpdateBucket, DeleteBucket

type getBucketInfoResponse struct {
	ID                              string            `json:"id"`
	Created                         time.Time         `json:"created"`
	GlobalAliases                   []string          `json:"globalAliases"`
	WebsiteAccess                   bool              `json:"websiteAccess"`
	Keys                            []getBucketInfoKey `json:"keys"`
	Objects                         int64             `json:"objects"`
	Bytes                           int64             `json:"bytes"`
	UnfinishedUploads               int64             `json:"unfinishedUploads"`
	UnfinishedMultipartUploads      int64             `json:"unfinishedMultipartUploads"`
	UnfinishedMultipartUploadParts  int64             `json:"unfinishedMultipartUploadParts"`
	UnfinishedMultipartUploadBytes  int64             `json:"unfinishedMultipartUploadBytes"`
	Quotas                          *apiBucketQuotas  `json:"quotas,omitempty"`
	WebsiteConfig                   *websiteConfig    `json:"websiteConfig,omitempty"`
	LifecycleRules                  interface{}       `json:"lifecycleRules,omitempty"`
	CorsRules                       interface{}       `json:"corsRules,omitempty"`
}

type getBucketInfoKey struct {
	AccessKeyID      string             `json:"accessKeyId"`
	Name             string             `json:"name"`
	Permissions      []bucketKeyPerm    `json:"permissions"`
	BucketLocalAliases []bucketLocalAlias `json:"bucketLocalAliases"`
}

type bucketKeyPerm struct {
	BucketID string `json:"bucketId"`
	Read     bool   `json:"read"`
	Write    bool   `json:"write"`
	Owner    bool   `json:"owner"`
}

type bucketLocalAlias struct {
	AccessKeyID string `json:"accessKeyId"`
	Alias       string `json:"alias"`
	AllowRead   bool   `json:"allowRead"`
	AllowWrite  bool   `json:"allowWrite"`
}

type apiBucketQuotas struct {
	MaxSize    *int64 `json:"maxSize,omitempty"`
	MaxObjects *int64 `json:"maxObjects,omitempty"`
}

type websiteConfig struct {
	IndexDocument     string            `json:"indexDocument"`
	ErrorDocument     string            `json:"errorDocument,omitempty"`
	RewriteRules      []rewriteRule     `json:"rewriteRules,omitempty"`
	RedirectAllRequestsTo *redirectConfig `json:"redirectAllRequestsTo,omitempty"`
}

type rewriteRule struct {
	KeyPrefixMatch    string `json:"keyPrefixMatch"`
	Condition         string `json:"condition,omitempty"`
	ReplaceKeyPrefix  string `json:"replaceKeyPrefix,omitempty"`
	HTTPRedirectCode  int    `json:"httpRedirectCode,omitempty"`
}

type redirectConfig struct {
	HostName        string `json:"hostName"`
	Protocol        string `json:"protocol,omitempty"`
}

type createBucketRequest struct {
	GlobalAlias *string              `json:"globalAlias,omitempty"`
	LocalAlias  *createBucketLocalAlias `json:"localAlias,omitempty"`
}

type createBucketLocalAlias struct {
	AccessKeyID string `json:"accessKeyId"`
	Alias       string `json:"alias"`
	Allow       bool   `json:"allow"`
}

type updateBucketRequestBody struct {
	GlobalAliases []string        `json:"globalAliases,omitempty"`
	Quotas        *apiBucketQuotas `json:"quotas,omitempty"`
}
