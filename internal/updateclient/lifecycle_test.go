package updateclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maccavelli/mcplib/selfupdate"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

func TestLifecycleDelegates(t *testing.T) {
	var stopped, started bool
	l := &Lifecycle{
		InstalledFn: func(string) (bool, error) { return true, nil },
		RunningFn:   func(string) (bool, error) { return true, nil },
		StopFn:      func(string) error { stopped = true; return nil },
		StartFn:     func(string) error { started = true; return nil },
	}
	ctx := context.Background()
	if ok, err := l.Installed(ctx, "mcremote"); err != nil || !ok {
		t.Fatalf("Installed = %v, %v", ok, err)
	}
	if ok, err := l.Running(ctx, "mcremote"); err != nil || !ok {
		t.Fatalf("Running = %v, %v", ok, err)
	}
	if err := l.Stop(ctx, "mcremote"); err != nil || !stopped {
		t.Fatalf("Stop = %v, stopped=%v", err, stopped)
	}
	if err := l.Start(ctx, "mcremote"); err != nil || !started {
		t.Fatalf("Start = %v, started=%v", err, started)
	}
}

// TestLifecycleHonoursCancellation proves an interrupted update stops asking
// the service manager instead of running to its own deadline.
func TestLifecycleHonoursCancellation(t *testing.T) {
	l := &Lifecycle{
		InstalledFn: func(string) (bool, error) { t.Fatal("called after cancel"); return false, nil },
		RunningFn:   func(string) (bool, error) { t.Fatal("called after cancel"); return false, nil },
		StopFn:      func(string) error { t.Fatal("called after cancel"); return nil },
		StartFn:     func(string) error { t.Fatal("called after cancel"); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Installed(ctx, "p"); !errors.Is(err, context.Canceled) {
		t.Errorf("Installed err = %v", err)
	}
	if _, err := l.Running(ctx, "p"); !errors.Is(err, context.Canceled) {
		t.Errorf("Running err = %v", err)
	}
	if err := l.Stop(ctx, "p"); !errors.Is(err, context.Canceled) {
		t.Errorf("Stop err = %v", err)
	}
	if err := l.Start(ctx, "p"); !errors.Is(err, context.Canceled) {
		t.Errorf("Start err = %v", err)
	}
}

func TestWaitHealthyReturnsWhenServiceComesUp(t *testing.T) {
	calls := 0
	l := &Lifecycle{
		RunningFn: func(string) (bool, error) { calls++; return calls >= 3, nil },
		Poll:      time.Millisecond,
	}
	if err := l.WaitHealthy(context.Background(), "mcremote"); err != nil {
		t.Fatalf("WaitHealthy = %v", err)
	}
	if calls < 3 {
		t.Fatalf("polled %d times, expected to wait for the third", calls)
	}
}

// TestWaitHealthyTimesOut is the case the previous implementation missed:
// Start returning nil is not evidence the unit came up, so a service that
// never becomes active must fail and let the shared rollback run.
func TestWaitHealthyTimesOut(t *testing.T) {
	l := &Lifecycle{
		RunningFn:     func(string) (bool, error) { return false, nil },
		Poll:          time.Millisecond,
		HealthTimeout: 20 * time.Millisecond,
	}
	err := l.WaitHealthy(context.Background(), "mcremote")
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

// TestWaitHealthyReportsProbeFailure keeps the underlying reason instead of
// reporting a bare timeout the operator cannot act on.
func TestWaitHealthyReportsProbeFailure(t *testing.T) {
	probe := errors.New("systemctl exploded")
	l := &Lifecycle{
		RunningFn:     func(string) (bool, error) { return false, probe },
		Poll:          time.Millisecond,
		HealthTimeout: 20 * time.Millisecond,
	}
	err := l.WaitHealthy(context.Background(), "mcremote")
	if !errors.Is(err, probe) {
		t.Fatalf("error = %v, want it to wrap %v", err, probe)
	}
}

func TestReconcilerRoundTrip(t *testing.T) {
	want := service.UnitRefresh{Changed: true, Path: "/u/x.service", BackupPath: "/u/x.bak", Output: "refreshed"}
	var restored service.UnitRefresh
	r := &Reconciler{
		RefreshFn: func(product, binary string) (service.UnitRefresh, error) { return want, nil },
		RestoreFn: func(product string, got service.UnitRefresh) error { restored = got; return nil },
	}
	res, err := r.Reconcile(context.Background(), "mcremote", "/bin/mcremote")
	if err != nil {
		t.Fatalf("Reconcile = %v", err)
	}
	if !res.Changed || res.Detail != "refreshed" {
		t.Fatalf("result = %+v", res)
	}
	if err := r.Restore(context.Background(), "mcremote", res); err != nil {
		t.Fatalf("Restore = %v", err)
	}
	if restored != want {
		t.Fatalf("restored = %+v, want %+v", restored, want)
	}
}

// TestReconcilerRestoreSkipsUnchanged avoids touching a definition that was
// never rewritten.
func TestReconcilerRestoreSkipsUnchanged(t *testing.T) {
	r := &Reconciler{RestoreFn: func(string, service.UnitRefresh) error {
		t.Fatal("restored a definition that was never changed")
		return nil
	}}
	if err := r.Restore(context.Background(), "p", selfupdate.ReconcileResult{Changed: false}); err != nil {
		t.Fatalf("Restore = %v", err)
	}
}

// TestReconcilerRestoreRejectsForeignReceipt refuses to report a rollback it
// did not perform.
func TestReconcilerRestoreRejectsForeignReceipt(t *testing.T) {
	r := &Reconciler{RestoreFn: func(string, service.UnitRefresh) error {
		t.Fatal("restored from a receipt this adapter did not produce")
		return nil
	}}
	err := r.Restore(context.Background(), "p", selfupdate.ReconcileResult{Changed: true, State: "not a UnitRefresh"})
	if err == nil {
		t.Fatal("expected an error for a foreign receipt")
	}
}

func TestReconcilerPropagatesFailure(t *testing.T) {
	boom := errors.New("refresh failed")
	r := &Reconciler{RefreshFn: func(string, string) (service.UnitRefresh, error) { return service.UnitRefresh{}, boom }}
	if _, err := r.Reconcile(context.Background(), "p", "/bin/p"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
}
