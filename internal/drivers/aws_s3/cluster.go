package aws_s3

import (
	"context"
	"errors"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Capabilities returns the AWS S3 driver's capability flags.
//
// AWS S3 is a managed service with no cluster management API, hence:
//   - LayoutReadonly: no layout staging/apply/revert
//   - Quotas=false: bucket quotas not exposed via this driver layer
//   - BucketAliases=false: buckets are globally unique by name
//   - KeyModelIAM: keys are IAM-managed (not in S3 service)
//   - Presign=true, Multipart=true, Versioning=true, ObjectBrowse=true: native S3 features
func (d *driver) Capabilities(_ context.Context) (driverpkg.Caps, error) {
	return driverpkg.Caps{
		Driver:         driverName,
		Layout:         driverpkg.LayoutReadonly,
		Quotas:         false,
		BucketAliases:  false,
		KeyModel:       driverpkg.KeyModelIAM,
		Presign:        true,
		Multipart:      true,
		Versioning:     true,
		ObjectBrowse:   true,
	}, nil
}

// HealthCheck performs a minimal health check by calling ListBuckets.
// This is a cheap operation that validates credentials and connectivity.
func (d *driver) HealthCheck(ctx context.Context) (driverpkg.HealthReport, error) {
	_, err := d.s3Client.listBuckets(ctx)
	if err != nil {
		// Map 403/401 errors to unauthorized status
		var apiErr interface{ ErrorCode() string }
		if ok := errors.As(err, &apiErr); ok && (apiErr.ErrorCode() == "InvalidSignature" || apiErr.ErrorCode() == "AccessDenied" || apiErr.ErrorCode() == "NoSuchBucket") {
			return driverpkg.HealthReport{
				Status:  "unauthorized",
				Details: map[string]any{"error": err.Error()},
			}, nil
		}

		return driverpkg.HealthReport{
			Status:  "unhealthy",
			Details: map[string]any{"error": err.Error()},
		}, nil
	}

	return driverpkg.HealthReport{
		Status: "healthy",
	}, nil
}

// ListNodes returns ErrUnsupported since AWS S3 has no cluster node model.
func (d *driver) ListNodes(_ context.Context) ([]driverpkg.Node, error) {
	return nil, d.unsupported("ListNodes")
}

// GetLayout returns ErrUnsupported since AWS S3 has no cluster layout concept.
func (d *driver) GetLayout(_ context.Context) (driverpkg.Layout, error) {
	return driverpkg.Layout{}, d.unsupported("GetLayout")
}

// StageLayout returns ErrUnsupported since AWS S3 has no layout staging.
func (d *driver) StageLayout(_ context.Context, _ driverpkg.LayoutChange) (driverpkg.LayoutDiff, error) {
	return driverpkg.LayoutDiff{}, d.unsupported("StageLayout")
}

// ApplyLayout returns ErrUnsupported since AWS S3 has no layout apply.
func (d *driver) ApplyLayout(_ context.Context) error {
	return d.unsupported("ApplyLayout")
}

// RevertLayout returns ErrUnsupported since AWS S3 has no layout revert.
func (d *driver) RevertLayout(_ context.Context) error {
	return d.unsupported("RevertLayout")
}
