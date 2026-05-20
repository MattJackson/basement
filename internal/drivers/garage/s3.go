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
