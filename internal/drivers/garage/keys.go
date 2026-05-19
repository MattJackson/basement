// Package garage implements the garage device driver.
package garage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListKeys returns all access keys in the cluster.
// Endpoint: GET /v2/ListKeys (docs/garage-admin-api.md lines 316-323)
func (d *driver) ListKeys(ctx context.Context) ([]driverpkg.Key, error) {
	var resp []listKeysResponseItem
	if err := d.client.do(ctx, "GET", "/v2/ListKeys", nil, &resp); err != nil {
		return nil, err
	}

	keys := make([]driverpkg.Key, 0, len(resp))
	for _, k := range resp {
		var createdTime time.Time
		if k.Created != "" {
			createdTime, _ = time.Parse(time.RFC3339, k.Created)
		}

		key := driverpkg.Key{
			ID:                k.ID,
			Name:              k.Name,
			Created:           createdTime,
			AllowCreateBucket: false, // Not available from ListKeys endpoint
		}

		keys = append(keys, key)
	}

	return keys, nil
}

// GetKey returns details for a specific access key by ID.
// Endpoint: GET /v2/GetKeyInfo?id={id} (docs/garage-admin-api.md lines 297-305)
func (d *driver) GetKey(ctx context.Context, id string) (driverpkg.Key, error) {
	var resp getKeyInfoResponse
	if err := d.client.do(ctx, "GET", "/v2/GetKeyInfo?id="+url.QueryEscape(id), nil, &resp); err != nil {
		return driverpkg.Key{}, err
	}

	key := driverpkg.Key{
		ID:                resp.ID,
		Name:              resp.Name,
		Created:           time.Now(), // Created not returned by default per spec
		AllowCreateBucket: false,      // Not returned by GetKeyInfo
	}

	if resp.SecretAccessKey != nil {
		key.AccessKeyID = *resp.SecretAccessKey
	} else if resp.ID != "" {
		key.AccessKeyID = resp.ID
	}

	return key, nil
}

// CreateKey creates a new API access key with the specified name.
// Endpoint: POST /v2/CreateKey (docs/garage-admin-api.md lines 278-285)
func (d *driver) CreateKey(ctx context.Context, spec driverpkg.KeySpec) (driverpkg.Key, error) {
	reqBody := createKeyRequest{
		Name: spec.Name,
	}

	var resp getKeyInfoResponse
	if err := d.client.do(ctx, "POST", "/v2/CreateKey", reqBody, &resp); err != nil {
		return driverpkg.Key{}, err
	}

	key := driverpkg.Key{
		ID:                resp.ID,
		Name:              resp.Name,
		Created:           time.Now(), // Created not returned by default per spec
		AllowCreateBucket: false,     // Not returned by CreateKey response
	}

	if resp.SecretAccessKey != nil {
		key.AccessKeyID = *resp.SecretAccessKey
	} else {
		key.AccessKeyID = resp.ID
	}

	return key, nil
}

// UpdateKeyPermissions updates the permissions for a key on multiple buckets.
// This method calls AllowBucketKey or DenyBucketKey for each bucket based on the desired permissions.
// OPEN: The diff-allow-deny logic assumes we have full previous state to compare against.
// If caller doesn't provide previous permissions, this only sets the new ones without removing old grants.
// Endpoint: POST /v2/AllowBucketKey and POST /v2/DenyBucketKey (docs/garage-admin-api.md lines 187-194, 234-241)
func (d *driver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driverpkg.BucketPermission) error {
	// For each desired permission entry, call AllowBucketKey with the flags set appropriately.
	// Note: Garage's semantics are unconventional - true activates, false keeps previous value.
	// So to grant read/write/owner, we set those flags to true in AllowBucketKey.
	for _, p := range perms {
		permChange := allowBucketKeyRequest{
			BucketId:    p.BucketID,
			AccessKeyId: keyID,
			Permissions: apiBucketKeyPerm{
				Read:  p.Read,
				Write: p.Write,
				Owner: p.Owner,
			},
		}

		if err := d.client.do(ctx, "POST", "/v2/AllowBucketKey", permChange, nil); err != nil {
			return err
		}
	}

	return nil
}

// DeleteKey deletes an access key by ID.
// Endpoint: POST /v2/DeleteKey?id={id} (docs/garage-admin-api.md lines 287-295)
func (d *driver) DeleteKey(ctx context.Context, id string) error {
	path := fmt.Sprintf("/v2/DeleteKey?id=%s", url.QueryEscape(id))
	return d.client.do(ctx, "POST", path, nil, nil)
}

// Response types for ListKeys endpoint

type listKeysResponseItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Created    string `json:"created,omitempty"`
	Expiration string `json:"expiration,omitempty"`
	Expired    bool   `json:"expired"`
}

// Request/Response types for GetKey, CreateKey, UpdateKeyPermissions, DeleteKey

type getKeyInfoResponse struct {
	ID                string            `json:"id"`
	SecretAccessKey   *string           `json:"secretAccessKey,omitempty"`
	Name              string            `json:"name"`
	BucketsPermissions []bucketPermissionResp `json:"bucketsPermissions"`
}

type bucketPermissionResp struct {
	BucketID        string          `json:"bucketId"`
	Read            bool            `json:"read"`
	Write           bool            `json:"write"`
	Owner           bool            `json:"owner"`
	BucketLocalAliases []bucketLocalAlias `json:"bucketLocalAliases,omitempty"`
}

type createKeyRequest struct {
	Name          string  `json:"name"`
	AccessKeyId   *string `json:"accessKeyId,omitempty"`
}

type allowBucketKeyRequest struct {
	BucketId    string        `json:"bucketId"`
	AccessKeyId string        `json:"accessKeyId"`
	Permissions apiBucketKeyPerm `json:"permissions"`
}

type apiBucketKeyPerm struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
	Owner bool `json:"owner"`
}
