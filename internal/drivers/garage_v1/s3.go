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

// PresignGet returns a presigned GET URL for an object.
func (d *driver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driverpkg.PresignedURL, error) {
	if d.s3Client == nil {
		return driverpkg.PresignedURL{}, &driverpkg.Error{
			Op:      "PresignGet",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	url, err := d.s3Client.presignGetObject(ctx, bucket, key, ttl)
	if err != nil {
		return driverpkg.PresignedURL{}, &driverpkg.Error{
			Op:      "PresignGet",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	now := time.Now()
	expires := now.Add(ttl)

	return driverpkg.PresignedURL{
		URL:     url,
		Expires: expires,
		Method:  "GET",
	}, nil
}

// PresignPut returns a presigned PUT URL for an object.
func (d *driver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driverpkg.PresignedURL, error) {
	if d.s3Client == nil {
		return driverpkg.PresignedURL{}, &driverpkg.Error{
			Op:      "PresignPut",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	url, err := d.s3Client.presignPutObject(ctx, bucket, key, ttl, contentType)
	if err != nil {
		return driverpkg.PresignedURL{}, &driverpkg.Error{
			Op:      "PresignPut",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	now := time.Now()
	expires := now.Add(ttl)

	return driverpkg.PresignedURL{
		URL:     url,
		Expires: expires,
		Method:  "PUT",
	}, nil
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
