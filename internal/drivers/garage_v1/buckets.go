package garage_v1

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListBuckets returns all buckets in the cluster.
// Endpoint: GET /v1/bucket?list (garage-admin-v1.yml:575-631).
//
// ADR-0002 v1.1.0c — when this driver is built for the region tier
// (user supplies only s3_endpoint + access_key + secret, no admin
// URL), there is no admin client to talk to. Fall back to the S3
// ListBuckets API in that case so the user's key can enumerate the
// buckets it can reach without basement needing admin creds.
//
// OPEN: the v1 ListBuckets response items do NOT include a "created"
// timestamp (garage-admin-v1.yml:611-630), so Bucket.Created is left as the
// zero time. Callers that need creation time must fetch the bucket
// individually via GetBucket — but GetBucket via the v1 BucketInfo schema
// (garage-admin-v1.yml:1277-1328) also doesn't include `created`, so this
// field is currently always zero for the v1 driver.
//
// Performance note: This method fans out GetBucket calls via a bounded
// worker pool (8 concurrent goroutines) to populate Objects, Bytes, and
// UnfinishedUploads fields. At 200 buckets with ~50ms per call, fanout
// adds ~1.5s total latency — acceptable for the admin list page. For
// clusters >500 buckets, consider adding pagination support in a follow-up.
func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	// Region-tier fallback: no admin client wired -> use S3 ListBuckets.
	//
	// CAVEAT: Garage's S3 data-plane endpoint does NOT implement the
	// ListBuckets verb (returns 404). Bucket enumeration only works
	// via the admin API on Garage, which the region tier doesn't have
	// creds for. AWS S3 + MinIO both implement S3 ListBuckets, so
	// region-tier users on those backends get a real list. Garage
	// users see an empty list and must navigate to a known bucket by
	// URL — the bucket browser itself still works because
	// ListObjects + presign-* go through the bucket-specific S3 paths
	// which Garage DOES implement. Tracked in ADR-0002 follow-ups
	// for a v1.1.0d/v1.1.0e revisit.
	if d.client == nil || d.client.baseURL == "" {
		if d.s3Client == nil {
			return nil, &driverpkg.Error{
				Op:      "ListBuckets",
				Driver:  driverName,
				Err:     driverpkg.ErrUnsupported,
				Message: "neither admin client nor S3 client configured",
			}
		}
		out, err := d.s3Client.listBucketsS3(ctx)
		if err != nil {
			// Garage's S3 endpoint 404s on ListBuckets — translate
			// to an empty list so the UI can render the friendly
			// "no buckets / navigate by URL" empty state instead of
			// a scary error banner.
			if strings.Contains(err.Error(), "StatusCode: 404") || strings.Contains(err.Error(), "NotFound") {
				return []driverpkg.Bucket{}, nil
			}
			return nil, &driverpkg.Error{
				Op:      "ListBuckets",
				Driver:  driverName,
				Err:     driverpkg.ErrInvalid,
				Message: err.Error(),
			}
		}
		buckets := make([]driverpkg.Bucket, 0, len(out.Buckets))
		for _, b := range out.Buckets {
			name := ""
			if b.Name != nil {
				name = *b.Name
			}
			// At the region tier we don't have a separate bucket ID
			// (the S3 API only knows names). Use the name as the ID
			// AND as the only alias so downstream callers can route
			// /buckets/{bid}/objects requests to the same name.
			buckets = append(buckets, driverpkg.Bucket{
				ID:      name,
				Aliases: []string{name},
			})
		}
		return buckets, nil
	}

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

	if len(buckets) == 0 {
		return buckets, nil
	}

	const maxWorkers = 8
	sem := make(chan struct{}, maxWorkers)
	var mu sync.Mutex
	var errs []error

	var wg sync.WaitGroup
	wg.Add(len(buckets))

	for i := range buckets {
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			info, err := d.GetBucket(ctx, buckets[idx].ID)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("bucket %q: %w", buckets[idx].ID, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			buckets[idx].Objects = info.Objects
			buckets[idx].Bytes = info.Bytes
			buckets[idx].UnfinishedUploads = info.UnfinishedUploads
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if len(errs) > 0 {
		var errMsgs []string
		for _, e := range errs {
			errMsgs = append(errMsgs, e.Error())
		}
		return buckets, fmt.Errorf("ListBuckets fanout: %v", errors.Join(errs...))
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

// UpdateBucket updates a bucket's quotas and/or aliases.
//
// Quotas go via PUT /v1/bucket?id={id} (garage-admin-v1.yml:755-828);
// alias management goes through the dedicated PUT/DELETE
// /v1/bucket/alias/global endpoints (garage-admin-v1.yml:950-1017),
// since the PUT /bucket body does NOT carry globalAliases.
//
// When the caller passes `update.Aliases`, we diff against the bucket's
// current `globalAliases` (read via GetBucket) and fan out one PUT per
// net-new alias and one DELETE per dropped alias. Adds come before
// removes so a single-alias rename (drop "old", add "new") never
// momentarily leaves the bucket with zero aliases.
//
// Spec note (garage-admin-v1.yml:769-771): "In `quotas`: new values of
// `maxSize` and `maxObjects` must both be specified, or set to `null` to
// remove the quotas." We follow this — if Quotas is non-nil we always send
// both fields (using JSON null for ones the caller left as nil).
//
// BUG01 (v1.11.0.5 smoke): prior to v1.11.0.6 the v1 driver silently
// dropped `update.Aliases`, matching the v2 bug. Fixed here for parity
// per `feedback_driver_parity`.
func (d *driver) UpdateBucket(ctx context.Context, id string, update driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	// Step 1: send quota changes (if any). Same body shape as before.
	if update.Quotas != nil {
		body := updateBucketRequestV1{
			Quotas: &updateBucketQuotasV1{
				MaxSize:    update.Quotas.MaxSize,
				MaxObjects: update.Quotas.MaxObjects,
			},
		}
		path := fmt.Sprintf("/v1/bucket?id=%s", url.QueryEscape(id))
		var resp bucketInfoV1
		if err := d.client.do(ctx, "PUT", path, body, &resp); err != nil {
			return driverpkg.Bucket{}, err
		}
	}

	// Step 2: reconcile aliases via the dedicated /bucket/alias/global
	// endpoints. Compute the diff against current state — the v1 API
	// has no replace-all primitive, only per-alias PUT / DELETE.
	if update.Aliases != nil {
		desired := *update.Aliases

		var current bucketInfoV1
		getPath := fmt.Sprintf("/v1/bucket?id=%s", url.QueryEscape(id))
		if err := d.client.do(ctx, "GET", getPath, nil, &current); err != nil {
			return driverpkg.Bucket{}, err
		}

		toAdd, toRemove := diffAliasesV1(current.GlobalAliases, desired)

		for _, a := range toAdd {
			p := fmt.Sprintf("/v1/bucket/alias/global?id=%s&alias=%s",
				url.QueryEscape(id), url.QueryEscape(a))
			if err := d.client.do(ctx, "PUT", p, nil, nil); err != nil {
				return driverpkg.Bucket{}, err
			}
		}
		for _, a := range toRemove {
			p := fmt.Sprintf("/v1/bucket/alias/global?id=%s&alias=%s",
				url.QueryEscape(id), url.QueryEscape(a))
			if err := d.client.do(ctx, "DELETE", p, nil, nil); err != nil {
				return driverpkg.Bucket{}, err
			}
		}
	}

	// Step 3: re-fetch + return the canonical post-update bucket.
	var final bucketInfoV1
	finalPath := fmt.Sprintf("/v1/bucket?id=%s", url.QueryEscape(id))
	if err := d.client.do(ctx, "GET", finalPath, nil, &final); err != nil {
		return driverpkg.Bucket{}, err
	}
	return bucketFromInfo(final), nil
}

// diffAliasesV1 returns (toAdd, toRemove) for the alias reconciliation
// loop. Order-insensitive; preserves the desired-list order for adds
// and current-list order for removes so reconciliation is deterministic
// across retries. Duplicated from the v2 driver intentionally — keeping
// the helper local sidesteps a cross-package dep that the driver
// interface doesn't otherwise need.
func diffAliasesV1(current, desired []string) (toAdd, toRemove []string) {
	currentSet := make(map[string]struct{}, len(current))
	for _, a := range current {
		currentSet[a] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, a := range desired {
		desiredSet[a] = struct{}{}
	}
	for _, a := range desired {
		if _, ok := currentSet[a]; !ok {
			toAdd = append(toAdd, a)
		}
	}
	for _, a := range current {
		if _, ok := desiredSet[a]; !ok {
			toRemove = append(toRemove, a)
		}
	}
	return toAdd, toRemove
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
