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
