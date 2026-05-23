package smb

import (
	"context"
	"testing"

	"github.com/mattjackson/basement/internal/gateway"
)

func TestImplementsGatewayInterface(t *testing.T) {
	var _ gateway.Gateway = (*Gateway)(nil)
}

func TestSMB_StubContract(t *testing.T) {
	g := New()
	if g.Name() != "smb" {
		t.Errorf("Name: want smb, got %q", g.Name())
	}
	if g.DisplayName() == "" {
		t.Errorf("DisplayName: want non-empty")
	}
	if g.Description() == "" {
		t.Errorf("Description: want non-empty")
	}
	if g.Implemented() {
		t.Errorf("Implemented: want false for stub")
	}
	if g.HTTPHandler() != nil {
		t.Errorf("HTTPHandler: want nil for port-bound stub")
	}
	if err := g.Start(context.Background()); err != nil {
		t.Errorf("Start: want nil, got %v", err)
	}
	if err := g.Stop(context.Background()); err != nil {
		t.Errorf("Stop: want nil, got %v", err)
	}
	if g.Status().Running {
		t.Errorf("Status.Running: want false for stub")
	}
	caps := g.Capabilities()
	// SMB advertises read/write/delete/move when implemented.
	if !caps.Read || !caps.Write || !caps.Delete || !caps.Move {
		t.Errorf("Capabilities: want read+write+delete+move; got %+v", caps)
	}
}
