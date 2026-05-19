package garage

import (
	"errors"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestOpenGarage(t *testing.T) {
	d, err := driverpkg.Open("garage", driverpkg.Config{})
	if err != nil {
		t.Fatalf("driverpkg.Open(\"garage\", ...) returned error: %v", err)
	}
	if d == nil {
		t.Fatal("driverpkg.Open(\"garage\", ...) returned nil Driver")
	}
}

func TestUnsupportedError(t *testing.T) {
	d, err := driverpkg.Open("garage", driverpkg.Config{})
	if err != nil {
		t.Fatalf("driverpkg.Open(\"garage\", ...) returned error: %v", err)
	}

	_, err = d.Capabilities(nil)
	if !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("d.Capabilities() error does not match ErrUnsupported: got %v", err)
	}
}

func TestErrorStructure(t *testing.T) {
	d, err := driverpkg.Open("garage", driverpkg.Config{})
	if err != nil {
		t.Fatalf("driverpkg.Open(\"garage\", ...) returned error: %v", err)
	}

	_, err = d.Capabilities(nil)
	errTyped, ok := err.(*driverpkg.Error)
	if !ok {
		t.Fatalf("error is not *driverpkg.Error, got type %T", err)
	}

	if errTyped.Op != "Capabilities" {
		t.Errorf("errTyped.Op = %q, want \"Capabilities\"", errTyped.Op)
	}

	if errTyped.Driver != "garage" {
		t.Errorf("errTyped.Driver = %q, want \"garage\"", errTyped.Driver)
	}
}
