package garage_v1

import (
	"context"
	"fmt"
	"net/url"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListKeys returns all API access keys in the cluster.
// Endpoint: GET /v1/key?list (garage-admin-v1.yml:337-367).
//
// The v1 list response is a minimal {id, name} array
// (garage-admin-v1.yml:360-367); created/expiration timestamps are NOT
// included — Key.Created is therefore left as the zero time.
func (d *driver) ListKeys(ctx context.Context) ([]driverpkg.Key, error) {
	var resp []listKeysItemV1
	if err := d.client.do(ctx, "GET", "/v1/key?list", nil, &resp); err != nil {
		return nil, err
	}

	keys := make([]driverpkg.Key, 0, len(resp))
	for _, k := range resp {
		keys = append(keys, driverpkg.Key{
			ID:          k.ID,
			Name:        k.Name,
			AccessKeyID: k.ID,
		})
	}
	return keys, nil
}

// GetKey returns details for a specific access key by id.
// Endpoint: GET /v1/key?id={id} (garage-admin-v1.yml:403-454).
//
// The "id" query parameter is documented at garage-admin-v1.yml:415-423.
// We do NOT set showSecretKey here; if downstream code needs the secret
// it should be retrieved from CreateKey's response at key-generation time
// (the secret is returned only on create per garage-admin-v1.yml:380).
func (d *driver) GetKey(ctx context.Context, id string) (driverpkg.Key, error) {
	path := fmt.Sprintf("/v1/key?id=%s", url.QueryEscape(id))

	var resp keyInfoV1
	if err := d.client.do(ctx, "GET", path, nil, &resp); err != nil {
		return driverpkg.Key{}, err
	}

	return keyFromInfo(resp), nil
}

// CreateKey creates a new API access key.
// Endpoint: POST /v1/key (body {name}) (garage-admin-v1.yml:368-401).
//
// The secret access key IS returned in the response on this endpoint per
// garage-admin-v1.yml:380. We copy it into Key.AccessKeyID for the caller.
func (d *driver) CreateKey(ctx context.Context, spec driverpkg.KeySpec) (driverpkg.Key, error) {
	// Spec groups AddKey under "/key?list" (garage-admin-v1.yml:337,368).
	// Garage accepts POST to /v1/key for key creation; we follow the
	// task-spec path here. If a deployment 404s on /v1/key the operator
	// should fall back to /v1/key?list.
	body := createKeyRequestV1{Name: spec.Name}

	var resp keyInfoV1
	if err := d.client.do(ctx, "POST", "/v1/key", body, &resp); err != nil {
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
// Endpoints: POST /v1/bucket/allow and POST /v1/bucket/deny
// (garage-admin-v1.yml:830-888, 890-948).
//
// Garage v1 puts permissions on the BUCKET-KEY EDGE — there's no single
// "update key permissions" call. We model the supplied perms list as one
// allow + one deny call per (key, bucket) pair, sending the bits that
// should be set in allow.permissions and the complementary bits in
// deny.permissions.
//
// IMPORTANT semantics (garage-admin-v1.yml:837-845, 897-905): both endpoints
// take a flag-per-bit; true means "act on this bit", false means "leave
// unchanged". So to flip a permission OFF you must call /bucket/deny with
// that flag set to true.
//
// This driver issues, for each permission entry:
//   1. POST /v1/bucket/allow with the bits the caller wants ON
//   2. POST /v1/bucket/deny  with the bits the caller wants OFF
//
// If both calls would be no-ops (all three bits unchanged) we still send
// allow with all-false; Garage treats that as a noop. We never skip the
// deny call so that toggling-off works.
//
// OPEN: this writes the full perm state per call. We do NOT compare against
// existing permissions because the driver interface gives no read-modify-
// write cursor. Callers that need atomicity should serialize updates.
func (d *driver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driverpkg.BucketPermission) error {
	for _, p := range perms {
		allow := bucketPermChangeV1{
			BucketID:    p.BucketID,
			AccessKeyID: keyID,
			Permissions: bucketKeyPermV1{
				Read:  p.Read,
				Write: p.Write,
				Owner: p.Owner,
			},
		}
		if err := d.client.do(ctx, "POST", "/v1/bucket/allow", allow, nil); err != nil {
			return err
		}

		deny := bucketPermChangeV1{
			BucketID:    p.BucketID,
			AccessKeyID: keyID,
			Permissions: bucketKeyPermV1{
				Read:  !p.Read,
				Write: !p.Write,
				Owner: !p.Owner,
			},
		}
		if err := d.client.do(ctx, "POST", "/v1/bucket/deny", deny, nil); err != nil {
			return err
		}
	}
	return nil
}

// DeleteKey deletes an access key by id.
// Endpoint: DELETE /v1/key?id={id} (garage-admin-v1.yml:455-475).
//
// Buckets the key had access to are NOT auto-deleted; they remain dangling
// until the operator removes them (garage-admin-v1.yml:461).
func (d *driver) DeleteKey(ctx context.Context, id string) error {
	path := fmt.Sprintf("/v1/key?id=%s", url.QueryEscape(id))
	return d.client.do(ctx, "DELETE", path, nil, nil)
}

// keyFromInfo converts a KeyInfo response into a driver.Key.
// KeyInfo schema: garage-admin-v1.yml:1228-1276.
//
// Note: the response may carry a secretAccessKey (set only on the create
// path per garage-admin-v1.yml:380-381), but driver.Key has no field for
// it — the secret is intentionally not part of the persisted model. Code
// that needs to capture the secret on create must do so at the call site
// before passing the response through this helper.
func keyFromInfo(resp keyInfoV1) driverpkg.Key {
	buckets := make([]driverpkg.KeyBucketAccess, 0, len(resp.Buckets))
	for _, b := range resp.Buckets {
		buckets = append(buckets, driverpkg.KeyBucketAccess{
			BucketID:      b.ID,
			GlobalAliases: b.GlobalAliases,
			LocalAliases:  b.LocalAliases,
			Read:          b.Permissions.Read,
			Write:         b.Permissions.Write,
			Owner:         b.Permissions.Owner,
		})
	}

	return driverpkg.Key{
		ID:                resp.AccessKeyID,
		Name:              resp.Name,
		AccessKeyID:       resp.AccessKeyID,
		AllowCreateBucket: resp.Permissions.CreateBucket,
		Buckets:           buckets,
		// KeyInfo (garage-admin-v1.yml:1228-1276) has no created timestamp.
		Created: time.Time{},
	}
}

// ===== v1 wire types =====

// listKeysItemV1 mirrors the items returned by GET /key?list
// (garage-admin-v1.yml:360-367).
type listKeysItemV1 struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// createKeyRequestV1 is the body of POST /v1/key
// (garage-admin-v1.yml:382-390).
type createKeyRequestV1 struct {
	Name string `json:"name"`
}

// keyInfoV1 mirrors KeyInfo (garage-admin-v1.yml:1228-1276).
type keyInfoV1 struct {
	Name            string            `json:"name"`
	AccessKeyID     string            `json:"accessKeyId"`
	SecretAccessKey *string           `json:"secretAccessKey,omitempty"`
	Permissions     keyInfoPermsV1    `json:"permissions"`
	Buckets         []keyInfoBucketV1 `json:"buckets,omitempty"`
}

type keyInfoPermsV1 struct {
	CreateBucket bool `json:"createBucket"`
}

type keyInfoBucketV1 struct {
	ID            string          `json:"id"`
	GlobalAliases []string        `json:"globalAliases,omitempty"`
	LocalAliases  []string        `json:"localAliases,omitempty"`
	Permissions   bucketKeyPermV1 `json:"permissions"`
}

// bucketPermChangeV1 is the request body shared by POST /v1/bucket/allow
// and POST /v1/bucket/deny (garage-admin-v1.yml:855-875, 915-935).
type bucketPermChangeV1 struct {
	BucketID    string          `json:"bucketId"`
	AccessKeyID string          `json:"accessKeyId"`
	Permissions bucketKeyPermV1 `json:"permissions"`
}
