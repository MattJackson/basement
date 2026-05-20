package garage_v1

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3Client wraps the AWS S3 client and provides a convenient interface for
// presign operations against Garage's S3-compatible endpoint. It handles SigV4
// signing automatically via the SDK with path-style addressing required by Garage.
type s3Client struct {
	client *s3.Client
}

// newS3Client creates an S3 client from the driver config. Config keys:
//   - "s3_endpoint": S3-compatible endpoint URL (required, e.g., http://garage:3972)
//   - "access_key_id": AWS access key ID for Garage's S3 API
//   - "secret_key": AWS secret access key for Garage's S3 API
func newS3Client(cfg map[string]string) (*s3Client, error) {
	endpoint := cfg["s3_endpoint"]
	if endpoint == "" {
		return nil, fmt.Errorf("missing required config key: s3_endpoint")
	}

	accessKey := cfg["access_key_id"]
	if accessKey == "" {
		return nil, fmt.Errorf("missing required config key: access_key_id")
	}

	secretKey := cfg["secret_key"]
	if secretKey == "" {
		return nil, fmt.Errorf("missing required config key: secret_key")
	}

	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
		accessKey,
		secretKey,
		"", // no token for static creds
	)))

	// Region doesn't matter for Garage's S3 endpoint; use us-east-1 as default.
	opts = append(opts, config.WithRegion("us-east-1"))

	opts = append(opts, config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(func(_, _ string, _ ...interface{}) (aws.Endpoint, error) { //nolint:staticcheck // Using deprecated API for custom endpoint support
		return aws.Endpoint{ //nolint:staticcheck // Using deprecated type for custom endpoint support
			URL:               endpoint,
			HostnameImmutable: true,
			SigningRegion:     "",
		}, nil
	})))

	cfgLoaded, err := config.LoadDefaultConfig(context.TODO(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for S3 endpoint %q: %w", endpoint, err)
	}

	client := s3.NewFromConfig(cfgLoaded, func(o *s3.Options) {
		o.UsePathStyle = true // Garage requires path-style addressing
	})

	return &s3Client{client: client}, nil
}

// presignGetObject creates a presigned GET URL for an object.
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

// presignPutObject creates a presigned PUT URL for an object.
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
