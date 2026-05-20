package garage

import (
	"context"
	"fmt"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListObjects lists objects in a bucket.
func (d *driver) ListObjects(ctx context.Context, bucket, prefix, continuation string, limit int) (driverpkg.ObjectPage, error) {
	if d.s3Endpoint == "" {
		return driverpkg.ObjectPage{}, fmt.Errorf("S3 endpoint not configured")
	}

	return driverpkg.ObjectPage{}, d.unsupported("ListObjects")
}

// StatObject returns metadata about an object.
func (d *driver) StatObject(ctx context.Context, bucket, key string) (driverpkg.ObjectInfo, error) {
	if d.s3Endpoint == "" {
		return driverpkg.ObjectInfo{}, fmt.Errorf("S3 endpoint not configured")
	}

	return driverpkg.ObjectInfo{}, d.unsupported("StatObject")
}

// PresignGet returns a presigned URL for GET operations.
func (d *driver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driverpkg.PresignedURL, error) {
	if d.s3Endpoint == "" {
		return driverpkg.PresignedURL{}, fmt.Errorf("S3 endpoint not configured")
	}

	return driverpkg.PresignedURL{}, d.unsupported("PresignGet")
}

// PresignPut returns a presigned URL for PUT operations.
func (d *driver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driverpkg.PresignedURL, error) {
	if d.s3Endpoint == "" {
		return driverpkg.PresignedURL{}, fmt.Errorf("S3 endpoint not configured")
	}

	return driverpkg.PresignedURL{}, d.unsupported("PresignPut")
}

// DeleteObject deletes an object from a bucket.
func (d *driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if d.s3Endpoint == "" {
		return fmt.Errorf("S3 endpoint not configured")
	}

	return d.unsupported("DeleteObject")
}

// CreateMultipart initializes a multipart upload.
func (d *driver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driverpkg.MultipartUpload, error) {
	if d.s3Endpoint == "" {
		return driverpkg.MultipartUpload{}, fmt.Errorf("S3 endpoint not configured")
	}

	return driverpkg.MultipartUpload{}, d.unsupported("CreateMultipart")
}

// PresignUploadPart returns a presigned URL for uploading a part.
func (d *driver) PresignUploadPart(ctx context.Context, upload driverpkg.MultipartUpload, partNum int) (driverpkg.PresignedURL, error) {
	if d.s3Endpoint == "" {
		return driverpkg.PresignedURL{}, fmt.Errorf("S3 endpoint not configured")
	}

	return driverpkg.PresignedURL{}, d.unsupported("PresignUploadPart")
}

// CompleteMultipart completes a multipart upload.
func (d *driver) CompleteMultipart(ctx context.Context, upload driverpkg.MultipartUpload, parts []driverpkg.CompletedPart) error {
	if d.s3Endpoint == "" {
		return fmt.Errorf("S3 endpoint not configured")
	}

	return d.unsupported("CompleteMultipart")
}

// AbortMultipart aborts a multipart upload.
func (d *driver) AbortMultipart(ctx context.Context, upload driverpkg.MultipartUpload) error {
	if d.s3Endpoint == "" {
		return fmt.Errorf("S3 endpoint not configured")
	}

	return d.unsupported("AbortMultipart")
}
