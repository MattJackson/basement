package driver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// AddressingStylePath / AddressingStyleVirtualHost are the canonical
// string values stored on store.UserRegion.AddressingStyle and threaded
// through driver Config maps as the "addressing_style" key. The default
// (zero-value / unset) is path-style: every backend supports it, and a
// virtual-host request against an endpoint with no wildcard DNS fails
// confusingly. Operators with wildcard DNS for the endpoint host
// (e.g. `*.s3.pq.io` Caddy setup) may opt into virtual-host per-region
// for traffic-shape / S3-tool-compat reasons (v1.3.0c).
const (
	AddressingStylePath        = "path"
	AddressingStyleVirtualHost = "virtual_host"
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

// NewS3VirtualHostClient is the v1.3.0c sibling of NewS3PathStyleClient
// for operators who have wildcard DNS for their endpoint host (a
// `*.s3.pq.io` Caddy / nginx setup) and prefer virtual-host addressing
// for traffic-shape predictability / compatibility with S3 tools that
// hard-code virtual-host requests (e.g. some boto3-defaults configs).
//
// Identical to the path-style constructor except UsePathStyle is left
// at false (the SDK default). Callers MUST verify the endpoint host
// actually has wildcard DNS — using virtual-host against
// `bucket.<bare-host>` with no wildcard A-record fails at DNS, which
// surfaces as an opaque "no such host" error that's hard to debug.
// Per the v1.3.0c smart-default: if endpoint hostname is an IP literal
// the caller should fall back to path-style regardless of this
// constructor — see BuildS3Client.
func NewS3VirtualHostClient(endpoint, accessKey, secretKey, region string) (*s3.Client, error) {
	if accessKey == "" {
		return nil, fmt.Errorf("NewS3VirtualHostClient: empty accessKey")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("NewS3VirtualHostClient: empty secretKey")
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
				HostnameImmutable: false,
				SigningRegion:     "",
			}, nil
		})))
	}

	cfgLoaded, err := config.LoadDefaultConfig(context.TODO(), opts...)
	if err != nil {
		return nil, fmt.Errorf("NewS3VirtualHostClient: load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfgLoaded, func(o *s3.Options) {
		o.UsePathStyle = false
	})

	return client, nil
}

// EndpointHostIsIP returns true when the supplied endpoint URL's host
// is an IPv4 or IPv6 literal (vs a DNS hostname). Used by BuildS3Client
// (and the FE's add-key form) to enforce path-style addressing for
// IP-addressed backends: virtual-host requires wildcard DNS for the
// bucket subdomain, which by definition can't exist for an IP literal.
//
// Empty / unparseable endpoints return false (the SDK's default endpoint
// path applies and addressing-style toggle is moot).
func EndpointHostIsIP(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		// url.Parse silently accepts bare host with no scheme; try once
		// more with the scheme prefix stripped.
		host = strings.TrimSpace(endpoint)
	}
	return net.ParseIP(host) != nil
}

// BuildS3Client is the v1.3.0c entry point shared by every caller that
// builds an S3 client from a per-region tuple. Picks the path-style or
// virtual-host constructor based on `addressingStyle` after applying the
// smart default: an IP-addressed endpoint forces path-style regardless
// of the requested style (virtual-host requires wildcard DNS for the
// bucket subdomain, which an IP literal cannot satisfy).
//
//   - addressingStyle == "" || AddressingStylePath → path-style
//   - addressingStyle == AddressingStyleVirtualHost && DNS host → virtual-host
//   - addressingStyle == AddressingStyleVirtualHost && IP host  → path-style
//     (silently falls back; the UI is responsible for surfacing the
//     disabled-toggle hint to the operator at edit time)
//   - any other addressingStyle value → path-style (defensive default;
//     a future "auto" or "global_endpoint" toggle can extend this here
//     without touching every call site)
func BuildS3Client(endpoint, accessKey, secretKey, region, addressingStyle string) (*s3.Client, error) {
	if addressingStyle == AddressingStyleVirtualHost && !EndpointHostIsIP(endpoint) {
		return NewS3VirtualHostClient(endpoint, accessKey, secretKey, region)
	}
	return NewS3PathStyleClient(endpoint, accessKey, secretKey, region)
}
