package providerauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Activation defaults. A validated candidate may wait for a busy provider to go
// idle, but never indefinitely.
const (
	defaultActivateWait      = 500 * time.Millisecond
	defaultActivationTimeout = ActivationGrace
)

// ErrActivationExpired means a validated candidate waited out its activation
// window without the provider going idle. The OAuth exchange succeeded; the
// publication did not happen, and LIVE is untouched.
var ErrActivationExpired = errors.New("provider auth: sign-in completed but could not be activated in time")

// OwnedFlowConfig describes one transactional device login.
type OwnedFlowConfig struct {
	// Coordinator owns the credential transaction this flow publishes into.
	Coordinator *Coordinator
	// Bin and Args are the provider CLI invocation.
	Bin  string
	Args []string
	// ScanTimeout bounds only the wait for the device code to appear.
	ScanTimeout time.Duration
	// EnvFor builds the child environment overlay for an isolated home. The
	// home always starts empty of credential material (MADR 0074 D22/F14).
	EnvFor func(home string) []string
	// Busy reports how many live provider sessions would be disrupted by
	// swapping the credential. Nil means never busy.
	Busy func() int
	// ActivateWait is the poll interval while waiting for an idle provider.
	ActivateWait time.Duration
	// ActivationTimeout bounds the whole deferred-activation window.
	ActivationTimeout time.Duration
	// Source records what produced the candidate; defaults to device auth.
	Source Source
	Log    *slog.Logger
}

// OwnedFlow is a device login that owns its child process, its credential
// transaction, its activation timer, and one cleanup path (MADR 0074 D27).
//
// Wait and Cancel share a single internal result, so they may be called in any
// order, concurrently, and repeatedly. Every incomplete outcome aborts the
// transaction, which leaves LIVE byte-identical.
type OwnedFlow struct {
	cfg  OwnedFlowConfig
	cli  *CLIFlow
	txn  *Txn
	flow DeviceFlowInfo
	log  *slog.Logger

	updates chan string

	// runOnce ensures the resolve path executes exactly once however many
	// callers race Wait and Cancel.
	runOnce  sync.Once
	done     chan struct{}
	resultMu sync.Mutex
	result   error

	cancelOnce sync.Once
	cancelled  chan struct{}
}

// DeviceFlowInfo is the display payload parsed from the child's output. It
// mirrors provider.DeviceFlow without importing that package.
type DeviceFlowInfo struct {
	VerificationURI string
	UserCode        string
	ExpiresIn       int
	Interval        int
}

// StartOwnedFlow opens a transaction, creates an empty isolated home, and
// spawns the provider CLI against it.
//
// The pending home is created empty and nothing is copied into it. That is the
// property that makes an isolated login safe: Codex revokes any stored token
// before deleting it, so a seeded home would invalidate the live grant while
// leaving LIVE byte-identical (MADR 0074 F14).
func StartOwnedFlow(ctx context.Context, cfg OwnedFlowConfig) (*OwnedFlow, error) {
	if cfg.Coordinator == nil {
		return nil, fmt.Errorf("provider auth: owned flow needs a coordinator")
	}
	if cfg.EnvFor == nil {
		return nil, fmt.Errorf("provider auth: owned flow needs a child environment")
	}
	if cfg.Source == "" {
		cfg.Source = SourceDeviceAuth
	}
	if cfg.ActivateWait <= 0 {
		cfg.ActivateWait = defaultActivateWait
	}
	if cfg.ActivationTimeout <= 0 {
		cfg.ActivationTimeout = defaultActivationTimeout
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}

	txn, err := cfg.Coordinator.Begin(ctx, cfg.Source)
	if err != nil {
		return nil, err
	}

	cls, cli, err := StartCLIDeviceFlow(ctx, cfg.Bin, cfg.Args, cfg.ScanTimeout, cfg.EnvFor(txn.Home()))
	if err != nil {
		// Nothing ran against LIVE, so abandoning the transaction is enough.
		_ = cfg.Coordinator.Abort(context.WithoutCancel(ctx), txn)
		return nil, err
	}

	return &OwnedFlow{
		cfg: cfg,
		cli: cli,
		txn: txn,
		flow: DeviceFlowInfo{
			VerificationURI: cls.VerificationURI,
			UserCode:        cls.UserCode,
		},
		log:       cfg.Log,
		updates:   make(chan string, 1),
		done:      make(chan struct{}),
		cancelled: make(chan struct{}),
	}, nil
}

// Flow is the immutable display payload.
func (f *OwnedFlow) Flow() DeviceFlowInfo { return f.flow }

// Updates publishes non-terminal state changes such as ready_to_activate. It is
// closed when the flow reaches its terminal outcome. A caller that never reads
// it does not block the flow.
func (f *OwnedFlow) Updates() <-chan string { return f.updates }

// Wait blocks until the flow reaches a terminal outcome. Every caller observes
// the same result.
func (f *OwnedFlow) Wait(ctx context.Context) error {
	go f.runOnce.Do(f.run)
	select {
	case <-f.done:
		return f.terminal()
	case <-ctx.Done():
		// The caller gave up; the flow keeps its own ownership and will still
		// resolve, so do not report a result it has not reached.
		return ctx.Err()
	}
}

// Cancel terminates the child, aborts the transaction, and resolves the flow.
// It is idempotent and safe before or after Wait.
func (f *OwnedFlow) Cancel() {
	f.cancelOnce.Do(func() {
		close(f.cancelled)
		f.cli.Kill()
	})
	go f.runOnce.Do(f.run)
}

func (f *OwnedFlow) terminal() error {
	f.resultMu.Lock()
	defer f.resultMu.Unlock()
	return f.result
}

func (f *OwnedFlow) resolve(err error) {
	f.resultMu.Lock()
	f.result = err
	f.resultMu.Unlock()
	close(f.updates)
	close(f.done)
}

// run drives the flow to exactly one terminal outcome. Every failure path
// aborts the transaction, which is what keeps LIVE byte-identical.
func (f *OwnedFlow) run() {
	// Detached from any caller's context: this owns the transaction, and a
	// caller that walked away must not leave the child or the journal behind.
	ctx := context.WithoutCancel(context.Background())

	if err := f.cli.Wait(ctx); err != nil {
		f.abort(ctx)
		f.resolve(err)
		return
	}
	if err := f.cfg.Coordinator.StageCandidate(ctx, f.txn); err != nil {
		f.abort(ctx)
		f.resolve(err)
		return
	}
	if err := f.cfg.Coordinator.ValidateCandidate(ctx, f.txn); err != nil {
		f.abort(ctx)
		f.resolve(err)
		return
	}
	if err := f.awaitIdle(ctx); err != nil {
		f.abort(ctx)
		f.resolve(err)
		return
	}
	if err := f.cfg.Coordinator.Commit(ctx, f.txn); err != nil {
		f.abort(ctx)
		f.resolve(err)
		return
	}
	f.resolve(nil)
}

// awaitIdle holds a validated candidate until the provider has no live work.
// This is not a failure state: the exchange succeeded and the candidate is
// ready, so the flow reports ready_to_activate and keeps waiting (D28).
func (f *OwnedFlow) awaitIdle(ctx context.Context) error {
	if f.cfg.Busy == nil || f.cfg.Busy() <= 0 {
		return nil
	}
	f.publish(provReadyToActivate)

	deadline := time.NewTimer(f.cfg.ActivationTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(f.cfg.ActivateWait)
	defer tick.Stop()

	for {
		select {
		case <-f.cancelled:
			return ErrFlowCancelled
		case <-deadline.C:
			return ErrActivationExpired
		case <-tick.C:
			if f.cfg.Busy() <= 0 {
				return ctx.Err()
			}
		}
	}
}

// provReadyToActivate mirrors provider.DeviceAuthReadyToActivate. The string is
// duplicated rather than imported because provider depends on this package.
const provReadyToActivate = "ready_to_activate"

// publish offers a non-terminal update without ever blocking the flow.
func (f *OwnedFlow) publish(state string) {
	select {
	case f.updates <- state:
	default:
	}
}

// abort releases the transaction. A failure here cannot be reported to the
// caller without masking the real outcome, so it is logged by provider and
// short transaction id only.
func (f *OwnedFlow) abort(ctx context.Context) {
	f.cli.Kill()
	if err := f.cfg.Coordinator.Abort(ctx, f.txn); err != nil {
		f.log.Warn("could not abort credential transaction",
			slog.String("provider", f.cfg.Coordinator.store.provider),
			slog.String("err", err.Error()))
	}
}
