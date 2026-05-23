package garage_v1

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Bucket default encryption support (v1.10.0d).
//
// TODO(v1.10.x): same posture as the Garage v2 driver — upstream
// Garage v1 does not implement bucket default encryption in its S3
// surface. The driver returns SSESupport=(false, false) so the FE
// hides the settings card and direct API callers see 501
// NOT_SUPPORTED.
//
// If a later Garage release adds default encryption we'll mirror the
// aws_s3/encryption.go implementation; the Driver method shape is
// already in place.

func (d *driver) SSESupport() (bool, bool) { return false, false }

func (d *driver) GetBucketEncryption(_ context.Context, _ string) (*driverpkg.BucketEncryption, error) {
	return nil, d.unsupported("GetBucketEncryption")
}

func (d *driver) PutBucketEncryption(_ context.Context, _ string, _ driverpkg.BucketEncryption) error {
	return d.unsupported("PutBucketEncryption")
}

func (d *driver) DeleteBucketEncryption(_ context.Context, _ string) error {
	return d.unsupported("DeleteBucketEncryption")
}
