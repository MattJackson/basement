package garage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListKeys returns all API access keys in the cluster.
// Endpoint: GET /v2/ListKeys (garage-admin-v2.json:1263-1286).
func (d *driver) ListKeys(ctx context.Context) ([]driverpkg.Key, error) {
	var resp []listKeysResponseItem
	if err := d.client.do(ctx, "GET", "/v2/ListKeys", nil, &resp); err != nil {
		return nil, err
	}

	keys := make([]driverpkg.Key, 0, len(resp))
	for _, k := range resp {
		key := driverpkg.Key{
			ID:          k.ID,
			Name:        k.Name,
			AccessKeyID: k.ID,
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// GetKey returns details for a specific access key by id.
// Endpoint: GET /v2/GetKeyInfo (garage-admin-v2.json:843-895).
func (d *driver) GetKey(ctx context.Context, id string) (driverpkg.Key, error) {
	path := fmt.Sprintf("/v2/GetKeyInfo?id=%s", url.QueryEscape(id))

	var resp getKeyInfoResponse
	if err := d.client.do(ctx, "GET", path, nil, &resp); err != nil {
		return driverpkg.Key{}, err
	}

	return keyFromInfo(resp), nil
}

// CreateKey creates a new API access key.
// Endpoint: POST /v2/CreateKey (garage-admin-v2.json:367-400).
func (d *driver) CreateKey(ctx context.Context, spec driverpkg.KeySpec) (driverpkg.Key, error) {
	body := createKeyRequest{Name: spec.Name}

	var resp getKeyInfoResponse
	if err := d.client.do(ctx, "POST", "/v2/CreateKey", body, &resp); err != nil {
		return driverpkg.Key{}, err
	}

	key := keyFromInfo(resp)
	// Surface the create-only secret. Garage returns it exactly here;
	// keyFromInfo drops it because GetKey/UpdateKeyPermissions paths
	// reuse the same converter but don't carry the secret.
	if resp.SecretAccessKey != nil && *resp.SecretAccessKey != "" {
		key.SecretAccessKey = resp.SecretAccessKey
	}
	return key, nil
}

// UpdateKeyPermissions sets per-bucket permissions for a key.
// Endpoints: POST /v2/AllowBucketKey and POST /v2/DenyBucketKey (garage-admin-v2.json:129-162, 526-559).
func (d *driver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driverpkg.BucketPermission) error {
	for _, p := range perms {
		// AllowBucketKey: set permissions that should be true (garage-admin-v2.json:129-162)
		allowReq := bucketPermChangeRequest{
			BucketID:    p.BucketID,
			AccessKeyID: keyID,
			Permissions: apiBucketKeyPerm{
				Read:  p.Read,
				Write: p.Write,
				Owner: p.Owner,
			},
		}

		var allowResp getBucketInfoResponse
		if err := d.client.do(ctx, "POST", "/v2/AllowBucketKey", allowReq, &allowResp); err != nil {
			return err
		}

		// DenyBucketKey: set permissions that should be false (garage-admin-v2.json:526-559)
		denyReq := bucketPermChangeRequest{
			BucketID:    p.BucketID,
			AccessKeyID: keyID,
			Permissions: apiBucketKeyPerm{
				Read:  !p.Read,
				Write: !p.Write,
				Owner: !p.Owner,
			},
		}

		var denyResp getBucketInfoResponse
		if err := d.client.do(ctx, "POST", "/v2/DenyBucketKey", denyReq, &denyResp); err != nil {
			return err
		}
	}
	return nil
}

// DeleteKey deletes an access key by id.
// Endpoint: POST /v2/DeleteKey (garage-admin-v2.json:498-525).
func (d *driver) DeleteKey(ctx context.Context, id string) error {
	path := fmt.Sprintf("/v2/DeleteKey?id=%s", url.QueryEscape(id))
	return d.client.do(ctx, "POST", path, nil, nil)
}

// keyFromInfo converts a GetKeyInfoResponse into a driver.Key.
// GetKeyInfoResponse schema: garage-admin-v2.json:2875-2930.
func keyFromInfo(resp getKeyInfoResponse) driverpkg.Key {
	buckets := make([]driverpkg.KeyBucketAccess, 0, len(resp.BucketsPermissions))
	for _, b := range resp.BucketsPermissions {
		buckets = append(buckets, driverpkg.KeyBucketAccess{
			BucketID:      b.BucketID,
			GlobalAliases: []string{},
			LocalAliases:  []string{},
			Read:          b.Read,
			Write:         b.Write,
			Owner:         b.Owner,
		})
	}

	key := driverpkg.Key{
		ID:                resp.ID,
		Name:              resp.Name,
		AccessKeyID:       resp.ID,
		Buckets:           buckets,
		Created:           resp.Created,
		AllowCreateBucket: resp.Permissions.CreateBucket,
	}

	if resp.Expiration != nil {
		key.Created = *resp.Expiration // Use expiration as proxy for created if needed
	}

	return key
}

// ===== v2 wire types for keys =====

// listKeysResponseItem mirrors ListKeysResponseItem (garage-admin-v2.json:3244-3270).
type listKeysResponseItem struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Created   *time.Time `json:"created,omitempty"`
	Expiration *time.Time `json:"expiration,omitempty"`
	Expired   bool       `json:"expired"`
}

// getKeyInfoResponse mirrors GetKeyInfoResponse (garage-admin-v2.json:2875-2930).
type getKeyInfoResponse struct {
	ID                 string              `json:"accessKeyId"`
	Name               string              `json:"name"`
	SecretAccessKey    *string             `json:"secretAccessKey,omitempty"`
	Created            time.Time           `json:"created,omitempty"`
	Expiration         *time.Time          `json:"expiration,omitempty"`
	Expired            bool                `json:"expired"`
	Permissions        keyPerm             `json:"permissions"`
	BucketsPermissions []bucketPermissionResp `json:"buckets"`
}

// bucketPermissionResp is used for tests and matches the buckets permissions structure.
type bucketPermissionResp struct {
	BucketID string `json:"id"`
	Read     bool   `json:"read"`
	Write    bool   `json:"write"`
	Owner    bool   `json:"owner"`
}

// keyInfoBucketResponse mirrors KeyInfoBucketResponse (garage-admin-v2.json:3490-3527).
type keyInfoBucketResponse struct {
	ID            string          `json:"id"`
	GlobalAliases []string        `json:"globalAliases"`
	LocalAliases  []string        `json:"localAliases"`
	Permissions   apiBucketKeyPerm `json:"permissions"`
}

// keyPerm mirrors KeyPerm (garage-admin-v2.json:3124-3131).
type keyPerm struct {
	CreateBucket bool `json:"createBucket,omitempty"`
}

// createKeyRequest mirrors CreateKeyRequest (garage-admin-v2.json:396-410).
type createKeyRequest struct {
	Name string `json:"name"`
}

// bucketPermChangeRequest mirrors BucketKeyPermChangeRequest (garage-admin-v2.json:1728-1745).
type bucketPermChangeRequest struct {
	BucketID    string          `json:"bucketId"`
	AccessKeyID string          `json:"accessKeyId"`
	Permissions apiBucketKeyPerm `json:"permissions"`
}

// allowBucketKeyRequest mirrors AllowBucketKeyRequest (garage-admin-v2.json:1730-1745).
type allowBucketKeyRequest struct {
	BucketID    string          `json:"bucketId"`
	AccessKeyID string          `json:"accessKeyId"`
	Permissions apiBucketKeyPerm `json:"permissions"`
}
