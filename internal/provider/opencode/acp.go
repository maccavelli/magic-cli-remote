package opencode

import (
	"log/slog"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// The OpenCode provider is this Spec over acpagent: sessions run on
// `opencode acp` (0214 MADR D1), and what ACP lacks runs on a lease-bound
// control plane (control/). It replaced the 1.x HTTP dialect at 0214 PLAN P7.
//
// The phone's auth runs on the control plane (acp_auth.go). Over ACP, opencode
// advertises one method, opencode-login, which runs `opencode auth login` in a
// terminal. No method is named safe, so acpagent never calls authenticate, and
// the engine uses its own credential store.
//
// What the Spec does not need to say, because acpagent does it for every agent:
//   - the model, effort and mode config options (model, thought_level and mode
//     categories), set through session/set_config_option;
//   - automatic permission choices, by option kind (0214 MADR D9);
//   - one process per session, in its directory (amendment A, A5).

// childSessionUpdatesKey is the client capability under which opencode sends a
// child session's output as methodChildUpdate rather than projecting it onto
// the parent (packages/cli/src/acp/capabilities.ts at b44b7164ba).
const childSessionUpdatesKey = "opencode/child-session-updates"

// acpStartTimeout bounds initialize and session/new: a cold start measured
// 43.7 s and about 45 s (0214 MADR amendment A, A6).
const acpStartTimeout = 120 * time.Second

// acpStaticModels is the fallback catalog for when opencode cannot be started
// to list its own. It is the free models opencode's catalog listed at G1
// (testdata/wire/2.2.3); deepseek-v4.1-flash-free is the one G1, G2 and G5 ran
// turns on. Once a session reports its model option, that list replaces this.
var acpStaticModels = []picker.Option{
	{ID: "opencode/deepseek-v4.1-flash-free", Label: "DeepSeek V4.1 Flash Free", Group: "opencode"},
	{ID: "opencode/beam-free", Label: "Beam Free", Group: "opencode"},
	{ID: "opencode/flint-40b-free", Label: "Flint 40B Free", Group: "opencode"},
	{ID: "opencode/glm-5.3-flash-free", Label: "GLM-5.3-Flash Free", Group: "opencode"},
	{ID: "opencode/mimo-v2.6-flash-free", Label: "MiMo-V2.6-Flash Free", Group: "opencode"},
	{ID: "opencode/nemotron-3.5-lightning-free", Label: "Nemotron 3.5 Lightning Free", Group: "opencode"},
	{ID: "opencode/nemotron-3.5-super-vl-free", Label: "Nemotron 3.5 Super VL Free", Group: "opencode"},
}

// acpStaticModes are opencode's two primary agents, the values of its mode
// config option (G1). opencode sends no ACP modes, so without this list nothing
// would advertise the switch. auto is appended by acpagent and enforced by
// intercepting permission requests (0214 MADR D9).
var acpStaticModes = []event.SessionMode{
	{ID: "build", Name: "Build", Description: "Full tool access; edits allowed"},
	{ID: "plan", Name: "Plan", Description: "Research and plan only; no edits"},
}

// acpClientCapabilities is acpagent's own set, which covers 0214 MADR D1's list
// (fs, terminal, elicitation.form, session.compaction), plus the flag for
// child-session notifications.
func acpClientCapabilities() *acp.ClientCapabilities {
	c := acpagent.New(acpagent.Spec{}, acpagent.Config{}).ClientCapabilities()
	c.Meta = map[string]any{childSessionUpdatesKey: true}
	return &c
}

// acpSpec is OpenCode v2 over ACP.
var acpSpec = acpagent.Spec{
	ID:                 provider.IDOpencode,
	DefaultBin:         "opencode",
	DefaultArgs:        func(acpagent.Config) []string { return []string{"acp"} },
	KnownGoodVersion:   KnownGoodVersion,
	MinVersion:         MinVersion,
	UpgradeURL:         UpgradeURL,
	StartTimeout:       acpStartTimeout,
	ClientCapabilities: acpClientCapabilities(),
	StaticModels:       acpStaticModels,
	StaticModes:        acpStaticModes,
	DefaultModeID:      "build",
	SynthesizeAutoMode: true,
	// OpenCode snapshots, and so undo, need the project's real path (0214
	// MADR, added at P6e).
	RealCWD:  true,
	Commands: acpCommandTable(),
	ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
		methodChildUpdate: handleChildUpdate,
	},
	MetaHandlers: map[string]acpagent.MetaHandler{
		metaRetry: handleRetry,
	},
}

// NewACP creates the OpenCode provider over ACP.
func NewACP(cfg acpagent.Config) *acpagent.Provider {
	return NewACPWithLogger(cfg, nil)
}

// NewACPWithLogger is like NewACP but sets a logger.
func NewACPWithLogger(cfg acpagent.Config, log *slog.Logger) *acpagent.Provider {
	bin := cfg.Bin
	if bin == "" {
		bin = acpSpec.DefaultBin
	}
	plane := control.New(control.Config{Bin: bin, MinVersion: MinVersion, UpgradeURL: UpgradeURL, Log: log})
	return newACP(cfg, log, plane)
}

// newACP is the provider on any control plane; tests pass a fake engine. The
// auth hooks restart the provider's own sessions after a credential change
// (D8), the catalog hooks group the model option by vendor (D7,
// acp_catalog.go), and the workspace, diagnostics and diff are the control
// plane's, in the session's directory (D10, acp_surfaces.go).
func newACP(cfg acpagent.Config, log *slog.Logger, plane controlPlane) *acpagent.Provider {
	var p *acpagent.Provider
	auth := &acpAuth{
		plane:   plane,
		log:     log,
		restart: func(notice string) { p.RestartSessionsWhenIdle(notice) },
	}
	if auth.log == nil {
		auth.log = slog.Default()
	}
	spec := acpSpec
	spec.AuthStatus = auth.status
	spec.AuthCatalogList = auth.catalog
	spec.SetCredential = auth.setCredential
	spec.ClearCredential = auth.clearCredential
	spec.StartDeviceAuth = auth.startDeviceAuth
	spec.ListModels = acpNoHarvest
	spec.ModelsFromOption = acpModelsFromOption
	spec.ModelProviders = auth.modelProviders
	surfaces := &acpSurfaces{plane: plane}
	spec.Ops.ListWorkspace = surfaces.listWorkspace
	spec.Ops.ReadWorkspace = surfaces.readWorkspace
	spec.Ops.SearchWorkspace = surfaces.searchWorkspace
	spec.Ops.Diagnostics = surfaces.diagnostics
	spec.Ops.Diff = surfaces.diff
	spec.NewSessionState = newACPUndo
	spec.Ops.UndoLast = surfaces.undoLast
	spec.Ops.Revert = surfaces.revert
	spec.Ops.Unrevert = surfaces.unrevert
	spec.Ops.BeforePrompt = surfaces.beforePrompt
	spec.Ops.Shell = surfaces.shell
	// The control plane stops with the daemon, rather than only when its lease
	// does (0214 PLAN P7 Deviation 1).
	if c, ok := plane.(interface{ Close() }); ok {
		spec.OnShutdown = c.Close
	}
	p = acpagent.NewWithLogger(spec, cfg, log)
	return p
}
