package driver

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// NewS3PathStyleClient builds an aws-sdk-go-v2 *s3.Client wired for
// path-style addressing (UsePathStyle = true) against a custom S3
// endpoint. Every per-driver constructor in internal/drivers/* delegates
// here so the "always path-style" guarantee lives in exactly one place —
// drift between drivers (Garage, MinIO, AWS, …) is impossible to introduce
// without changing this helper.
//
// Why path-style is mandatory:
//
//   - Garage's S3 API only supports path-style addressing
//     (https://garagehq.deuxfleurs.fr/documentation/quick-start/ — "S3
//     compatibility" section). Virtual-host (bucket.endpoint) returns 404.
//   - MinIO supports both, but when accessed by IP (bucket.10.1.7.10:9000)
//     virtual-host fails at DNS — there is no wildcard A-record for the
//     bucket prefix.
//   - AWS S3 supports both; path-style works against every region. The
//     v2 SDK warns that path-style is deprecated for new buckets but it
//     remains functional and is the only mode we need for self-hosted
//     deployments. Using one mode everywhere keeps the request shape
//     predictable for operators reading backend access logs.
//
// v1.3.0a.2: extracted from per-driver newS3Client to fix a user-region
// ListObjects regression where the v1 path-style flag was assumed but
// not auditable across all four drivers — see basement-internal
// freshman_backlog.md, cycle v1.3.0a.2.
//
// When endpoint is empty the helper omits the custom resolver so the
// SDK's default AWS endpoint resolution applies (production AWS S3
// usage). When region is empty it defaults to "us-east-1" — Garage and
// MinIO ignore region but the SDK refuses to load a config without one.
func NewS3PathStyleClient(endpoint, accessKey, secretKey, region string) (*s3.Client, error) {
	if accessKey == "" {
		return nil, fmt.Errorf("NewS3PathStyleClient: empty accessKey")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("NewS3PathStyleClient: empty secretKey")
	}
	if region == "" {
		region = "us-east-1"
	}

	opts := []func(*config.LoadOptions) error{
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			"", // no token for static creds
		)),
		config.WithRegion(region),
	}

	if endpoint != "" {
		opts = append(opts, config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(func(_, _ string, _ ...interface{}) (aws.Endpoint, error) { //nolint:staticcheck // Using deprecated API for custom endpoint support
			return aws.Endpoint{ //nolint:staticcheck // Using deprecated type for custom endpoint support
				URL:               endpoint,
				HostnameImmutable: true,
				SigningRegion:     "",
			}, nil
		})))
	}

	cfgLoaded, err := config.LoadDefaultConfig(context.TODO(), opts...)
	if err != nil {
		return nil, fmt.Errorf("NewS3PathStyleClient: load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfgLoaded, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return client, nil
}
