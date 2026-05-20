// Package garage implements the garage device driver for Garage v2 admin API.
package garage

import driverpkg "github.com/mattjackson/basement/internal/driver"

func init() {
	driverpkg.Register("garage", newDriver)
}

// driverName is the name under which this driver is registered.
const driverName = "garage"

// Config holds the configuration for the Garage v2 driver.
type Config struct {
	AdminURL    string `json:"admin_url"`
	AdminToken  string `json:"admin_token"`
	S3Endpoint  string `json:"s3_endpoint,omitempty"`
	AccessKeyID string `json:"access_key_id,omitempty"`
	SecretKey   string `json:"secret_key,omitempty"`
}

// driver is the Garage v2 admin API implementation of driver.Driver.
type driver struct {
	client     *client
	s3Endpoint string
	accessKey  string
	secretKey  string
}

func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	return &driver{
		client:     newClient(cfg),
		s3Endpoint: cfg["s3_endpoint"],
		accessKey:  cfg["access_key_id"],
		secretKey:  cfg["secret_key"],
	}, nil
}

func (d *driver) unsupported(op string) error {
	return &driverpkg.Error{
		Op:      op,
		Driver:  driverName,
		Err:     driverpkg.ErrUnsupported,
		Message: "not implemented yet",
	}
}
