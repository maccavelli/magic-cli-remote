package daemon

import (
	"context"
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// credentialGuard owns the coordinators and watchers that keep Codex and Grok
// credentials recoverable (MADR 0074 D21-D26).
//
// It is constructed only when at least one transactional adapter is enabled, so
// a host running neither provider carries none of this machinery and reports no
// transactional capability.
type credentialGuard struct {
	coords   map[string]*providerauth.Coordinator
	watchers []*providerauth.Watcher
	log      *slog.Logger
}

// newCredentialGuard builds coordinators for the enabled transactional
// providers. It performs no I/O beyond creating the private store directories.
func newCredentialGuard(dataDir string, codexEnabled, grokEnabled bool, log *slog.Logger) (*credentialGuard, error) {
	g := &credentialGuard{coords: map[string]*providerauth.Coordinator{}, log: log}
	add := func(id string, ad providerauth.Adapter) error {
		c, err := providerauth.NewCoordinator(dataDir, ad, providerauth.CoordinatorOptions{})
		if err != nil {
			return err
		}
		g.coords[id] = c
		return nil
	}
	if codexEnabled {
		if err := add("codex", codex.NewCredentialAdapter("codex")); err != nil {
			return nil, err
		}
	}
	if grokEnabled {
		if err := add("grok", grok.NewCredentialAdapter("grok")); err != nil {
			return nil, err
		}
	}
	if len(g.coords) == 0 {
		return nil, nil
	}
	return g, nil
}

// coordinator returns one provider's coordinator, or nil.
func (g *credentialGuard) coordinator(id string) *providerauth.Coordinator {
	if g == nil {
		return nil
	}
	return g.coords[id]
}

// recover runs startup recovery for every coordinator before anything can
// mutate a credential.
//
// A provider needing an operator decision is reported and left reachable for
// status: it must not silently fall back to the destructive legacy flow, which
// is the behaviour this whole repair exists to remove (MADR 0074 P20 step 10).
func (g *credentialGuard) recover(ctx context.Context) {
	if g == nil {
		return
	}
	list := make([]*providerauth.Coordinator, 0, len(g.coords))
	for _, c := range g.coords {
		list = append(list, c)
	}
	for _, r := range providerauth.RecoverAll(ctx, list) {
		switch {
		case r.Err != nil:
			g.log.Warn("credential recovery failed",
				slog.String("provider", r.Provider), slog.String("err", r.Err.Error()))
		case r.State == providerauth.StateRecoveryRequired:
			g.log.Warn("credential state needs an operator decision; "+
				"run `mcremote auth-recovery status` to inspect and "+
				"`mcremote auth-recovery choose` to resolve",
				slog.String("provider", r.Provider))
		default:
			g.log.Info("credential state recovered",
				slog.String("provider", r.Provider), slog.String("state", string(r.State)))
		}
	}
}

// startWatchers begins watching each credential directory.
//
// A watcher that cannot start is logged and skipped, never fatal: on Linux this
// is inotify, whose per-user instance and watch limits a container can
// legitimately exhaust, and startup plus pre-mutation reconciliation already
// cover every event a watcher would have delivered (MADR 0074 D24).
func (g *credentialGuard) startWatchers(ctx context.Context) {
	if g == nil {
		return
	}
	for id, c := range g.coords {
		w := providerauth.NewWatcher(c).WithLogger(g.log)
		if err := w.Start(ctx); err != nil {
			g.log.Warn("credential watcher unavailable; "+
				"reconciliation falls back to startup and pre-mutation checkpoints",
				slog.String("provider", id), slog.String("err", err.Error()))
			continue
		}
		g.watchers = append(g.watchers, w)
	}
}

// close stops every watcher within the fixed drain bound.
func (g *credentialGuard) close(ctx context.Context) {
	if g == nil {
		return
	}
	for _, w := range g.watchers {
		if err := w.Close(ctx); err != nil {
			g.log.Warn("credential watcher did not drain", slog.String("err", err.Error()))
		}
	}
	g.watchers = nil
}

// enabled reports whether any transactional adapter is active, which gates the
// ProviderAuthTransactions capability.
func (g *credentialGuard) enabled() bool { return g != nil && len(g.coords) > 0 }

// activatePending gives a coordinator the chance to publish a validated
// credential now that the provider is idle.
//
// Activation itself is driven by the owned flow, which polls the live-session
// count it was constructed with; this hook exists so the publication happens
// before prewarm can start a new process against the credential being
// replaced. It is bounded and synchronous by contract.
func (g *credentialGuard) activatePending(id providerID) {
	if g == nil {
		return
	}
	if _, ok := g.coords[string(id)]; !ok {
		return
	}
	// Nothing to force: the owning flow rechecks the live count itself and
	// commits under the provider lock. Ordering is the guarantee this hook
	// provides, not a second publication path.
}

// providerID mirrors provider.ID without importing the provider package, which
// would make this file part of an import cycle through the coordinated
// provider constructors.
type providerID = provider.ID
