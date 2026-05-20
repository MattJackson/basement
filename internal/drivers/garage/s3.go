package garage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

// CreateMultipart starts a multipart upload and returns the upload ID.
func (d *driver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driverpkg.MultipartUpload, error) {
	if d.s3Client == nil {
		return driverpkg.MultipartUpload{}, &driverpkg.Error{
			Op:      "CreateMultipart",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	resp, err := d.s3Client.createMultipartUpload(ctx, bucket, key, contentType)
	if err != nil {
		return driverpkg.MultipartUpload{}, &driverpkg.Error{
			Op:      "CreateMultipart",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	mu := driverpkg.MultipartUpload{
		UploadID: *resp.UploadId,
		Bucket:   bucket,
		Key:      key,
	}

	if contentType != "" {
		mu.ContentType = contentType
	}

	return mu, nil
}

// PresignUploadPart creates a presigned URL for uploading a specific part.
func (d *driver) PresignUploadPart(ctx context.Context, upload driverpkg.MultipartUpload, partNum int) (driverpkg.PresignedURL, error) {
	if d.s3Client == nil {
		return driverpkg.PresignedURL{}, &driverpkg.Error{
			Op:      "PresignUploadPart",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	url, err := d.s3Client.presignUploadPart(ctx, upload.Bucket, upload.Key, upload.UploadID, partNum, 15*time.Minute)
	if err != nil {
		return driverpkg.PresignedURL{}, &driverpkg.Error{
			Op:      "PresignUploadPart",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	now := time.Now()
	expires := now.Add(15 * time.Minute)

	return driverpkg.PresignedURL{
		URL:     url,
		Expires: expires,
		Method:  "PUT",
	}, nil
}

// CompleteMultipartUpload completes a multipart upload with all parts.
func (d *driver) CompleteMultipart(ctx context.Context, upload driverpkg.MultipartUpload, parts []driverpkg.CompletedPart) error {
	if d.s3Client == nil {
		return &driverpkg.Error{
			Op:      "CompleteMultipart",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	s3Parts := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		s3Parts[i] = types.CompletedPart{
			PartNumber: aws.Int32(int32(p.PartNumber)),
			ETag:       aws.String(p.ETag),
		}
	}

	_, err := d.s3Client.completeMultipartUpload(ctx, upload.Bucket, upload.Key, upload.UploadID, s3Parts)
	if err != nil {
		return &driverpkg.Error{
			Op:      "CompleteMultipart",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return nil
}

// AbortMultipartUpload cancels an in-progress multipart upload.
func (d *driver) AbortMultipart(ctx context.Context, upload driverpkg.MultipartUpload) error {
	if d.s3Client == nil {
		return &driverpkg.Error{
			Op:      "AbortMultipart",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	err := d.s3Client.abortMultipartUpload(ctx, upload.Bucket, upload.Key, upload.UploadID)
	if err != nil {
		return &driverpkg.Error{
			Op:      "AbortMultipart",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return nil
}
