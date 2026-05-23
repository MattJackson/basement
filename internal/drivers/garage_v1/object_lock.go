package garage_v1

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Object Lock support (v1.10.0c).
//
// TODO(v1.10.x): same posture as the Garage v2 driver — upstream
// Garage v1 does not implement Object Lock in its S3 surface. The
// driver returns ObjectLockSupport=false so the FE hides the
// settings card and direct API callers see 501 NOT_SUPPORTED.
//
// If a later Garage release adds Object Lock we'll mirror the
// aws_s3/object_lock.go implementation; the Driver method shape is
// already in place.

func (d *driver) ObjectLockSupport() bool { return false }

func (d *driver) GetObjectLockConfig(_ context.Context, _ string) (*driverpkg.ObjectLockConfig, error) {
	return nil, d.unsupported("GetObjectLockConfig")
}

func (d *driver) PutObjectLockConfig(_ context.Context, _ string, _ driverpkg.ObjectLockConfig) error {
	return d.unsupported("PutObjectLockConfig")
}

func (d *driver) GetObjectRetention(_ context.Context, _, _, _ string) (*driverpkg.ObjectLockRetention, error) {
	return nil, d.unsupported("GetObjectRetention")
}

func (d *driver) PutObjectRetention(_ context.Context, _, _, _ string, _ driverpkg.ObjectLockRetention, _ bool) error {
	return d.unsupported("PutObjectRetention")
}

func (d *driver) GetObjectLegalHold(_ context.Context, _, _, _ string) (bool, error) {
	return false, d.unsupported("GetObjectLegalHold")
}

func (d *driver) PutObjectLegalHold(_ context.Context, _, _, _ string, _ bool) error {
	return d.unsupported("PutObjectLegalHold")
}
