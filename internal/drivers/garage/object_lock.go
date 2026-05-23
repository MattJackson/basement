package garage

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Object Lock support (v1.10.0c).
//
// TODO(v1.10.x): research whether Garage v2's S3 endpoint accepts
// PutObjectLockConfiguration / GetObjectRetention / etc. As of the
// cycle that shipped this stub, upstream Garage does not implement
// Object Lock in its S3 surface — same posture as bucket versioning.
// The driver returns ObjectLockSupport=false so the FE hides the
// settings card entirely and the API layer surfaces 501 NOT_SUPPORTED
// on direct callers.
//
// If upstream adds Object Lock later, swap this file for an
// implementation that proxies through the s3Client the same way
// aws_s3/object_lock.go does — the wire contract is S3-shaped and
// the Driver method shape is already in place.

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
