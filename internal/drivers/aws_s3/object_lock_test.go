package aws_s3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// TestObjectLockSupport_True confirms AWS S3 advertises Object Lock.
func TestObjectLockSupport_True(t *testing.T) {
	d := &driver{}
	if !d.ObjectLockSupport() {
		t.Fatalf("expected ObjectLockSupport()=true for aws-s3")
	}
}

// TestGetObjectLockConfig_Enabled exercises the success path with a
// non-empty default retention. The S3 XML carries Days, the driver
// converts to a wall-clock RetainUntilDate.
func TestGetObjectLockConfig_Enabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <ObjectLockEnabled>Enabled</ObjectLockEnabled>
  <Rule>
    <DefaultRetention>
      <Mode>GOVERNANCE</Mode>
      <Days>30</Days>
    </DefaultRetention>
  </Rule>
</ObjectLockConfiguration>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	cfg, err := d.GetObjectLockConfig(context.Background(), "b")
	if err != nil {
		t.Fatalf("GetObjectLockConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Errorf("expected Enabled=true")
	}
	if cfg.DefaultRetention == nil {
		t.Fatalf("expected DefaultRetention")
	}
	if cfg.DefaultRetention.Mode != driverpkg.ObjectLockGovernance {
		t.Errorf("Mode=%q, want GOVERNANCE", cfg.DefaultRetention.Mode)
	}
	// 30 days from "now" — give it a 1-minute fudge factor.
	want := time.Now().Add(30 * 24 * time.Hour)
	delta := cfg.DefaultRetention.RetainUntilDate.Sub(want)
	if delta < -time.Minute || delta > time.Minute {
		t.Errorf("RetainUntilDate=%v, want ~%v", cfg.DefaultRetention.RetainUntilDate, want)
	}
}

// TestGetObjectLockConfig_NeverEnabled exercises the
// ObjectLockConfigurationNotFoundError normalization branch.
func TestGetObjectLockConfig_NeverEnabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>ObjectLockConfigurationNotFoundError</Code><Message>not found</Message></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	cfg, err := d.GetObjectLockConfig(context.Background(), "b")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if cfg.Enabled {
		t.Errorf("expected Enabled=false on never-enabled bucket")
	}
}

// TestPutObjectLockConfig_RejectsDisable confirms the driver refuses
// to flip Enabled from true → false.
func TestPutObjectLockConfig_RejectsDisable(t *testing.T) {
	d := &driver{}
	err := d.PutObjectLockConfig(context.Background(), "b", driverpkg.ObjectLockConfig{Enabled: false})
	if err == nil {
		t.Fatalf("expected error rejecting disable")
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestPutObjectLockConfig_HappyPath checks the wire path: the driver
// constructs an enabled config with a Days-based default retention.
func TestPutObjectLockConfig_HappyPath(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.PutObjectLockConfig(context.Background(), "b", driverpkg.ObjectLockConfig{
		Enabled: true,
		DefaultRetention: &driverpkg.ObjectLockRetention{
			Mode:            driverpkg.ObjectLockCompliance,
			RetainUntilDate: time.Now().Add(10 * 24 * time.Hour),
		},
	})
	if err != nil {
		t.Fatalf("PutObjectLockConfig: %v", err)
	}
	if !strings.Contains(gotBody, "<ObjectLockEnabled>Enabled</ObjectLockEnabled>") {
		t.Errorf("body missing ObjectLockEnabled=Enabled: %q", gotBody)
	}
	if !strings.Contains(gotBody, "<Mode>COMPLIANCE</Mode>") {
		t.Errorf("body missing COMPLIANCE mode: %q", gotBody)
	}
}

// TestPutObjectRetention_RequiresVersionID guards the per-version
// surface against an accidental "no versionID" call.
func TestPutObjectRetention_RequiresVersionID(t *testing.T) {
	d := &driver{}
	err := d.PutObjectRetention(context.Background(), "b", "k", "", driverpkg.ObjectLockRetention{
		Mode:            driverpkg.ObjectLockGovernance,
		RetainUntilDate: time.Now().Add(time.Hour),
	}, false)
	if err == nil {
		t.Fatalf("expected error for empty versionID")
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestPutObjectRetention_RequiresValidMode rejects unknown modes.
func TestPutObjectRetention_RequiresValidMode(t *testing.T) {
	d := &driver{}
	err := d.PutObjectRetention(context.Background(), "b", "k", "v1", driverpkg.ObjectLockRetention{
		Mode:            "BOGUS",
		RetainUntilDate: time.Now().Add(time.Hour),
	}, false)
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

// TestPutObjectRetention_ForwardsBypassFlag asserts the
// BypassGovernanceRetention header / param is set when requested.
func TestPutObjectRetention_ForwardsBypassFlag(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("x-amz-bypass-governance-retention")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.PutObjectRetention(context.Background(), "b", "k", "v1",
		driverpkg.ObjectLockRetention{
			Mode:            driverpkg.ObjectLockGovernance,
			RetainUntilDate: time.Now().Add(time.Hour),
		}, true)
	if err != nil {
		t.Fatalf("PutObjectRetention: %v", err)
	}
	if gotHeader != "true" {
		t.Errorf("bypass header=%q, want true", gotHeader)
	}
}

// TestGetObjectRetention_NoRetentionReturnsNil exercises the
// NoSuchObjectLockConfiguration normalization branch.
func TestGetObjectRetention_NoRetentionReturnsNil(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NoSuchObjectLockConfiguration</Code><Message>not set</Message></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ret, err := d.GetObjectRetention(context.Background(), "b", "k", "v1")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if ret != nil {
		t.Errorf("expected nil retention, got %+v", ret)
	}
}

// TestGetObjectRetention_HappyPath exercises success.
func TestGetObjectRetention_HappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Retention xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Mode>COMPLIANCE</Mode>
  <RetainUntilDate>2030-01-02T00:00:00Z</RetainUntilDate>
</Retention>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ret, err := d.GetObjectRetention(context.Background(), "b", "k", "v1")
	if err != nil {
		t.Fatalf("GetObjectRetention: %v", err)
	}
	if ret == nil {
		t.Fatalf("expected non-nil retention")
	}
	if ret.Mode != driverpkg.ObjectLockCompliance {
		t.Errorf("Mode=%q, want COMPLIANCE", ret.Mode)
	}
	if ret.RetainUntilDate.Year() != 2030 {
		t.Errorf("Year=%d, want 2030", ret.RetainUntilDate.Year())
	}
}

// TestObjectLegalHold_RoundTrip exercises PUT then GET.
func TestObjectLegalHold_RoundTrip(t *testing.T) {
	var lastMethod, lastBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		if r.Method == http.MethodPut {
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			lastBody = string(buf[:n])
			w.WriteHeader(http.StatusOK)
			return
		}
		// GET: respond with ON.
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<LegalHold xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>ON</Status></LegalHold>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.PutObjectLegalHold(context.Background(), "b", "k", "v1", true); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if lastMethod != http.MethodPut {
		t.Errorf("method=%q, want PUT", lastMethod)
	}
	if !strings.Contains(lastBody, "<Status>ON</Status>") {
		t.Errorf("body missing ON status: %q", lastBody)
	}

	on, err := d.GetObjectLegalHold(context.Background(), "b", "k", "v1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !on {
		t.Errorf("expected on=true")
	}
}

// TestObjectLockErrorMapping confirms wrapAWSObjectLockErr maps the
// canonical S3 error codes to the right driver sentinels.
func TestObjectLockErrorMapping(t *testing.T) {
	cases := []struct {
		code     string
		sentinel error
	}{
		{"NoSuchBucket", driverpkg.ErrNotFound},
		{"NoSuchKey", driverpkg.ErrNotFound},
		{"NoSuchVersion", driverpkg.ErrNotFound},
		{"AccessDenied", driverpkg.ErrPermissionDenied},
		{"Forbidden", driverpkg.ErrPermissionDenied},
		{"InvalidRetentionPeriod", driverpkg.ErrInvalid},
		{"InvalidArgument", driverpkg.ErrInvalid},
		{"InvalidRequest", driverpkg.ErrConflict},
		{"SomethingElse", driverpkg.ErrInvalid},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			err := wrapAWSObjectLockErr("Op", &fakeAPIErr{code: tc.code, msg: "boom"})
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("code=%q: want %v, got %v", tc.code, tc.sentinel, err)
			}
		})
	}
}
