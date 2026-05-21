package garage_v1 //nolint:revive // package name matches the API generation we target

import (
	"context"
	"errors"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Garage v1's admin API doesn't expose lifecycle CRUD, so the driver
// reports Supported=false and both methods return ErrUnsupported.
// The UI gates on capabilities.Supported and never reaches the
// methods — these tests just confirm the wire contract.
func TestLifecycleSupport_ReportsUnsupported(t *testing.T) {
	d := &driver{}
	caps := d.LifecycleSupport()
	if caps.Supported {
		t.Fatalf("expected Supported=false for Garage v1")
	}
}

func TestGetLifecycle_ReturnsUnsupported(t *testing.T) {
	d := &driver{}
	_, err := d.GetLifecycle(context.Background(), "bid")
	if !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestPutLifecycle_ReturnsUnsupported(t *testing.T) {
	d := &driver{}
	err := d.PutLifecycle(context.Background(), "bid", nil)
	if !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}
