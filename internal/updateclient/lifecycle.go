package updateclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maccavelli/mcplib/selfupdate"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// healthPoll is how often WaitHealthy re-checks an started service.
const healthPoll = 250 * time.Millisecond

// healthTimeout bounds WaitHealthy when the caller's context has no earlier
// deadline. Start returning nil only means the service manager accepted the
// command; it does not mean the unit came up (MADR 0100 F3).
const healthTimeout = 30 * time.Second

// Lifecycle adapts this repository's user-service control to the shared
// selfupdate.Lifecycle seam. Every method carries the caller's context so an
// interrupted update stops waiting instead of blocking to its own deadline.
type Lifecycle struct {
	// InstalledFn, RunningFn, StopFn and StartFn default to the service
	// package's implementations. Tests inject fakes.
	InstalledFn func(product string) (bool, error)
	RunningFn   func(product string) (bool, error)
	StopFn      func(product string) error
	StartFn     func(product string) error
	// HealthTimeout bounds WaitHealthy. Zero selects healthTimeout.
	HealthTimeout time.Duration
	// Poll is the health re-check interval. Zero selects healthPoll.
	Poll time.Duration
}

var _ selfupdate.Lifecycle = (*Lifecycle)(nil)

// Installed reports whether a service definition exists at all, regardless of
// whether its process is running. Without this distinction an update starts a
// service that was never installed, fails, and rolls back a good swap.
func (l *Lifecycle) Installed(ctx context.Context, product string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	fn := l.InstalledFn
	if fn == nil {
		fn = service.IsInstalled
	}
	return fn(product)
}

// Running reports whether the installed definition's process is active.
func (l *Lifecycle) Running(ctx context.Context, product string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	fn := l.RunningFn
	if fn == nil {
		fn = service.IsActive
	}
	return fn(product)
}

// Stop halts the running service.
func (l *Lifecycle) Stop(ctx context.Context, product string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fn := l.StopFn
	if fn == nil {
		fn = service.Stop
	}
	return fn(product)
}

// Start launches the service.
func (l *Lifecycle) Start(ctx context.Context, product string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fn := l.StartFn
	if fn == nil {
		fn = service.Start
	}
	return fn(product)
}

// WaitHealthy polls until the service reports active, the deadline passes, or
// the caller cancels. A service manager accepting `start` is not evidence the
// unit came up, so the shared rollback path must not be skipped on that basis.
func (l *Lifecycle) WaitHealthy(ctx context.Context, product string) error {
	timeout := l.HealthTimeout
	if timeout <= 0 {
		timeout = healthTimeout
	}
	poll := l.Poll
	if poll <= 0 {
		poll = healthPoll
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var lastErr error
	for {
		running, err := l.Running(ctx, product)
		switch {
		case err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled):
			// A probe failure is not yet fatal: the manager may still be
			// bringing the unit up. Keep the reason for the timeout message.
			lastErr = err
		case running:
			return nil
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%s did not become healthy within %s: %w", product, timeout, lastErr)
			}
			return fmt.Errorf("%s did not become healthy within %s", product, timeout)
		case <-ticker.C:
		}
	}
}

// Reconciler adapts service.ExecRefresher to the shared selfupdate.Reconciler
// seam. Reconciliation runs the NEW binary as a child process, because the
// definition a release ships can only be rendered by the binary that ships it
// (MADR 0100).
type Reconciler struct {
	// RefreshFn and RestoreFn default to service.ExecRefresher. Tests inject
	// fakes rather than spawning a child process.
	RefreshFn func(product, binary string) (service.UnitRefresh, error)
	RestoreFn func(product string, r service.UnitRefresh) error
}

var _ selfupdate.Reconciler = (*Reconciler)(nil)

// Reconcile rewrites the installed definition from the newly installed binary.
// Under MADR 0005 a reconcile failure is fatal and enters the shared rollback
// path; it is no longer a best-effort warning.
func (r *Reconciler) Reconcile(ctx context.Context, product, executable string) (selfupdate.ReconcileResult, error) {
	if err := ctx.Err(); err != nil {
		return selfupdate.ReconcileResult{}, err
	}
	fn := r.RefreshFn
	if fn == nil {
		fn = service.ExecRefresher{}.RefreshUnit
	}
	res, err := fn(product, executable)
	if err != nil {
		return selfupdate.ReconcileResult{}, err
	}
	return selfupdate.ReconcileResult{
		Changed: res.Changed,
		Detail:  res.Output,
		State:   res,
	}, nil
}

// Restore puts the previous definition back during rollback. The receipt must
// be one this adapter produced; anything else is a programming error and is
// reported rather than silently ignored, so a rollback never reports success
// it did not achieve.
func (r *Reconciler) Restore(ctx context.Context, product string, receipt selfupdate.ReconcileResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !receipt.Changed {
		return nil
	}
	state, ok := receipt.State.(service.UnitRefresh)
	if !ok {
		return fmt.Errorf("restore %s: unexpected reconcile receipt %T", product, receipt.State)
	}
	fn := r.RestoreFn
	if fn == nil {
		fn = service.ExecRefresher{}.RestoreUnit
	}
	return fn(product, state)
}
