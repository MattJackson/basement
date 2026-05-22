package garage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListObjects lists objects in a bucket with optional prefix and pagination.
// delimiter="" returns a flat recursive listing; delimiter="/" returns
// folder-tier browsing with CommonPrefixes populated for sub-folders.
func (d *driver) ListObjects(ctx context.Context, bucket, prefix, continuation, delimiter string, limit int) (driverpkg.ObjectPage, error) {
	if d.s3Client == nil {
		return driverpkg.ObjectPage{}, &driverpkg.Error{
			Op:      "ListObjects",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	resp, err := d.s3Client.listObjectsV2(ctx, bucket, prefix, continuation, delimiter, limit)
	if err != nil {
		return driverpkg.ObjectPage{}, &driverpkg.Error{
			Op:      "ListObjects",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	objects := make([]driverpkg.ObjectInfo, 0, len(resp.Contents))
	for _, obj := range resp.Contents {
		info := driverpkg.ObjectInfo{
			Key:   keyFromPtr(obj.Key),
			Size:  sizeFromPtr(obj.Size),
			ETag:  etagFromPtr(obj.ETag),
			IsDir: false,
		}
		if obj.LastModified != nil {
			info.LastModified = *obj.LastModified
		}
		objects = append(objects, info)
	}

	commonPrefixes := make([]string, 0, len(resp.CommonPrefixes))
	for _, p := range resp.CommonPrefixes {
		if p.Prefix != nil {
			commonPrefixes = append(commonPrefixes, *p.Prefix)
		}
	}

	page := driverpkg.ObjectPage{
		Objects:        objects,
		CommonPrefixes: commonPrefixes,
		IsTruncated:    resp.IsTruncated != nil && *resp.IsTruncated,
	}

	if resp.NextContinuationToken != nil {
		page.NextContinuation = *resp.NextContinuationToken
	}

	return page, nil
}

// StatObject gets object metadata via HeadObject.
func (d *driver) StatObject(ctx context.Context, bucket, key string) (driverpkg.ObjectInfo, error) {
	if d.s3Client == nil {
		return driverpkg.ObjectInfo{}, &driverpkg.Error{
			Op:      "StatObject",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	resp, err := d.s3Client.headObject(ctx, bucket, key)
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return driverpkg.ObjectInfo{}, &driverpkg.Error{
				Op:      "StatObject",
				Driver:  driverName,
				Err:     driverpkg.ErrNotFound,
				Message: "object not found",
			}
		}

		return driverpkg.ObjectInfo{}, &driverpkg.Error{
			Op:      "StatObject",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	info := driverpkg.ObjectInfo{
	Key:    key,
	IsDir:  false,
	Size:   sizeFromPtr(resp.ContentLength),
	ETag:   etagFromPtr(resp.ETag),
}
	if resp.LastModified != nil {
		info.LastModified = *resp.LastModified
	}
	if resp.ContentType != nil {
		info.ContentType = *resp.ContentType
	}

	return info, nil
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
	if d.s3Client == nil {
		return &driverpkg.Error{
			Op:      "DeleteObject",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	err := d.s3Client.deleteObject(ctx, bucket, key)
	if err != nil {
		return &driverpkg.Error{
			Op:      "DeleteObject",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return nil
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

// StreamObject returns the object body as a ReadCloser plus headers.
func (d *driver) StreamObject(ctx context.Context, bucket, key, rng string) (driverpkg.StreamResult, error) {
	if d.s3Client == nil {
		return driverpkg.StreamResult{}, &driverpkg.Error{
			Op:      "StreamObject",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	if rng != "" {
		input.Range = aws.String(rng)
	}

	resp, err := d.s3Client.client.GetObject(ctx, input)
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return driverpkg.StreamResult{}, &driverpkg.Error{
				Op:      "StreamObject",
				Driver:  driverName,
				Err:     driverpkg.ErrNotFound,
				Message: "object not found",
			}
		}

		return driverpkg.StreamResult{}, &driverpkg.Error{
			Op:      "StreamObject",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	result := driverpkg.StreamResult{
		Body:          resp.Body,
		ContentType:   aws.ToString(resp.ContentType),
		ContentLength: aws.ToInt64(resp.ContentLength),
		ETag:          aws.ToString(resp.ETag),
	}

	if resp.LastModified != nil {
		result.LastModified = *resp.LastModified
	}

	return result, nil
}

// PutObjectStream writes the reader contents to the object.
func (d *driver) PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) (driverpkg.PutResult, error) {
	if d.s3Client == nil {
		return driverpkg.PutResult{}, &driverpkg.Error{
			Op:      "PutObjectStream",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        reader,
		ContentType: aws.String(contentType),
	}

	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}

	resp, err := d.s3Client.client.PutObject(ctx, input)
	if err != nil {
		return driverpkg.PutResult{}, &driverpkg.Error{
			Op:      "PutObjectStream",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return driverpkg.PutResult{
		ETag: aws.ToString(resp.ETag),
	}, nil
}

// ServerSideCopy copies an object from (srcBucket, srcKey) to (dstBucket, dstKey)
// using Garage's S3-compatible CopyObject API for same-backend optimization.
func (d *driver) ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if d.s3Client == nil {
		return &driverpkg.Error{
			Op:      "ServerSideCopy",
			Driver:  driverName,
			Err:     driverpkg.ErrUnsupported,
			Message: "S3 endpoint not configured — set s3_endpoint in connection config",
		}
	}

	input := &s3.CopyObjectInput{
		CopySource:      aws.String(fmt.Sprintf("%s/%s", srcBucket, url.QueryEscape(srcKey))),
		Bucket:          aws.String(dstBucket),
		Key:             aws.String(dstKey),
	}

	_, err := d.s3Client.client.CopyObject(ctx, input)
	if err != nil {
		return &driverpkg.Error{
			Op:      "ServerSideCopy",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return nil
}

// Helper functions for pointer dereferencing.
func keyFromPtr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

func sizeFromPtr(i *int64) int64 {
	if i != nil {
		return *i
	}
	return 0
}

func etagFromPtr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}
