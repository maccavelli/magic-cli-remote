package daemon

import (
	"context"
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/registered"
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

// CredentialAdapters returns the credential adapter of each provider that has
// one, by provider id: a descriptor's own (0208 MADR D6), and codex's.
// enabledOnly limits it to the providers cfg enables, as serve does. mcremote
// auth-recovery passes false, so a provider disabled since its credential was
// stored can still be recovered.
//
// codex's adapter is built with the configured binary. It is REQUIRED, not
// optional: the reality probe that distinguishes "no credential anywhere" from
// "a credential this coordinator cannot see" asks the CLI, and an adapter
// built without a binary answers RealityUnknown for both (MADR 0134).
func CredentialAdapters(cfg config.Config, enabledOnly bool) map[string]providerauth.Adapter {
	out := map[string]providerauth.Adapter{}
	for _, d := range registered.Descriptors() {
		if sec := d.SectionIn(cfg); d.Credentials != nil && (sec.IsEnabled() || !enabledOnly) {
			out[string(d.ID)] = d.Credentials(sec)
		}
	}
	if cfg.Providers.Codex.Enabled || !enabledOnly {
		out["codex"] = codex.NewCredentialAdapter("codex", cfg.Providers.Codex.Bin)
	}
	return out
}

// newCredentialGuard builds a coordinator for each adapter, by provider id.
// It performs no I/O beyond creating the private store directories.
func newCredentialGuard(
	dataDir string, adapters map[string]providerauth.Adapter, log *slog.Logger,
) (*credentialGuard, error) {
	g := &credentialGuard{coords: map[string]*providerauth.Coordinator{}, log: log}
	for id, ad := range adapters {
		c, err := providerauth.NewCoordinator(dataDir, ad, providerauth.CoordinatorOptions{})
		if err != nil {
			return nil, err
		}
		g.coords[id] = c
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
		case r.State == providerauth.StateExternal:
			// Not a warning and not a decision (MADR 0134). The provider works;
			// mcremote simply cannot back up a credential it cannot see, and
			// asking an operator to choose here would offer only destructive
			// answers. Signing in from the phone is what produces a credential
			// this daemon can protect.
			g.log.Info("provider is signed in but keeps its credential outside the file "+
				"mcremote can back up; signing in from here will create one it can",
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
