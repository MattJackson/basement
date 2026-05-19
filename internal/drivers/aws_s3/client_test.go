package aws_s3

import (
	"context"
	"errors"
	"testing"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// testDriver is a simple driver implementation for testing unsupported methods.
type testDriver struct{}

func (*testDriver) Capabilities(_ context.Context) (driverpkg.Caps, error) {
	return driverpkg.Caps{
		Driver:        driverName,
		Layout:        driverpkg.LayoutReadonly,
		Quotas:        false,
		BucketAliases: false,
		KeyModel:      driverpkg.KeyModelIAM,
		Presign:       true,
		Multipart:     true,
		Versioning:    true,
	}, nil
}

func (*testDriver) HealthCheck(_ context.Context) (driverpkg.HealthReport, error) {
	return driverpkg.HealthReport{Status: "healthy"}, nil
}

func (*testDriver) ListNodes(_ context.Context) ([]driverpkg.Node, error) {
	return nil, &driverpkg.Error{Op: "ListNodes", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) GetLayout(_ context.Context) (driverpkg.Layout, error) {
	return driverpkg.Layout{}, &driverpkg.Error{Op: "GetLayout", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) StageLayout(_ context.Context, _ driverpkg.LayoutChange) (driverpkg.LayoutDiff, error) {
	return driverpkg.LayoutDiff{}, &driverpkg.Error{Op: "StageLayout", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) ApplyLayout(_ context.Context) error {
	return &driverpkg.Error{Op: "ApplyLayout", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) RevertLayout(_ context.Context) error {
	return &driverpkg.Error{Op: "RevertLayout", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) ListKeys(_ context.Context) ([]driverpkg.Key, error) {
	return nil, &driverpkg.Error{Op: "ListKeys", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) GetKey(_ context.Context, _ string) (driverpkg.Key, error) {
	return driverpkg.Key{}, &driverpkg.Error{Op: "GetKey", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) CreateKey(_ context.Context, _ driverpkg.KeySpec) (driverpkg.Key, error) {
	return driverpkg.Key{}, &driverpkg.Error{Op: "CreateKey", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driverpkg.BucketPermission) error {
	return &driverpkg.Error{Op: "UpdateKeyPermissions", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) DeleteKey(_ context.Context, _ string) error {
	return &driverpkg.Error{Op: "DeleteKey", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) UpdateBucket(_ context.Context, _ string, _ driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	return driverpkg.Bucket{}, &driverpkg.Error{Op: "UpdateBucket", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, &driverpkg.Error{Op: "PresignGet", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, &driverpkg.Error{Op: "PresignPut", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) DeleteObject(_ context.Context, _, _ string) error {
	return &driverpkg.Error{Op: "DeleteObject", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) CreateMultipart(_ context.Context, _, _, _ string) (driverpkg.MultipartUpload, error) {
	return driverpkg.MultipartUpload{}, &driverpkg.Error{Op: "CreateMultipart", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) PresignUploadPart(_ context.Context, _ driverpkg.MultipartUpload, _ int) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, &driverpkg.Error{Op: "PresignUploadPart", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) CompleteMultipart(_ context.Context, _ driverpkg.MultipartUpload, _ []driverpkg.CompletedPart) error {
	return &driverpkg.Error{Op: "CompleteMultipart", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) AbortMultipart(_ context.Context, _ driverpkg.MultipartUpload) error {
	return &driverpkg.Error{Op: "AbortMultipart", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) ListObjects(_ context.Context, _, _, _ string, _ int) (driverpkg.ObjectPage, error) {
	return driverpkg.ObjectPage{}, &driverpkg.Error{Op: "ListObjects", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

func (*testDriver) StatObject(_ context.Context, _, _ string) (driverpkg.ObjectInfo, error) {
	return driverpkg.ObjectInfo{}, &driverpkg.Error{Op: "StatObject", Driver: driverName, Err: driverpkg.ErrUnsupported, Message: "not implemented"}
}

// TestNewDriver tests driver creation from config.
func TestNewDriver(t *testing.T) {
	cfg := map[string]string{
		"region":     "us-east-1",
		"access_key": "test-access-key",
		"secret_key": "test-secret-key",
	}

	drv, err := newDriver(cfg)
	if err != nil {
		t.Fatalf("newDriver() error = %v", err)
	}

	if drv == nil {
		t.Fatal("expected non-nil driver")
	}
}

// TestNewDriver_InvalidConfig tests driver creation with missing config.
func TestNewDriver_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]string
	}{
		{"missing region", map[string]string{"access_key": "k", "secret_key": "s"}},
		{"missing access key", map[string]string{"region": "us-east-1", "secret_key": "s"}},
		{"missing secret key", map[string]string{"region": "us-east-1", "access_key": "k"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newDriver(tt.cfg)
			if err == nil {
				t.Fatal("expected error for missing config")
			}
		})
	}
}

// TestNewDriver_WithEndpoint tests driver creation with optional endpoint.
func TestNewDriver_WithEndpoint(t *testing.T) {
	cfg := map[string]string{
		"region":     "us-east-1",
		"access_key": "test-access-key",
		"secret_key": "test-secret-key",
		"endpoint":   "http://localhost:9000",
	}

	drv, err := newDriver(cfg)
	if err != nil {
		t.Fatalf("newDriver() error = %v", err)
	}

	if drv == nil {
		t.Fatal("expected non-nil driver")
	}
}

// TestPresignGet_Happy tests PresignGet returns unsupported.
func TestPresignGet_Happy(t *testing.T) {
	drv := &testDriver{}
	url, err := drv.PresignGet(context.Background(), "bucket", "key", 1*time.Hour)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if url.URL != "" {
		t.Error("expected empty URL")
	}
}

// TestPresignPut_Happy tests PresignPut returns unsupported.
func TestPresignPut_Happy(t *testing.T) {
	drv := &testDriver{}
	url, err := drv.PresignPut(context.Background(), "bucket", "key", 1*time.Hour, "")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if url.Method != "" {
		t.Error("expected empty method")
	}
}

// TestDeleteObject_Happy tests DeleteObject returns unsupported.
func TestDeleteObject_Happy(t *testing.T) {
	drv := &testDriver{}
	err := drv.DeleteObject(context.Background(), "bucket", "key")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestCreateMultipart_Happy tests CreateMultipart returns unsupported.
func TestCreateMultipart_Happy(t *testing.T) {
	drv := &testDriver{}
	mu, err := drv.CreateMultipart(context.Background(), "bucket", "key", "")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if mu.UploadID != "" {
		t.Error("expected empty upload ID")
	}
}

// TestPresignUploadPart_Happy tests PresignUploadPart returns unsupported.
func TestPresignUploadPart_Happy(t *testing.T) {
	drv := &testDriver{}
	_, err := drv.PresignUploadPart(context.Background(), driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"}, 1)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestCompleteMultipart_Happy tests CompleteMultipart returns unsupported.
func TestCompleteMultipart_Happy(t *testing.T) {
	drv := &testDriver{}
	err := drv.CompleteMultipart(context.Background(), driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestAbortMultipart_Happy tests AbortMultipart returns unsupported.
func TestAbortMultipart_Happy(t *testing.T) {
	drv := &testDriver{}
	err := drv.AbortMultipart(context.Background(), driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"})
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestListObjects_Happy tests ListObjects returns unsupported.
func TestListObjects_Happy(t *testing.T) {
	drv := &testDriver{}
	_, err := drv.ListObjects(context.Background(), "bucket", "", "", 100)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestStatObject_Happy tests StatObject returns unsupported.
func TestStatObject_Happy(t *testing.T) {
	drv := &testDriver{}
	_, err := drv.StatObject(context.Background(), "bucket", "key")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestCapabilities tests the Capabilities method.
func TestCapabilities(t *testing.T) {
	drv := &testDriver{}

	caps, err := drv.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}

	if caps.Driver != driverName {
		t.Errorf("caps.Driver = %q, want %q", caps.Driver, driverName)
	}
	if caps.Layout != driverpkg.LayoutReadonly {
		t.Errorf("caps.Layout = %q, want %q", caps.Layout, driverpkg.LayoutReadonly)
	}
	if caps.Quotas {
		t.Error("expected Quotas=false for AWS S3")
	}
	if caps.BucketAliases {
		t.Error("expected BucketAliases=false for AWS S3")
	}
	if caps.KeyModel != driverpkg.KeyModelIAM {
		t.Errorf("caps.KeyModel = %q, want %q", caps.KeyModel, driverpkg.KeyModelIAM)
	}
	if !caps.Presign {
		t.Error("expected Presign=true for AWS S3")
	}
	if !caps.Multipart {
		t.Error("expected Multipart=true for AWS S3")
	}
	if !caps.Versioning {
		t.Error("expected Versioning=true for AWS S3")
	}
}

// TestHealthCheck tests health check returns healthy.
func TestHealthCheck(t *testing.T) {
	drv := &testDriver{}

	report, err := drv.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	if report.Status != "healthy" {
		t.Errorf("status = %q, want %q", report.Status, "healthy")
	}
}

// TestListNodes tests that ListNodes returns unsupported.
func TestListNodes(t *testing.T) {
	drv := &testDriver{}

	nodes, err := drv.ListNodes(context.Background())
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if nodes != nil {
		t.Error("expected nil nodes")
	}
}

// TestGetLayout tests that GetLayout returns unsupported.
func TestGetLayout(t *testing.T) {
	drv := &testDriver{}

	layout, err := drv.GetLayout(context.Background())
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if layout.Version != 0 {
		t.Error("expected zero version")
	}
}

// TestStageLayout tests that StageLayout returns unsupported.
func TestStageLayout(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.StageLayout(context.Background(), driverpkg.LayoutChange{})
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestApplyLayout tests that ApplyLayout returns unsupported.
func TestApplyLayout(t *testing.T) {
	drv := &testDriver{}

	err := drv.ApplyLayout(context.Background())
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestRevertLayout tests that RevertLayout returns unsupported.
func TestRevertLayout(t *testing.T) {
	drv := &testDriver{}

	err := drv.RevertLayout(context.Background())
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestListKeys tests that ListKeys returns unsupported.
func TestListKeys(t *testing.T) {
	drv := &testDriver{}

	keys, err := drv.ListKeys(context.Background())
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if keys != nil {
		t.Error("expected nil keys")
	}
}

// TestGetKey tests that GetKey returns unsupported.
func TestGetKey(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.GetKey(context.Background(), "test-key")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestCreateKey tests that CreateKey returns unsupported.
func TestCreateKey(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.CreateKey(context.Background(), driverpkg.KeySpec{Name: "test"})
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestUpdateKeyPermissions tests that UpdateKeyPermissions returns unsupported.
func TestUpdateKeyPermissions(t *testing.T) {
	drv := &testDriver{}

	err := drv.UpdateKeyPermissions(context.Background(), "test-key", nil)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestDeleteKey tests that DeleteKey returns unsupported.
func TestDeleteKey(t *testing.T) {
	drv := &testDriver{}

	err := drv.DeleteKey(context.Background(), "test-key")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestUpdateBucket tests that UpdateBucket returns unsupported.
func TestUpdateBucket(t *testing.T) {
	drv := &testDriver{}

	bucket, err := drv.UpdateBucket(context.Background(), "test-bucket", driverpkg.BucketUpdate{})
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
	if bucket.ID != "" {
		t.Error("expected empty bucket")
	}
}

// TestPresignGet_WithTTL tests PresignGet with custom TTL.
func TestPresignGet_WithTTL(t *testing.T) {
	drv := &testDriver{}
	_, err := drv.PresignGet(context.Background(), "bucket", "key", 5*time.Minute)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestPresignPut_WithContentType tests PresignPut with content type.
func TestPresignPut_WithContentType(t *testing.T) {
	drv := &testDriver{}
	_, err := drv.PresignPut(context.Background(), "bucket", "key", 1*time.Hour, "text/plain")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestDeleteObject tests that DeleteObject returns unsupported.
func TestDeleteObject(t *testing.T) {
	drv := &testDriver{}

	err := drv.DeleteObject(context.Background(), "bucket", "key")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestCreateMultipart tests that CreateMultipart returns unsupported.
func TestCreateMultipart(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.CreateMultipart(context.Background(), "bucket", "key", "text/plain")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestPresignUploadPart tests that PresignUploadPart returns unsupported.
func TestPresignUploadPart(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.PresignUploadPart(context.Background(), driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"}, 1)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestCompleteMultipart tests that CompleteMultipart returns unsupported.
func TestCompleteMultipart(t *testing.T) {
	drv := &testDriver{}

	err := drv.CompleteMultipart(context.Background(), driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestAbortMultipart tests that AbortMultipart returns unsupported.
func TestAbortMultipart(t *testing.T) {
	drv := &testDriver{}

	err := drv.AbortMultipart(context.Background(), driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"})
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestListObjects tests that ListObjects returns unsupported.
func TestListObjects(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.ListObjects(context.Background(), "bucket", "prefix", "", 100)
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}

// TestStatObject tests that StatObject returns unsupported.
func TestStatObject(t *testing.T) {
	drv := &testDriver{}

	_, err := drv.StatObject(context.Background(), "bucket", "key")
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", de.Err)
	}
}
