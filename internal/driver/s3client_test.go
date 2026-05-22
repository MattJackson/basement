package driver

import (
	"strings"
	"testing"
)

// TestNewS3PathStyleClient_ForcesPathStyle is the regression guard for
// cycle v1.3.0a.2 — the bug where user-region ListObjects against
// Garage (and IP-addressed MinIO) returned 404 because the SDK fell
// back to virtual-host addressing. The fix is a single line
// (o.UsePathStyle = true) but it must hold across every driver — this
// test asserts the option survives all the way to the built client.
func TestNewS3PathStyleClient_ForcesPathStyle(t *testing.T) {
	t.Parallel()

	client, err := NewS3PathStyleClient(
		"http://10.1.7.10:3902",
		"GK000000000000000000",
		"00000000000000000000000000000000",
		"garage",
	)
	if err != nil {
		t.Fatalf("NewS3PathStyleClient: %v", err)
	}
	if client == nil {
		t.Fatal("NewS3PathStyleClient: returned nil client")
	}

	opts := client.Options()
	if !opts.UsePathStyle {
		t.Errorf("Options().UsePathStyle = false, want true — Garage requires path-style addressing; virtual-host returns 404")
	}
}

// TestNewS3PathStyleClient_DefaultsRegion: empty region passes us-east-1
// to the SDK so config loading doesn't fail; Garage and MinIO ignore the
// signed region anyway.
func TestNewS3PathStyleClient_DefaultsRegion(t *testing.T) {
	t.Parallel()

	client, err := NewS3PathStyleClient(
		"http://10.1.7.10:3902",
		"GK000000000000000000",
		"00000000000000000000000000000000",
		"",
	)
	if err != nil {
		t.Fatalf("NewS3PathStyleClient with empty region: %v", err)
	}
	if got := client.Options().Region; got != "us-east-1" {
		t.Errorf("Options().Region = %q, want us-east-1 default", got)
	}
}

// TestNewS3PathStyleClient_EmptyEndpointSkipsResolver: when endpoint is
// blank (real AWS S3 path) we don't install a custom resolver — the SDK
// keeps its default region-aware AWS endpoint logic. UsePathStyle still
// applies because the SDK option is independent of resolver wiring.
func TestNewS3PathStyleClient_EmptyEndpointSkipsResolver(t *testing.T) {
	t.Parallel()

	client, err := NewS3PathStyleClient(
		"",
		"AKIA0000000000000000",
		"0000000000000000000000000000000000000000",
		"us-east-1",
	)
	if err != nil {
		t.Fatalf("NewS3PathStyleClient empty endpoint: %v", err)
	}
	if !client.Options().UsePathStyle {
		t.Error("UsePathStyle must hold even when no custom endpoint resolver is installed")
	}
}

// TestNewS3VirtualHostClient_LeavesPathStyleFalse is the v1.3.0c
// regression guard for the virtual-host constructor sibling. Operators
// with wildcard DNS opt their region into virtual-host addressing; the
// SDK's UsePathStyle option must come back FALSE on the built client
// or the toggle is a no-op.
func TestNewS3VirtualHostClient_LeavesPathStyleFalse(t *testing.T) {
	t.Parallel()

	client, err := NewS3VirtualHostClient(
		"https://s3.pq.io",
		"GK000000000000000000",
		"00000000000000000000000000000000",
		"us-east-1",
	)
	if err != nil {
		t.Fatalf("NewS3VirtualHostClient: %v", err)
	}
	if client == nil {
		t.Fatal("NewS3VirtualHostClient: returned nil client")
	}
	if client.Options().UsePathStyle {
		t.Error("Options().UsePathStyle = true, want false — virtual-host constructor must leave path-style off")
	}
}

// TestBuildS3Client_PathStyleByDefault: empty addressingStyle (the
// zero-value carried by every UserRegion persisted before v1.3.0c)
// picks path-style. Backwards-compat guarantee — no behaviour change
// without an explicit toggle flip.
func TestBuildS3Client_PathStyleByDefault(t *testing.T) {
	t.Parallel()

	client, err := BuildS3Client("https://s3.example.com", "AK", "SK", "us-east-1", "")
	if err != nil {
		t.Fatalf("BuildS3Client empty style: %v", err)
	}
	if !client.Options().UsePathStyle {
		t.Error("empty addressingStyle must default to path-style; got UsePathStyle=false")
	}
}

// TestBuildS3Client_VirtualHostOnDNSEndpoint: a DNS endpoint with
// virtual-host requested honours the toggle and returns a client with
// UsePathStyle=false. The happy path for operators with wildcard DNS.
func TestBuildS3Client_VirtualHostOnDNSEndpoint(t *testing.T) {
	t.Parallel()

	client, err := BuildS3Client("https://s3.pq.io", "AK", "SK", "us-east-1", AddressingStyleVirtualHost)
	if err != nil {
		t.Fatalf("BuildS3Client virtual_host on DNS: %v", err)
	}
	if client.Options().UsePathStyle {
		t.Error("virtual_host + DNS endpoint must produce UsePathStyle=false; got true")
	}
}

// TestBuildS3Client_IPEndpointForcesPathStyle is the smart-default
// regression guard: an IP-addressed endpoint MUST produce a path-style
// client regardless of the addressingStyle toggle, because virtual-host
// requires wildcard DNS for the bucket subdomain (impossible against
// a bare IP literal). Covers both IPv4 and IPv6.
func TestBuildS3Client_IPEndpointForcesPathStyle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, endpoint string
	}{
		{"ipv4 with port", "http://10.1.7.10:3902"},
		{"ipv4 no port", "http://192.168.1.1"},
		{"ipv6 bracketed", "http://[fe80::1]:9000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := BuildS3Client(tc.endpoint, "AK", "SK", "garage", AddressingStyleVirtualHost)
			if err != nil {
				t.Fatalf("BuildS3Client virtual_host + IP endpoint %q: %v", tc.endpoint, err)
			}
			if !client.Options().UsePathStyle {
				t.Errorf("IP endpoint %q must force path-style; got UsePathStyle=false", tc.endpoint)
			}
		})
	}
}

// TestEndpointHostIsIP exercises the helper used by BuildS3Client AND
// the FE's add-key form (mirrored in TS) to detect when virtual-host
// addressing is meaningful.
func TestEndpointHostIsIP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, endpoint string
		want           bool
	}{
		{"ipv4 with port", "http://10.1.7.10:3902", true},
		{"ipv4 no port", "https://192.168.1.1", true},
		{"ipv6 bracketed", "http://[::1]:9000", true},
		{"dns hostname", "https://s3.pq.io", false},
		{"dns hostname with port", "http://garage.lan:3902", false},
		{"empty", "", false},
		{"malformed", "://nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EndpointHostIsIP(tc.endpoint); got != tc.want {
				t.Errorf("EndpointHostIsIP(%q) = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

// TestNewS3PathStyleClient_RejectsEmptyCreds: empty accessKey/secret
// short-circuits with a clear error — guards against a misconfigured
// caller silently producing a client that signs with anonymous creds.
func TestNewS3PathStyleClient_RejectsEmptyCreds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, ak, sk, wantSubstr string
	}{
		{"empty accessKey", "", "secret", "accessKey"},
		{"empty secretKey", "AKIA", "", "secretKey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewS3PathStyleClient("http://x:1", tc.ak, tc.sk, "us-east-1")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q missing %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}
