package driver

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestBuildUserRegionS3Client_BlocksLoopback is the SSRF regression guard
// for the user-region tier (security/audit r11). A user can store a
// UserRegion whose endpoint points at loopback / link-local /
// 169.254.169.254 metadata / RFC-1918; the guarded client used for that
// tier must refuse to DIAL such an address even though it is reachable.
//
// We stand up a loopback httptest server (its URL is http://127.0.0.1:<port>)
// and assert the guarded client refuses to connect, while the ordinary
// (admin/cluster-tier) client reaches it — proving the guard is the only
// differentiator and a normal/public dial path is unaffected.
func TestBuildUserRegionS3Client_BlocksLoopback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(nil) // 200 on any path is fine; we only care that a dial happens
	defer srv.Close()

	const ak = "GK000000000000000000"
	const sk = "00000000000000000000000000000000"

	// Guarded (user-region) client: the dial to 127.0.0.1 must be refused
	// at connect time with the shared SSRF error.
	guarded, err := BuildUserRegionS3Client(context.Background(), srv.URL, ak, sk, "garage", "")
	if err != nil {
		t.Fatalf("BuildUserRegionS3Client build: %v", err)
	}
	_, err = guarded.ListBuckets(context.Background(), &s3.ListBucketsInput{})
	if err == nil {
		t.Fatalf("expected the SSRF guard to block loopback %s, got nil error", srv.URL)
	}
	if !strings.Contains(err.Error(), "non-public address") {
		t.Errorf("expected a non-public-address block error, got: %v", err)
	}

	// Unguarded (admin/cluster-tier) client against the SAME loopback
	// endpoint must NOT be blocked at connect — it reaches the server (the
	// request then fails on the S3 protocol level, not on the dial). This
	// confirms the guard is scoped to the user-region path only.
	unguarded, err := BuildS3Client(context.Background(), srv.URL, ak, sk, "garage", "")
	if err != nil {
		t.Fatalf("BuildS3Client build: %v", err)
	}
	_, err = unguarded.ListBuckets(context.Background(), &s3.ListBucketsInput{})
	if err != nil && strings.Contains(err.Error(), "non-public address") {
		t.Errorf("unguarded admin/cluster client must NOT apply the SSRF guard, got: %v", err)
	}
}

// TestBuildUserRegionS3Client_AllowsPublicHost confirms the guard does not
// false-positive on a normal public hostname: building the client and
// issuing a request resolves+dials a public IP (the dial may still fail for
// other reasons in CI, but it must NOT be the SSRF block).
func TestBuildUserRegionS3Client_AllowsPublicHost(t *testing.T) {
	t.Parallel()

	guarded, err := BuildUserRegionS3Client(context.Background(), "https://s3.amazonaws.com", "AKIA0000000000000000", "0000000000000000000000000000000000000000", "us-east-1", "")
	if err != nil {
		t.Fatalf("BuildUserRegionS3Client build: %v", err)
	}
	// A HEAD on a public bucket name will fail auth/404, but importantly it
	// must not be refused by the SSRF dial guard.
	_, err = guarded.HeadBucket(context.Background(), &s3.HeadBucketInput{Bucket: aws.String("basement-ssrf-guard-probe")})
	if err != nil && strings.Contains(err.Error(), "non-public address") {
		t.Errorf("public host must not be blocked by the SSRF guard, got: %v", err)
	}
}

// TestForUserRegion_SetsSSRFGuardMarker asserts the default (production)
// builder path in Registry.ForUserRegion threads ssrf_guard="user-region"
// into the driver Config. The garage_v1 driver reads that marker to route
// through BuildUserRegionS3Client (the dial-time block proven by the
// BuildUserRegionS3Client_* tests above). We register a capturing stand-in
// for the "garage-v1" factory (the real one lives in internal/drivers/
// garage_v1, which the driver package can't import without a cycle) and
// assert the marker reaches the Config that factory receives.
func TestForUserRegion_SetsSSRFGuardMarker(t *testing.T) {
	var gotCfg Config
	Register("garage-v1", func(cfg Config) (Driver, error) {
		gotCfg = cfg
		return &mockDriver{}, nil
	})

	reg := NewRegistry(newMockConnStore())
	reg.SetUserRegionsStore(fakeUserRegions{})
	// No SetRegionDriverBuilder → exercises the default Open("garage-v1", cfg)
	// path that must carry the ssrf_guard marker.

	if _, err := reg.ForUserRegion(context.Background(), "https://s3.example.com", "GK000000000000000000", "00000000000000000000000000000000", "garage", ""); err != nil {
		t.Fatalf("ForUserRegion: %v", err)
	}
	if gotCfg["ssrf_guard"] != "user-region" {
		t.Errorf("ForUserRegion default path must set cfg[ssrf_guard]=user-region (so garage_v1 routes through the guarded S3 client); got %q", gotCfg["ssrf_guard"])
	}
}
