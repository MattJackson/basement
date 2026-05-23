package garage

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Bucket versioning support (v1.10.0a).
//
// TODO(v1.10.x): research whether Garage v2's S3 endpoint accepts
// PutBucketVersioning + ListObjectVersions. As of the cycle that
// shipped this stub, upstream Garage does not implement bucket
// versioning in its S3 surface (the team's stated position is that
// versioning conflicts with their content-addressed block store
// model). The driver returns VersioningSupport=false so the FE
// hides the toggle entirely and the API layer surfaces 501
// NOT_SUPPORTED on direct callers.
//
// If upstream adds versioning later, swap this file for an
// implementation that proxies through the s3Client the same way
// aws_s3/versioning.go does — the wire contract is S3-shaped and
// the Driver method shape is already in place.

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
