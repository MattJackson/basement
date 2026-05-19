// Package garage_v1 implements the driver.Driver interface against the
// Garage v1 admin API (paths under /v1/*), as spoken by Garage 1.0.1.
//
// This is a parallel implementation to internal/drivers/garage (the v2
// driver). Garage 1.0.1 only exposes the v1 admin API generation, so the
// operator selects this driver via BASEMENT_DRIVER=garage-v1.
//
// Specification: basement-internal/upstream/garage-admin-v1.yml
package garage_v1 //nolint:revive // package name matches the API generation we target

import (
	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// driverName is the name under which this driver is registered.
const driverName = "garage-v1"

func init() {
	driverpkg.Register(driverName, newDriver)
}

// driver is the Garage v1 admin API implementation of driver.Driver.
type driver struct {
	client *client
}

// newDriver constructs a Garage v1 driver from the given Config.
//
// Config keys are the same as the v2 driver:
//   - admin_url: e.g. http://garage:3903
//   - admin_token: bearer token for the admin API
func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	return &driver{
		client: newClient(cfg),
	}, nil
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
