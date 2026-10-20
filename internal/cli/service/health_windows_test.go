//go:build windows

package service

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// TestTaskOptionsAreAccepted: with RequestStop set, taskscheduler.New
// requires a StopGrace shorter than the poll timeout, 60 seconds
// (taskscheduler/task.go:149-150). The grace mcremote passes is accepted, and
// one that is not shorter is refused, so the constraint this test relies on
// is real (MADR 0054 A2; PLAN S8 step 1c).
func TestTaskOptionsAreAccepted(t *testing.T) {
	stop := func(context.Context, ServeArgs) error { return nil }
	if _, err := taskscheduler.New(taskOptions("mcremote", "", "mcremote", HealthOptions{RequestStop: stop, StopGrace: 30 * time.Second})); err != nil {
		t.Fatalf("New refused mcremote's task options: %v", err)
	}
	if _, err := taskscheduler.New(taskOptions("mcremote", "", "mcremote", HealthOptions{RequestStop: stop, StopGrace: time.Minute})); err == nil {
		t.Fatal("New accepted a StopGrace equal to the poll timeout")
	}
}
