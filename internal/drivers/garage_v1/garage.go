// Package garage_v1 implements the driver.Driver interface against the
// Garage v1 admin API (paths under /v1/*), as spoken by Garage 1.0.1.
//
// This is a parallel implementation to internal/drivers/garage (the v2
// driver). Garage 1.0.1 only exposes the v1 admin API generation, so the
// operator selects this driver via BASEMENT_DRIVER=garage-v1.
//
// The driver supports S3-compatible operations (presign, multipart) by
// connecting to Garage's S3 endpoint on a separate port. Configuration:
//   - admin_url: e.g. http://garage:3903 (admin API)
//   - admin_token: bearer token for admin API auth
//   - s3_endpoint: e.g. http://garage:3972 (S3-compatible data plane)
//   - access_key_id, secret_key: credentials for S3 endpoint
//
// Specification: basement-internal/upstream/garage-admin-v1.yml
package garage_v1 //nolint:revive // package name matches the API generation we target

import (
	"fmt"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// driverName is the name under which this driver is registered.
const driverName = "garage-v1"

func init() {
	driverpkg.Register(driverName, newDriver)
}

// driver is the Garage v1 admin API implementation of driver.Driver.
type driver struct {
	client     *client
	s3Endpoint string
	accessKey  string
	secretKey  string
	s3Client   *s3Client
}

// newDriver constructs a Garage v1 driver from the given Config.
//
// Config keys:
//   - admin_url: e.g. http://garage:3903
//   - admin_token: bearer token for the admin API
//   - s3_endpoint: S3-compatible endpoint for presign (Garage exposes this on separate port)
//   - access_key_id: credentials for S3 endpoint auth
//   - secret_key: credentials for S3 endpoint auth
func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	d := &driver{
		client:     newClient(cfg),
		s3Endpoint: cfg["s3_endpoint"],
		accessKey:  cfg["access_key_id"],
		secretKey:  cfg["secret_key"],
	}

	if d.s3Endpoint != "" {
		var err error
		d.s3Client, err = newS3Client(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 client for endpoint %q: %w", d.s3Endpoint, err)
		}
	}

	return d, nil
}

// unsupported builds a driver.Error wrapping ErrUnsupported for stub methods.
func (d *driver) unsupported(op string) error {
	return &driverpkg.Error{
		Op:      op,
		Driver:  driverName,
		Err:     driverpkg.ErrUnsupported,
		Message: "not implemented yet",
	}
}
