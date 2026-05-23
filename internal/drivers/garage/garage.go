// Package garage implements the garage device driver for Garage v2 admin API.
package garage

import (
	"fmt"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

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
	s3Client   *s3Client
}

func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	d := &driver{
		client:     newClient(cfg),
		s3Endpoint: cfg["s3_endpoint"],
		accessKey:  cfg["access_key_id"],
		secretKey:  cfg["secret_key"],
	}

	// Build the S3 client only when admin-tier creds are present.
	// ADR-0001 (v0.9.0d) strips per-user S3 creds from admin Connections;
	// admin-only drivers carry s3_endpoint but no access_key_id. Those
	// drivers serve admin-tier ops (bucket/key list via admin_token) and
	// the v1.1.0d region bridge picks them by endpoint, then signs with
	// the user's UserRegion key. So skip S3 client construction here when
	// creds are absent — they'll come from the user-region path instead.
	// Mirrors the v1 driver gate (internal/drivers/garage_v1/garage.go).
	if d.s3Endpoint != "" && d.accessKey != "" && d.secretKey != "" {
		var err error
		d.s3Client, err = newS3Client(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 client for endpoint %q: %w", d.s3Endpoint, err)
		}
	}

	return d, nil
}

func (d *driver) unsupported(op string) error {
	return &driverpkg.Error{
		Op:      op,
		Driver:  driverName,
		Err:     driverpkg.ErrUnsupported,
		Message: "not implemented yet",
	}
}
