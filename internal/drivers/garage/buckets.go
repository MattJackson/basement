// Package garage implements the garage device driver.
package garage

import (
	"context"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListBuckets returns all buckets in the cluster.
// Endpoint: GET /v2/ListBuckets (docs/garage-admin-api.md lines 252-259)
func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	var resp []listBucketsResponseItem
	if err := d.client.do(ctx, "ListBuckets", "/v2/ListBuckets", nil, &resp); err != nil {
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

// Response types for ListBuckets endpoint

type listBucketsResponseItem struct {
	ID             string   `json:"id"`
	Created        string   `json:"created"`
	GlobalAliases  []string `json:"globalAliases"`
	LocalAliases   []string `json:"localAliases"`
}
