package opencode

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// closingPlane is a control plane that records Close.
type closingPlane struct{ closed int }

func (c *closingPlane) Do(context.Context, control.Request) (control.Response, error) {
	return control.Response{}, nil
}

func (c *closingPlane) Close() { c.closed++ }

// TestACPShutdownClosesTheControlPlane: the daemon's Shutdown stops the control
// plane, rather than leaving it to its lease (0214 PLAN P7 Deviation 1).
func TestACPShutdownClosesTheControlPlane(t *testing.T) {
	plane := &closingPlane{}
	newACP(acpagent.Config{}, nil, plane).Shutdown()
	if plane.closed != 1 {
		t.Fatalf("the control plane was closed %d times, want 1", plane.closed)
	}
}

// The real control plane satisfies the interface newACP asserts, so the
// assertion cannot fail silently on a signature change.
func TestControlPlaneHasTheShutdownMethod(t *testing.T) {
	var plane controlPlane = control.New(control.Config{Bin: "opencode"})
	if _, ok := plane.(interface{ Close() }); !ok {
		t.Fatal("control.Plane has no Close(), so Shutdown would leave it running")
	}
}
