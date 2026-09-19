//go:build windows

package service

import (
	"fmt"
	"testing"
	"time"
)

// TestTaskStateRealProbeReportsAbsent runs the real Get-ScheduledTask probe
// against a task that cannot exist: it must answer "not found" without an
// error, which is what distinguishes it from the old parser that turned every
// failure into "not registered" (MADR 0159 F12).
func TestTaskStateRealProbeReportsAbsent(t *testing.T) {
	name := fmt.Sprintf("mcr-no-such-task-%d", time.Now().UnixNano())
	state, found, err := taskState(name)
	if err != nil {
		t.Fatalf("probe of an absent task errored: %v", err)
	}
	if found {
		t.Fatalf("probe reported %q as registered (state %d)", name, state)
	}
}
