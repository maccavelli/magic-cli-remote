package providerauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher observes a provider's credential directory and checkpoints an
// autonomous refresh into the generation chain (MADR 0074 D24).
//
// It watches the parent directory, not the credential inode. Providers publish
// with a same-directory temporary plus rename, which replaces the inode: an
// inode watch goes deaf after the first rotation, which is exactly when it
// matters most.
//
// The watcher is an optimization, never the correctness story. Startup and
// pre-mutation reconciliation cover every event it misses, so a dropped or
// coalesced notification delays a checkpoint rather than losing one.
//
// That matters most on Linux, where fsnotify is inotify: fs.inotify's
// max_user_instances and max_user_watches are per-user limits a container or a
// busy desktop can genuinely exhaust, and some filesystems (NFS, some overlay
// setups) report no events at all. Start therefore returns an ordinary error
// that a caller should log and continue past. A host with no working watcher
// stays fully correct; it just checkpoints an autonomous refresh at the next
// startup or mutation instead of within a debounce window.
type Watcher struct {
	coord *Coordinator
	log   *slog.Logger

	mu      sync.Mutex
	started bool
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
	fsw     *fsnotify.Watcher
}

// NewWatcher builds an inert watcher. It starts no goroutine: a component that
// is constructed but never started must not be able to leak one.
func NewWatcher(c *Coordinator) *Watcher {
	return &Watcher{coord: c, log: slog.Default().With(slog.String("component", "providerauth.watch"))}
}

// WithLogger sets the watcher's logger.
func (w *Watcher) WithLogger(l *slog.Logger) *Watcher {
	if l != nil {
		w.log = l.With(slog.String("component", "providerauth.watch"))
	}
	return w
}

// Start begins watching. It is safe to call once; a second call is an error.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return fmt.Errorf("provider auth: watcher already started")
	}
	if w.closed {
		return fmt.Errorf("provider auth: watcher already closed")
	}

	live, err := w.coord.LivePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(live)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("provider auth: watch dir: %w", err)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("provider auth: watcher: %w", err)
	}
	if err := fsw.Add(dir); err != nil {
		_ = fsw.Close()
		return fmt.Errorf("provider auth: watch %s: %w", dir, err)
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.fsw = fsw
	w.cancel = cancel
	w.done = make(chan struct{})
	w.started = true

	go w.run(runCtx, filepath.Base(live))
	return nil
}

// Close stops the watcher within the fixed drain bound. It is idempotent, and
// safe on a watcher that was never started.
func (w *Watcher) Close(ctx context.Context) error {
	w.mu.Lock()
	if w.closed || !w.started {
		w.closed = true
		fsw := w.fsw
		w.fsw = nil
		w.mu.Unlock()
		if fsw != nil {
			return fsw.Close()
		}
		return nil
	}
	w.closed = true
	cancel, done, fsw := w.cancel, w.done, w.fsw
	w.fsw = nil
	w.mu.Unlock()

	cancel()
	if fsw != nil {
		_ = fsw.Close()
	}
	select {
	case <-done:
		return nil
	case <-time.After(DrainTimeout):
		// Report retained ownership rather than forcing; disk evidence stays.
		return fmt.Errorf("provider auth: watcher did not drain within %s", DrainTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run coalesces events and evaluates at most one checkpoint per debounce
// window.
func (w *Watcher) run(ctx context.Context, name string) {
	defer close(w.done)

	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(WatchDebounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(WatchDebounce)
		}
		timerC = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.events():
			if !ok {
				return
			}
			// Only this provider's credential matters; a sibling lock file or
			// temporary is noise.
			if filepath.Base(ev.Name) != name {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			arm()
		case err, ok := <-w.errors():
			if !ok {
				return
			}
			// Log the method-level fact only; a watch error never carries
			// credential content, but it can carry a path.
			w.log.Warn("credential watch error",
				slog.String("provider", w.coord.ProviderID()),
				slog.String("err", errClass(err)))
		case <-timerC:
			timerC = nil
			if err := w.coord.Reconcile(ctx); err != nil && !errors.Is(err, ErrTransactionBusy) {
				w.log.Warn("credential reconcile failed",
					slog.String("provider", w.coord.ProviderID()),
					slog.String("err", errClass(err)))
			}
		}
	}
}

func (w *Watcher) events() chan fsnotify.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fsw == nil {
		return nil
	}
	return w.fsw.Events
}

func (w *Watcher) errors() chan error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fsw == nil {
		return nil
	}
	return w.fsw.Errors
}

// errClass reduces an error to its type name so a log line cannot carry a
// path or payload (MADR 0074 D29).
func errClass(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}
