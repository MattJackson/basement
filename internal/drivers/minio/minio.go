// Package minio implements the driver.Driver interface against AWS S3 using
// the aws-sdk-go-v2 SDK. This driver provides S3 data-plane operations while
// cluster-management methods return ErrUnsupported since AWS manages the
// infrastructure itself.
//
// The driver is registered under the name "aws-s3" and can be selected via
// BASEMENT_DRIVER=minio with required environment variables:
//   - BASEMENT_DRIVER_MINIO_REGION (required)
//   - BASEMENT_DRIVER_MINIO_ACCESS_KEY (required)
//   - BASEMENT_DRIVER_MINIO_SECRET_KEY (required)
//   - BASEMENT_DRIVER_MINIO_ENDPOINT (optional, for S3-compatible endpoints)
package minio

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

const driverName = "minio"

func init() {
	driverpkg.Register(driverName, newDriver)
}

// driver implements the driver.Driver interface against AWS S3.
type driver struct {
	s3Client *s3Client
}

// newDriver constructs an AWS S3 driver from the given Config.
//
// Config keys:
//   - "region": AWS region (e.g., "us-east-1")
//   - "access_key": AWS access key ID
//   - "secret_key": AWS secret access key
//   - "endpoint": optional S3-compatible endpoint URL
func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	// Driver construction happens inside the registry Factory (no caller
	// context available here); pass Background. The ctx parameter exists so
	// the SDK config load is cancellation-aware where a context can be
	// threaded.
	s3Client, err := newS3Client(context.Background(), cfg)
	if err != nil {
		return nil, &driverpkg.Error{
			Op:      "newDriver",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return &driver{s3Client: s3Client}, nil
}

// unsupported builds a driver.Error wrapping ErrUnsupported for stub methods.
func (d *driver) unsupported(op string) error {
	return &driverpkg.Error{
		Op:      op,
		Driver:  driverName,
		Err:     driverpkg.ErrUnsupported,
		Message: "not implemented",
	}
}
