// Package aws_s3 implements the driver.Driver interface against AWS S3 using
// the aws-sdk-go-v2 SDK. This driver provides S3 data-plane operations while
// cluster-management methods return ErrUnsupported since AWS manages the
// infrastructure itself.
//
// The driver is registered under the name "aws-s3" and can be selected via
// BASEMENT_DRIVER=aws-s3 with required environment variables:
//   - BASEMENT_DRIVER_AWS_S3_REGION (required)
//   - BASEMENT_DRIVER_AWS_S3_ACCESS_KEY (required)
//   - BASEMENT_DRIVER_AWS_S3_SECRET_KEY (required)
//   - BASEMENT_DRIVER_AWS_S3_ENDPOINT (optional, for S3-compatible endpoints)
package aws_s3

import (
	driverpkg "github.com/mattjackson/basement/internal/driver"
)

const driverName = "aws-s3"

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
	s3Client, err := newS3Client(cfg)
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
