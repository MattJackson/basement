package s3

import (
	"context"
	"testing"

	"github.com/mattjackson/basement/internal/gateway"
)

func TestImplementsGatewayInterface(t *testing.T) {
	var _ gateway.Gateway = (*Gateway)(nil)
}

func TestS3_StubContract(t *testing.T) {
	g := New()
	if g.Name() != "s3" {
		t.Errorf("Name: want s3, got %q", g.Name())
	}
	if g.Implemented() {
		t.Errorf("Implemented: want false for stub")
	}
	if g.HTTPHandler() != nil {
		// v1.9.0c stub returns nil; v2.0 will return a real handler.
		t.Errorf("HTTPHandler: want nil for v1.9.0c stub")
	}
	if err := g.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := g.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	caps := g.Capabilities()
	if !caps.Read || !caps.Write || !caps.Delete || !caps.Move {
		t.Errorf("Capabilities: want read+write+delete+move; got %+v", caps)
	}
	if !caps.SigV4Auth {
		t.Errorf("Capabilities: want SigV4Auth=true for S3 gateway; got %+v", caps)
	}
}
