package aws_s3

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// s3Client wraps the AWS S3 client and provides a convenient interface for
// driver operations. It handles SigV4 signing automatically via the SDK.
type s3Client struct {
	client *s3.Client
}

// newS3Client creates an S3 client from the driver config. Config keys:
//   - "region": AWS region (required, e.g., "us-east-1")
//   - "access_key": AWS access key ID (required)
//   - "secret_key": AWS secret access key (required)
//   - "endpoint": optional S3-compatible endpoint URL for non-AWS backends
//
// Delegates to driver.NewS3PathStyleClient so the path-style guarantee
// (see helper doc) lives in one place across all four drivers. AWS S3
// accepts path-style on every region; self-hosted S3 backends (Garage,
// IP-addressed MinIO) require it.
func newS3Client(cfg map[string]string) (*s3Client, error) {
	region := cfg["region"]
	if region == "" {
		return nil, fmt.Errorf("missing required config key: region")
	}

	accessKey := cfg["access_key"]
	if accessKey == "" {
		return nil, fmt.Errorf("missing required config key: access_key")
	}

	secretKey := cfg["secret_key"]
	if secretKey == "" {
		return nil, fmt.Errorf("missing required config key: secret_key")
	}

	client, err := driverpkg.NewS3PathStyleClient(cfg["endpoint"], accessKey, secretKey, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	return &s3Client{client: client}, nil
}

// listBuckets calls ListBuckets on the S3 client.
func (c *s3Client) listBuckets(ctx context.Context) (*s3.ListBucketsOutput, error) {
	return c.client.ListBuckets(ctx, &s3.ListBucketsInput{})
}

// headBucket calls HeadBucket to check if a bucket exists.
func (c *s3Client) headBucket(ctx context.Context, bucket string) (*s3.HeadBucketOutput, error) {
	return c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
}

// createBucket creates a new S3 bucket.
func (c *s3Client) createBucket(ctx context.Context, bucket string, region string) (*s3.CreateBucketOutput, error) {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	}

	// Note: For regions other than us-east-1, we would need to specify
	// CreateBucketConfiguration.LocationConstraint. However, this requires
	// the client to be configured for that region upfront. For simplicity,
	// we let AWS handle default region behavior here.
	_ = region // region is already used in client creation

	return c.client.CreateBucket(ctx, input)
}

// deleteBucket deletes an S3 bucket (must be empty).
func (c *s3Client) deleteBucket(ctx context.Context, bucket string) error {
	_, err := c.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	return err
}

// listObjectsV2 lists objects in a bucket with optional prefix and pagination.
// Passing a non-empty delimiter (typically "/") turns flat listing into
// folder-tier browsing: Contents are objects directly under prefix and
// CommonPrefixes are sub-folder prefixes.
func (c *s3Client) listObjectsV2(ctx context.Context, bucket, prefix, continuation, delimiter string, limit int) (*s3.ListObjectsV2Output, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	if continuation != "" {
		input.ContinuationToken = aws.String(continuation)
	}
	if delimiter != "" {
		input.Delimiter = aws.String(delimiter)
	}
	if limit > 0 {
		input.MaxKeys = aws.Int32(int32(limit))
	}

	return c.client.ListObjectsV2(ctx, input)
}

// headObject gets object metadata.
func (c *s3Client) headObject(ctx context.Context, bucket, key string) (*s3.HeadObjectOutput, error) {
	return c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
}

// deleteObject deletes an object from a bucket.
func (c *s3Client) deleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// createMultipartUpload starts a multipart upload.
func (c *s3Client) createMultipartUpload(ctx context.Context, bucket, key, contentType string) (*s3.CreateMultipartUploadOutput, error) {
	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	return c.client.CreateMultipartUpload(ctx, input)
}

// presignGetObject creates a presigned GET URL.
func (c *s3Client) presignGetObject(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.client)
	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))

	if err != nil {
		return "", err
	}

	return req.URL, nil
}

// presignPutObject creates a presigned PUT URL.
func (c *s3Client) presignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (string, error) {
	presignClient := s3.NewPresignClient(c.client)

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	req, err := presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(ttl))

	if err != nil {
		return "", err
	}

	return req.URL, nil
}

// completeMultipartUpload completes a multipart upload with all parts.
func (c *s3Client) completeMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []types.CompletedPart) (*s3.CompleteMultipartUploadOutput, error) {
	input := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: parts,
		},
	}

	return c.client.CompleteMultipartUpload(ctx, input)
}

// abortMultipartUpload cancels a multipart upload.
func (c *s3Client) abortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	_, err := c.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return err
}

// presignUploadPart creates a presigned URL for uploading a part.
func (c *s3Client) presignUploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, ttl time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.client)

	req, err := presignClient.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(int32(partNum)),
	}, s3.WithPresignExpires(ttl))

	if err != nil {
		return "", err
	}

	return req.URL, nil
}
