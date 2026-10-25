//go:build windows

package service

import (
	"fmt"
	"testing"
	"time"
)

// TestTaskStatusReportsAbsent runs the real Task Scheduler read, through
// go-selfupdate-lib (MADR 0207 D17), against a task that cannot exist: it
// must answer "not installed" without an error, which is what distinguishes it
// from the old parser that turned every failure into "not registered" (MADR
// 0159 F12).
func TestTaskStatusReportsAbsent(t *testing.T) {
	name := fmt.Sprintf("mcr-no-such-task-%d", time.Now().UnixNano())
	st, err := taskStatus(name)
	if err != nil {
		t.Fatalf("read of an absent task errored: %v", err)
	}
	if st.Installed {
		t.Fatalf("read reported %q as registered: %+v", name, st)
	}
}
