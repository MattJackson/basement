// Package garage implements the garage device driver.
package garage

import (
	"context"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func init() {
	driverpkg.Register("garage", newDriver)
}

type driver struct {
	client *client
}

func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	return &driver{
		client: newClient(cfg),
	}, nil
}

func (d *driver) unsupported(op string) error {
	return &driverpkg.Error{
		Op:      op,
		Driver:  "garage",
		Err:     driverpkg.ErrUnsupported,
		Message: "not implemented yet",
	}
}

// StageLayout stages a layout change by calling POST /v2/UpdateClusterLayout.
// IMPORTANT: Each call to StageLayout REPLACES the entire staged layout with the provided changes.
// It does not accumulate across calls. Callers should build the complete LayoutDiff they want,
// then convert it to a single UpdateClusterLayout request body. This function handles one node change at a time,
// so callers typically need to call GetLayout first, apply their desired modifications to the full layout,
// and pass those as multiple StageLayout calls (each replacing staged state).
// For simpler usage: if you want to modify just one node, call this with that single change.
// Endpoint: POST /v2/UpdateClusterLayout (docs/garage-admin-api.md lines 162-169)
func (d *driver) StageLayout(ctx context.Context, change driverpkg.LayoutChange) (driverpkg.LayoutDiff, error) {
	var newRole *layoutNodeRole

	if change.Zone != nil || change.Capacity != nil || len(change.Tags) > 0 {
		newRole = &layoutNodeRole{
			Zone:     *change.Zone,
			Tags:     change.Tags,
			Capacity: change.Capacity,
		}

		if change.Role != nil && *change.Role == "gateway" {
			gw := true
			newRole.Gateway = &gw
		} else if change.Role != nil && *change.Role == "storage" {
			gw := false
			newRole.Gateway = &gw
		}
	}

	updateReq := updateClusterLayoutRequest{
		NodeId:  change.NodeID,
		NewRole: newRole,
		Remove:  false,
	}

	var resp getClusterLayoutResponse
	if err := d.client.do(ctx, "POST", "/v2/UpdateClusterLayout", updateReq, &resp); err != nil {
		return driverpkg.LayoutDiff{}, err
	}

	diff := computeLayoutDiff(resp.Version, resp.Roles)
	return diff, nil
}

// ApplyLayout applies the currently staged layout changes.
// It first fetches the current layout version from GetClusterLayout, then calls POST /v2/ApplyClusterLayout
// with version = current + 1 as a safety assertion. If Garage returns 409 (version mismatch), this maps to ErrConflict.
// Endpoint: GET /v2/GetClusterLayout and POST /v2/ApplyClusterLayout (docs/garage-admin-api.md lines 126-133, 99-106)
func (d *driver) ApplyLayout(ctx context.Context) error {
	// First get current layout version
	var currentResp getClusterLayoutResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterLayout", nil, &currentResp); err != nil {
		return err
	}

	newVersion := currentResp.Version + 1

	reqBody := applyClusterLayoutRequest{
		Version: newVersion,
	}

	var resp applyClusterLayoutResponse
	if err := d.client.do(ctx, "POST", "/v2/ApplyClusterLayout", reqBody, &resp); err != nil {
		return err
	}

	return nil
}

// RevertLayout discards all staged layout changes by calling POST /v2/RevertClusterLayout.
// It first fetches the current layout version and includes it in the request as a safety assertion.
// If Garage returns 409 (version mismatch), this maps to ErrConflict.
// Endpoint: POST /v2/RevertClusterLayout (docs/garage-admin-api.md lines 153-160)
func (d *driver) RevertLayout(ctx context.Context) error {
	// Get current layout version for assertion
	var currentResp getClusterLayoutResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterLayout", nil, &currentResp); err != nil {
		return err
	}

	reqBody := revertClusterLayoutRequest{
		Version: currentResp.Version,
	}

	var resp revertClusterLayoutResponse
	if err := d.client.do(ctx, "POST", "/v2/RevertClusterLayout", reqBody, &resp); err != nil {
		return err
	}

	return nil
}

// computeLayoutDiff computes the diff between current and a given layout.
// NOTE: This is a simplified placeholder - actual implementation should compare current vs staged roles.
func computeLayoutDiff(version int64, roles []layoutNodeRole) driverpkg.LayoutDiff {
	return driverpkg.LayoutDiff{}
}

func (d *driver) ListObjects(ctx context.Context, bucket, prefix, continuation string, limit int) (driverpkg.ObjectPage, error) {
	return driverpkg.ObjectPage{}, d.unsupported("ListObjects")
}

func (d *driver) StatObject(ctx context.Context, bucket, key string) (driverpkg.ObjectInfo, error) {
	return driverpkg.ObjectInfo{}, d.unsupported("StatObject")
}

func (d *driver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignGet")
}

func (d *driver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignPut")
}

func (d *driver) DeleteObject(ctx context.Context, bucket, key string) error {
	return d.unsupported("DeleteObject")
}

func (d *driver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driverpkg.MultipartUpload, error) {
	return driverpkg.MultipartUpload{}, d.unsupported("CreateMultipart")
}

func (d *driver) PresignUploadPart(ctx context.Context, upload driverpkg.MultipartUpload, partNum int) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignUploadPart")
}

func (d *driver) CompleteMultipart(ctx context.Context, upload driverpkg.MultipartUpload, parts []driverpkg.CompletedPart) error {
	return d.unsupported("CompleteMultipart")
}

func (d *driver) AbortMultipart(ctx context.Context, upload driverpkg.MultipartUpload) error {
	return d.unsupported("AbortMultipart")
}
