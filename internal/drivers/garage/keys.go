// Package garage implements the garage device driver.
package garage

import (
	"context"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListKeys returns all access keys in the cluster.
// Endpoint: GET /v2/ListKeys (docs/garage-admin-api.md lines 316-323)
func (d *driver) ListKeys(ctx context.Context) ([]driverpkg.Key, error) {
	var resp []listKeysResponseItem
	if err := d.client.do(ctx, "ListKeys", "/v2/ListKeys", nil, &resp); err != nil {
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

// Response types for ListKeys endpoint

type listKeysResponseItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Created    string `json:"created,omitempty"`
	Expiration string `json:"expiration,omitempty"`
	Expired    bool   `json:"expired"`
}
