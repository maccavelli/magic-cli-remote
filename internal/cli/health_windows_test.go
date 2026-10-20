//go:build windows

package cli

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// TestUpdateHealthBuildsTheTaskBackend: the Task Scheduler backend accepts
// mcremote's own wiring, whose StopGrace must be shorter than the poll
// timeout once RequestStop is set (MADR 0054 A2; PLAN S8 step 1c).
func TestUpdateHealthBuildsTheTaskBackend(t *testing.T) {
	h := updateHealth()
	if h.RequestStop == nil || h.Probe == nil {
		t.Fatal("mcremote's health wiring has no probe or no stop request")
	}
	if _, err := service.NewBackendWith("mcremote", "", h); err != nil {
		t.Fatalf("NewBackendWith refused mcremote's health wiring: %v", err)
	}
}
