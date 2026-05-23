package aws_s3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// TestSSESupport_BothTrue confirms AWS S3 advertises both SSE-S3 and
// SSE-KMS. The FE uses this to render the full radio + key-ID input.
func TestSSESupport_BothTrue(t *testing.T) {
	d := &driver{}
	s3, kms := d.SSESupport()
	if !s3 || !kms {
		t.Fatalf("SSESupport()=(%v,%v), want (true,true)", s3, kms)
	}
}

// TestGetBucketEncryption_NeverConfigured exercises the
// ServerSideEncryptionConfigurationNotFoundError normalization branch.
func TestGetBucketEncryption_NeverConfigured(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>ServerSideEncryptionConfigurationNotFoundError</Code><Message>not found</Message></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	enc, err := d.GetBucketEncryption(context.Background(), "b")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if enc.Enabled {
		t.Errorf("expected Enabled=false on never-configured bucket")
	}
}

// TestGetBucketEncryption_SSE_S3 exercises the success path with an
// AES256 (SSE-S3) configuration.
func TestGetBucketEncryption_SSE_S3(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Rule>
    <ApplyServerSideEncryptionByDefault>
      <SSEAlgorithm>AES256</SSEAlgorithm>
    </ApplyServerSideEncryptionByDefault>
  </Rule>
</ServerSideEncryptionConfiguration>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	enc, err := d.GetBucketEncryption(context.Background(), "b")
	if err != nil {
		t.Fatalf("GetBucketEncryption: %v", err)
	}
	if !enc.Enabled {
		t.Errorf("Enabled=false, want true")
	}
	if enc.Algorithm != driverpkg.SSEAlgorithmAES256 {
		t.Errorf("Algorithm=%q, want AES256", enc.Algorithm)
	}
	if enc.KMSKeyID != "" {
		t.Errorf("KMSKeyID=%q, want empty on SSE-S3", enc.KMSKeyID)
	}
}

// TestGetBucketEncryption_SSE_KMS exercises the success path with a
// KMS-keyed configuration plus BucketKey optimization on.
func TestGetBucketEncryption_SSE_KMS(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Rule>
    <ApplyServerSideEncryptionByDefault>
      <SSEAlgorithm>aws:kms</SSEAlgorithm>
      <KMSMasterKeyID>arn:aws:kms:us-east-1:111122223333:key/abcd-1234</KMSMasterKeyID>
    </ApplyServerSideEncryptionByDefault>
    <BucketKeyEnabled>true</BucketKeyEnabled>
  </Rule>
</ServerSideEncryptionConfiguration>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	enc, err := d.GetBucketEncryption(context.Background(), "b")
	if err != nil {
		t.Fatalf("GetBucketEncryption: %v", err)
	}
	if !enc.Enabled {
		t.Errorf("Enabled=false, want true")
	}
	if enc.Algorithm != driverpkg.SSEAlgorithmKMS {
		t.Errorf("Algorithm=%q, want aws:kms", enc.Algorithm)
	}
	if !strings.Contains(enc.KMSKeyID, "arn:aws:kms") {
		t.Errorf("KMSKeyID=%q, want a KMS ARN", enc.KMSKeyID)
	}
	if !enc.BucketKey {
		t.Errorf("BucketKey=false, want true")
	}
}

// TestPutBucketEncryption_RejectsDisabled confirms the driver refuses
// to PUT with Enabled=false (operator should DELETE instead).
func TestPutBucketEncryption_RejectsDisabled(t *testing.T) {
	d := &driver{}
	err := d.PutBucketEncryption(context.Background(), "b", driverpkg.BucketEncryption{Enabled: false})
	if err == nil {
		t.Fatalf("expected error rejecting disabled PUT")
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestPutBucketEncryption_RejectsBadAlgorithm rejects unknown ciphers.
func TestPutBucketEncryption_RejectsBadAlgorithm(t *testing.T) {
	d := &driver{}
	err := d.PutBucketEncryption(context.Background(), "b", driverpkg.BucketEncryption{
		Enabled:   true,
		Algorithm: "ROT13",
	})
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestPutBucketEncryption_RequiresKMSKeyID rejects SSE-KMS without an
// ARN — the wire boundary catches this too but the driver stays
// defensive.
func TestPutBucketEncryption_RequiresKMSKeyID(t *testing.T) {
	d := &driver{}
	err := d.PutBucketEncryption(context.Background(), "b", driverpkg.BucketEncryption{
		Enabled:   true,
		Algorithm: driverpkg.SSEAlgorithmKMS,
	})
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestPutBucketEncryption_SSE_S3_HappyPath checks the wire body — the
// driver constructs a single-rule ApplyServerSideEncryptionByDefault
// with SSEAlgorithm=AES256.
func TestPutBucketEncryption_SSE_S3_HappyPath(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.PutBucketEncryption(context.Background(), "b", driverpkg.BucketEncryption{
		Enabled:   true,
		Algorithm: driverpkg.SSEAlgorithmAES256,
	})
	if err != nil {
		t.Fatalf("PutBucketEncryption: %v", err)
	}
	if !strings.Contains(gotBody, "<SSEAlgorithm>AES256</SSEAlgorithm>") {
		t.Errorf("body missing AES256 algorithm: %q", gotBody)
	}
}

// TestPutBucketEncryption_SSE_KMS_HappyPath verifies KMSMasterKeyID +
// BucketKeyEnabled make it onto the wire.
func TestPutBucketEncryption_SSE_KMS_HappyPath(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.PutBucketEncryption(context.Background(), "b", driverpkg.BucketEncryption{
		Enabled:   true,
		Algorithm: driverpkg.SSEAlgorithmKMS,
		KMSKeyID:  "arn:aws:kms:us-east-1:111122223333:key/abcd-1234",
		BucketKey: true,
	})
	if err != nil {
		t.Fatalf("PutBucketEncryption: %v", err)
	}
	if !strings.Contains(gotBody, "<SSEAlgorithm>aws:kms</SSEAlgorithm>") {
		t.Errorf("body missing aws:kms algorithm: %q", gotBody)
	}
	if !strings.Contains(gotBody, "abcd-1234") {
		t.Errorf("body missing KMS key ARN: %q", gotBody)
	}
	if !strings.Contains(gotBody, "<BucketKeyEnabled>true</BucketKeyEnabled>") {
		t.Errorf("body missing BucketKeyEnabled=true: %q", gotBody)
	}
}

// TestDeleteBucketEncryption_HappyPath confirms the DELETE call lands.
func TestDeleteBucketEncryption_HappyPath(t *testing.T) {
	var gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.DeleteBucketEncryption(context.Background(), "b"); err != nil {
		t.Fatalf("DeleteBucketEncryption: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method=%q, want DELETE", gotMethod)
	}
}

// TestDeleteBucketEncryption_IdempotentOnNotConfigured confirms the
// driver treats "never configured" as success.
func TestDeleteBucketEncryption_IdempotentOnNotConfigured(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>ServerSideEncryptionConfigurationNotFoundError</Code><Message>not set</Message></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.DeleteBucketEncryption(context.Background(), "b"); err != nil {
		t.Fatalf("expected nil err on already-empty bucket, got %v", err)
	}
}

// TestEncryptionErrorMapping confirms wrapAWSEncryptionErr maps the
// canonical S3 error codes to the right driver sentinels.
func TestEncryptionErrorMapping(t *testing.T) {
	cases := []struct {
		code     string
		sentinel error
	}{
		{"NoSuchBucket", driverpkg.ErrNotFound},
		{"AccessDenied", driverpkg.ErrPermissionDenied},
		{"Forbidden", driverpkg.ErrPermissionDenied},
		{"InvalidArgument", driverpkg.ErrInvalid},
		{"MalformedXML", driverpkg.ErrInvalid},
		{"KMSInvalidStateException", driverpkg.ErrConflict},
		{"KMSDisabledException", driverpkg.ErrConflict},
		{"KMSAccessDeniedException", driverpkg.ErrConflict},
		{"SomethingElse", driverpkg.ErrInvalid},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			err := wrapAWSEncryptionErr("Op", &fakeAPIErr{code: tc.code, msg: "boom"})
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("code=%q: want %v, got %v", tc.code, tc.sentinel, err)
			}
		})
	}
}
