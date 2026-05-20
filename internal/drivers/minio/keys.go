package minio

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListKeys returns ErrUnsupported since AWS access keys are managed in IAM,
// not through the S3 service API.
func (d *driver) ListKeys(_ context.Context) ([]driverpkg.Key, error) {
	return nil, d.unsupported("ListKeys")
}

// GetKey returns ErrUnsupported since AWS access keys are managed in IAM,
// not through the S3 service API.
func (d *driver) GetKey(_ context.Context, _ string) (driverpkg.Key, error) {
	return driverpkg.Key{}, d.unsupported("GetKey")
}

// CreateKey returns ErrUnsupported since AWS access keys are managed in IAM,
// not through the S3 service API.
func (d *driver) CreateKey(_ context.Context, _ driverpkg.KeySpec) (driverpkg.Key, error) {
	return driverpkg.Key{}, d.unsupported("CreateKey")
}

// UpdateKeyPermissions returns ErrUnsupported since AWS access key permissions
// are managed in IAM policies, not through the S3 service API.
func (d *driver) UpdateKeyPermissions(_ context.Context, _ string, _ []driverpkg.BucketPermission) error {
	return d.unsupported("UpdateKeyPermissions")
}

// DeleteKey returns ErrUnsupported since AWS access keys are managed in IAM,
// not through the S3 service API.
func (d *driver) DeleteKey(_ context.Context, _ string) error {
	return d.unsupported("DeleteKey")
}
