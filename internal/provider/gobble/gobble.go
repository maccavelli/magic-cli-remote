package gobble

import (
	"log/slog"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// KnownGoodVersion is the gobble release whose wire this provider was built
// and checked against, and the supported minimum (MADR 0179 D5, A2). Another
// version warns at initialize and is never refused: a development build
// reports a prerelease, which a MinVersion would refuse.
const KnownGoodVersion = "0.4.0"

// closeTimeout bounds session/close: gobble's own wait for a cancelled turn's
// last entries (closeWait, 5 s), plus a second for what its close does after
// it (MADR 0179 D6, A3).
const closeTimeout = 6 * time.Second

// commandCaveat is said with the command list: gobble runs a skill as
// `/skill:<name>` prompt text, which it does not advertise (MADR 0179 D9).
const commandCaveat = "gobble's skills run as /skill:<name> and are not listed."

// clientCapabilities is acpagent's default set without client files and
// terminals: gobble then reads, writes and runs locally, confined to the
// session's directory (MADR 0179 D2). auth.terminal stays false, so gobble
// offers no terminal sign-in. session.compaction and session.notices stay:
// without them gobble sends its own compaction notification, which nothing
// here handles, and a paused automatic compaction would not reach the phone
// (A2).
func clientCapabilities() *acp.ClientCapabilities {
	c := acpagent.New(acpagent.Spec{}, acpagent.Config{}).ClientCapabilities()
	c.Fs = acp.FileSystemCapabilities{}
	c.Terminal = false
	c.Auth = acp.AuthCapabilities{}
	return &c
}

// safeAuthMethodIDs are acpext.AuthProviders' ids: each is both an upstream
// and gobble's agent auth method for it, a presence check that sends no
// secret (MADR 0179 D4).
func safeAuthMethodIDs() []string {
	ps := acpext.AuthProviders()
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	return ids
}

// spec is gobble's Spec for cfg: the auth hooks run cfg's binary.
func spec(cfg acpagent.Config, log *slog.Logger) acpagent.Spec {
	a := newAuth(cfg.Bin, log)
	return acpagent.Spec{
		ID:                 provider.IDGobble,
		DefaultBin:         "gobble",
		DefaultArgs:        func(acpagent.Config) []string { return []string{"acp"} },
		KnownGoodVersion:   KnownGoodVersion,
		CloseTimeout:       closeTimeout,
		ClientCapabilities: clientCapabilities(),

		AuthStatus:            a.status,
		AuthCatalogList:       a.catalog,
		SetCredential:         a.setCredential,
		ClearCredential:       a.clearCredential,
		StartDeviceAuth:       a.startDeviceAuth,
		SafeAuthMethodIDs:     safeAuthMethodIDs(),
		AuthPreferenceFunc:    a.readyUpstreams,
		HasHeadlessCredential: a.hasCredential,

		ListModels:       noHarvest,
		ModelsFromOption: modelsFromOption,

		DefaultModeID:      "default",
		SynthesizeAutoMode: false,
		DangerousModeIDs:   []string{"bypass"},

		CommandCaveat: commandCaveat,

		ErrorLimit: errorLimit,

		ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
			acpext.MethodRetry: handleRetry,
		},
		MetaHandlers: map[string]acpagent.MetaHandler{
			acpext.MetaKey: handleCompactionMeta,
		},
		Ops: acpagent.Ops{
			Rename: rename,
			Fork:   fork,
		},
	}
}

// New creates the gobble provider.
func New(cfg acpagent.Config, log *slog.Logger) *acpagent.Provider {
	if cfg.Bin == "" {
		cfg.Bin = "gobble"
	}
	if log == nil {
		log = slog.Default()
	}
	return acpagent.NewWithLogger(spec(cfg, log), cfg, log)
}
