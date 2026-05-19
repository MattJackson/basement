package garage_v1

import (
	"context"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// The Garage v1 admin API does not include S3 data-plane operations; those
// are spoken on a separate S3-compatible endpoint. The methods below are
// stubbed exactly like the v2 driver's S3 stubs and will be replaced with
// real implementations against aws-sdk-go-v2 in a follow-up task.

// ListObjects is not yet implemented.
func (d *driver) ListObjects(_ context.Context, _, _, _ string, _ int) (driverpkg.ObjectPage, error) {
	return driverpkg.ObjectPage{}, d.unsupported("ListObjects")
}

// StatObject is not yet implemented.
func (d *driver) StatObject(_ context.Context, _, _ string) (driverpkg.ObjectInfo, error) {
	return driverpkg.ObjectInfo{}, d.unsupported("StatObject")
}

// PresignGet is not yet implemented.
func (d *driver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignGet")
}

// PresignPut is not yet implemented.
func (d *driver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignPut")
}

// DeleteObject is not yet implemented.
func (d *driver) DeleteObject(_ context.Context, _, _ string) error {
	return d.unsupported("DeleteObject")
}

// CreateMultipart is not yet implemented.
func (d *driver) CreateMultipart(_ context.Context, _, _, _ string) (driverpkg.MultipartUpload, error) {
	return driverpkg.MultipartUpload{}, d.unsupported("CreateMultipart")
}

// PresignUploadPart is not yet implemented.
func (d *driver) PresignUploadPart(_ context.Context, _ driverpkg.MultipartUpload, _ int) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignUploadPart")
}

// CompleteMultipart is not yet implemented.
func (d *driver) CompleteMultipart(_ context.Context, _ driverpkg.MultipartUpload, _ []driverpkg.CompletedPart) error {
	return d.unsupported("CompleteMultipart")
}

// AbortMultipart is not yet implemented.
func (d *driver) AbortMultipart(_ context.Context, _ driverpkg.MultipartUpload) error {
	return d.unsupported("AbortMultipart")
}
