package aws_s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// TestVersioningSupport_True confirms the AWS driver advertises
// versioning unconditionally — the FE's gate fires off this flag
// and AWS S3 implements the full versioning surface natively.
func TestVersioningSupport_True(t *testing.T) {
	d := &driver{}
	if !d.VersioningSupport() {
		t.Fatalf("expected VersioningSupport()=true for aws-s3")
	}
}

// TestGetVersioningStatus_Enabled exercises the happy path against a
// fake S3 server that returns an Enabled status response.
func TestGetVersioningStatus_Enabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "versioning") {
			http.Error(w, "expected ?versioning", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Status>Enabled</Status>
</VersioningConfiguration>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	st, err := d.GetVersioningStatus(context.Background(), "my-bucket")
	if err != nil {
		t.Fatalf("GetVersioningStatus: %v", err)
	}
	if st != driverpkg.VersioningEnabled {
		t.Fatalf("status=%q, want enabled", st)
	}
}

// TestGetVersioningStatus_NeverEnabled exercises the empty-status
// branch — S3 returns an empty body / empty <Status> on a bucket
// that has never been versioned. The driver normalises that to
// VersioningDisabled rather than leaking the empty string.
func TestGetVersioningStatus_NeverEnabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	st, err := d.GetVersioningStatus(context.Background(), "my-bucket")
	if err != nil {
		t.Fatalf("GetVersioningStatus: %v", err)
	}
	if st != driverpkg.VersioningDisabled {
		t.Fatalf("status=%q, want disabled", st)
	}
}

// TestGetVersioningStatus_Suspended exercises the suspended branch.
func TestGetVersioningStatus_Suspended(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Status>Suspended</Status>
</VersioningConfiguration>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	st, err := d.GetVersioningStatus(context.Background(), "my-bucket")
	if err != nil {
		t.Fatalf("GetVersioningStatus: %v", err)
	}
	if st != driverpkg.VersioningSuspended {
		t.Fatalf("status=%q, want suspended", st)
	}
}

// TestEnableVersioning_PutsEnabled asserts the driver issues a
// PUT ?versioning with Status=Enabled.
func TestEnableVersioning_PutsEnabled(t *testing.T) {
	var gotMethod, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.EnableVersioning(context.Background(), "my-bucket"); err != nil {
		t.Fatalf("EnableVersioning: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method=%q, want PUT", gotMethod)
	}
	if !strings.Contains(gotBody, "<Status>Enabled</Status>") {
		t.Errorf("body missing Status=Enabled: %s", gotBody)
	}
}

// TestSuspendVersioning_PutsSuspended asserts the inverse — the
// suspend path emits Status=Suspended.
func TestSuspendVersioning_PutsSuspended(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.SuspendVersioning(context.Background(), "my-bucket"); err != nil {
		t.Fatalf("SuspendVersioning: %v", err)
	}
	if !strings.Contains(gotBody, "<Status>Suspended</Status>") {
		t.Errorf("body missing Status=Suspended: %s", gotBody)
	}
}

// TestListObjectVersions_HappyPath returns a mixed Versions +
// DeleteMarkers response and asserts both are surfaced in the
// driver's flat slice.
func TestListObjectVersions_HappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>my-bucket</Name>
  <Prefix>logs/app.txt</Prefix>
  <IsTruncated>false</IsTruncated>
  <Version>
    <Key>logs/app.txt</Key>
    <VersionId>v2</VersionId>
    <IsLatest>true</IsLatest>
    <LastModified>2025-01-02T00:00:00.000Z</LastModified>
    <ETag>"etag-2"</ETag>
    <Size>200</Size>
  </Version>
  <Version>
    <Key>logs/app.txt</Key>
    <VersionId>v1</VersionId>
    <IsLatest>false</IsLatest>
    <LastModified>2025-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag-1"</ETag>
    <Size>100</Size>
  </Version>
  <DeleteMarker>
    <Key>logs/old.txt</Key>
    <VersionId>vd1</VersionId>
    <IsLatest>true</IsLatest>
    <LastModified>2025-01-03T00:00:00.000Z</LastModified>
  </DeleteMarker>
</ListVersionsResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	versions, next, err := d.ListObjectVersions(context.Background(), "my-bucket", "logs/", "", 100)
	if err != nil {
		t.Fatalf("ListObjectVersions: %v", err)
	}
	if next != "" {
		t.Errorf("nextMarker=%q, want empty (not truncated)", next)
	}
	if len(versions) != 3 {
		t.Fatalf("versions=%d, want 3", len(versions))
	}
	if versions[0].VersionID != "v2" || !versions[0].IsLatest || versions[0].Size != 200 {
		t.Errorf("versions[0]=%+v", versions[0])
	}
	if versions[1].VersionID != "v1" || versions[1].IsLatest {
		t.Errorf("versions[1]=%+v", versions[1])
	}
	if versions[2].VersionID != "vd1" || !versions[2].IsDeleteMarker {
		t.Errorf("versions[2]=%+v (want delete marker)", versions[2])
	}
}

// TestListObjectVersions_TruncatedPagination asserts the
// NextKeyMarker/NextVersionIdMarker pair is fused into a single
// opaque token via joinVersionMarker.
func TestListObjectVersions_TruncatedPagination(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>b</Name>
  <IsTruncated>true</IsTruncated>
  <NextKeyMarker>k99</NextKeyMarker>
  <NextVersionIdMarker>v99</NextVersionIdMarker>
  <Version>
    <Key>k1</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest>
    <LastModified>2025-01-01T00:00:00.000Z</LastModified>
    <ETag>"x"</ETag><Size>10</Size>
  </Version>
</ListVersionsResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, next, err := d.ListObjectVersions(context.Background(), "b", "", "", 1)
	if err != nil {
		t.Fatalf("ListObjectVersions: %v", err)
	}
	if next != "k99|v99" {
		t.Errorf("nextMarker=%q, want k99|v99", next)
	}

	// Round-trip through split.
	k, v := splitVersionMarker(next)
	if k != "k99" || v != "v99" {
		t.Errorf("split: k=%q v=%q, want k99/v99", k, v)
	}
}

// TestGetObjectVersion_StreamsBody asserts the driver passes
// VersionId to the GetObject call and returns the body via
// StreamResult.
func TestGetObjectVersion_StreamsBody(t *testing.T) {
	var gotVersion string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.URL.Query().Get("versionId")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"abc"`)
		_, _ = w.Write([]byte("hello v1"))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	res, err := d.GetObjectVersion(context.Background(), "b", "k", "v1")
	if err != nil {
		t.Fatalf("GetObjectVersion: %v", err)
	}
	defer res.Body.Close()
	if gotVersion != "v1" {
		t.Errorf("versionId param=%q, want v1", gotVersion)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "hello v1" {
		t.Errorf("body=%q", string(body))
	}
}

// TestGetObjectVersion_MissingVersion400s asserts the driver rejects
// an empty versionID up front rather than sending a bare GetObject
// that would silently fetch the current version.
func TestGetObjectVersion_MissingVersion400s(t *testing.T) {
	d := &driver{}
	_, err := d.GetObjectVersion(context.Background(), "b", "k", "")
	if err == nil {
		t.Fatal("expected error for empty versionID")
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestDeleteObjectVersion_PassesVersion asserts the driver routes
// the DELETE through with VersionId set.
func TestDeleteObjectVersion_PassesVersion(t *testing.T) {
	var gotMethod, gotVersion string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotVersion = r.URL.Query().Get("versionId")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.DeleteObjectVersion(context.Background(), "b", "k", "v1"); err != nil {
		t.Fatalf("DeleteObjectVersion: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method=%q, want DELETE", gotMethod)
	}
	if gotVersion != "v1" {
		t.Errorf("versionId=%q, want v1", gotVersion)
	}
}

// TestDeleteObjectVersion_MissingVersion400s — same defensive
// guardrail as GetObjectVersion; an empty versionID would silently
// translate to "delete current version + insert delete marker"
// which is NOT what the per-version endpoint contract promises.
func TestDeleteObjectVersion_MissingVersion400s(t *testing.T) {
	d := &driver{}
	err := d.DeleteObjectVersion(context.Background(), "b", "k", "")
	if err == nil {
		t.Fatal("expected error for empty versionID")
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestVersioningErrorMapping confirms wrapAWSVersioningErr maps the
// canonical S3 error codes to the right driver sentinels.
func TestVersioningErrorMapping(t *testing.T) {
	cases := []struct {
		code   string
		sentinel error
	}{
		{"NoSuchBucket", driverpkg.ErrNotFound},
		{"NoSuchKey", driverpkg.ErrNotFound},
		{"NoSuchVersion", driverpkg.ErrNotFound},
		{"AccessDenied", driverpkg.ErrPermissionDenied},
		{"Forbidden", driverpkg.ErrPermissionDenied},
		{"SomethingElse", driverpkg.ErrInvalid},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			err := wrapAWSVersioningErr("Op", &fakeAPIErr{code: tc.code, msg: "boom"})
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("code=%q: want %v, got %v", tc.code, tc.sentinel, err)
			}
		})
	}
}

// TestSplitJoinVersionMarker round-trip confirms the opaque marker
// shape is symmetric — joinVersionMarker(splitVersionMarker(x)) == x
// for any composed token, and split of a no-pipe input keeps the
// input as the key half.
func TestSplitJoinVersionMarker(t *testing.T) {
	if got := joinVersionMarker("k", "v"); got != "k|v" {
		t.Errorf("join=%q, want k|v", got)
	}
	if k, v := splitVersionMarker("k|v"); k != "k" || v != "v" {
		t.Errorf("split: k=%q v=%q", k, v)
	}
	if k, v := splitVersionMarker("solo"); k != "solo" || v != "" {
		t.Errorf("split-solo: k=%q v=%q", k, v)
	}
	if k, v := splitVersionMarker(""); k != "" || v != "" {
		t.Errorf("split-empty: k=%q v=%q", k, v)
	}
}

// fakeAPIErr satisfies the ErrorCode() interface the SDK error
// shape exposes so wrapAWSVersioningErr can be exercised without a
// real SDK error path.
type fakeAPIErr struct {
	code string
	msg  string
}

func (e *fakeAPIErr) Error() string     { return e.msg }
func (e *fakeAPIErr) ErrorCode() string { return e.code }
