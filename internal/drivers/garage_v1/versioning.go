package garage_v1

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Bucket versioning support (v1.10.0a).
//
// TODO(v1.10.x): same posture as the Garage v2 driver — upstream
// Garage v1 does not implement bucket versioning in its S3 surface.
// The driver returns VersioningSupport=false so the FE hides the
// toggle and direct API callers see 501 NOT_SUPPORTED.
//
// If a later Garage release adds versioning we'll mirror the
// aws_s3/versioning.go implementation; the Driver method shape is
// already in place.

func (d *driver) VersioningSupport() bool { return false }

func (d *driver) GetVersioningStatus(_ context.Context, _ string) (driverpkg.VersioningStatus, error) {
	return driverpkg.VersioningDisabled, d.unsupported("GetVersioningStatus")
}

func (d *driver) EnableVersioning(_ context.Context, _ string) error {
	return d.unsupported("EnableVersioning")
}

func (d *driver) SuspendVersioning(_ context.Context, _ string) error {
	return d.unsupported("SuspendVersioning")
}

func (d *driver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]driverpkg.ObjectVersion, string, error) {
	return nil, "", d.unsupported("ListObjectVersions")
}

func (d *driver) GetObjectVersion(_ context.Context, _, _, _ string) (driverpkg.StreamResult, error) {
	return driverpkg.StreamResult{}, d.unsupported("GetObjectVersion")
}

func (d *driver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return d.unsupported("DeleteObjectVersion")
}
