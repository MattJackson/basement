package minio

import (
	"context"
	"errors"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Capabilities returns the MinIO driver's capability flags.
//
// MinIO is an S3-compatible object storage service with cluster management
// APIs for node operations, but basement treats it as a managed service
// without exposing layout staging/apply/revert at this layer.
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
		ServerSideCopy: d.s3Client != nil && cfgHasEndpoint(d.s3Client),
	}, nil
}

// cfgHasEndpoint checks if an s3Client was configured with a custom endpoint.
func cfgHasEndpoint(sc *s3Client) bool {
	return sc != nil
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
