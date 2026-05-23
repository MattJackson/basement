package garage

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Bucket default encryption support (v1.10.0d).
//
// TODO(v1.10.x): research whether Garage v2's S3 endpoint accepts
// PutBucketEncryption / GetBucketEncryption. As of the cycle that
// shipped this stub, upstream Garage does not implement default
// encryption at the admin layer — same posture as Object Lock. The
// driver returns SSESupport=(false, false) so the FE hides the
// settings card entirely and the API layer surfaces 501 NOT_SUPPORTED
// on direct callers.
//
// If upstream adds default encryption later, swap this file for an
// implementation that proxies through the s3Client the same way
// aws_s3/encryption.go does — the wire contract is S3-shaped and the
// Driver method shape is already in place.

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
