package ftp

import (
	"context"
	"testing"

	"github.com/mattjackson/basement/internal/gateway"
)

func TestImplementsGatewayInterface(t *testing.T) {
	var _ gateway.Gateway = (*Gateway)(nil)
}

func TestFTP_StubContract(t *testing.T) {
	g := New()
	if g.Name() != "ftp" {
		t.Errorf("Name: want ftp, got %q", g.Name())
	}
	if g.Implemented() {
		t.Errorf("Implemented: want false for stub")
	}
	if g.HTTPHandler() != nil {
		t.Errorf("HTTPHandler: want nil for port-bound stub")
	}
	if err := g.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := g.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	caps := g.Capabilities()
	if !caps.Read || !caps.Write || !caps.Delete {
		t.Errorf("Capabilities: want read+write+delete; got %+v", caps)
	}
	if !caps.BasicAuth {
		t.Errorf("Capabilities: want BasicAuth=true; got %+v", caps)
	}
}
