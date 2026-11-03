// Package acpagent implements the shared ACP CLI agent provider machinery:
// subprocess launch, the ACP client-side connection (initialize, session
// new/load, prompt, cancel, close), streaming update → event mapping, remote
// permissions, filesystem callbacks, and the terminal host. Each agent supplies
// a Spec from its own package and reuses everything here; this package names
// none of them (MADR 0208 D2).
package acpagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/acp-go-sdk2/agentproc"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/launch"
	"github.com/maccavelli/magic-cli-remote/internal/provider/stderrtail"
	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// startTimeout bounds ACP initialize + session/new. The process outlives
// the request context, but hung Start must not pin the WS handler forever.
const startTimeout = 30 * time.Second

// agentGrace is agentproc's grace for an agent. Magic stops agents with Kill
// (MADR 0207 Q6), which has no grace steps, so it bounds only the wait in
// Process.Wait for the wire capture's tap to write what it holds.
const agentGrace = 2 * time.Second

// loadTimeout bounds session/load separately: the agent replays the entire
// prior conversation as notifications before responding, so long sessions
// legitimately need far more than a fresh session/new.
const loadTimeout = 120 * time.Second

// Spec is what varies between ACP CLI agents. Everything else — process
// lifecycle, the ACP handshake, event mapping, permissions, terminals — is
// shared.
type Spec struct {
	// ID is the provider identity.
	ID provider.ID
	// DefaultBin is the executable name used when Config.Bin is empty.
	DefaultBin string
	// DefaultArgs builds the CLI args that put the agent into ACP-stdio mode,
	// used when Config.Args is empty.
	DefaultArgs func(cfg Config) []string
	// ModelArgs, when non-nil, rebuilds the args for a per-session model
	// override (StartOptions.Model set and no config-level default model).
	// Nil means the binary takes no model flag; per-session models are then a
	// Spec concern elsewhere (e.g. an ACP session config option).
	ModelArgs func(cfg Config, model string) []string
	// ConfigureSession, when non-nil, runs after session/new (not load) inside
	// the Start timeout — e.g. to apply a model via an ACP session config
	// option. A failure aborts Start.
	ConfigureSession func(ctx context.Context, conn *acp.ClientSideConnection, resp acp.NewSessionResponse, opts provider.StartOptions, cfg Config, log *slog.Logger) error
	// SessionMeta, when non-nil, is copied onto session/new and session/load
	// `_meta`. Empty/nil omits the field (MADR 0106).
	SessionMeta func(opts provider.StartOptions, cfg Config) map[string]any
	// KnownGoodVersion is the agent release this Spec's wire shapes were
	// checked against. Empty disables the check entirely. A mismatch produces
	// one warning per engine start and NEVER refuses to run: a routine
	// upstream upgrade must not become an outage (MADR 0137 Phase 3).
	KnownGoodVersion string
	// MinVersion is the lowest agent release this Spec can drive. An engine
	// below it, or one that names no version at initialize, fails Start with
	// an error naming the version found and UpgradeURL. Empty starts any
	// engine (0214 MADR D3, amendment C).
	MinVersion string
	// UpgradeURL is where MinVersion's refusal sends the operator.
	UpgradeURL string
	// StartTimeout bounds initialize, and session/new, each; zero is
	// startTimeout. An agent whose cold start is slower sets it from measurement
	// (0214 MADR D1, amended).
	StartTimeout time.Duration
	// StaticModels is the fallback model picker catalog when ListModels is
	// nil or fails. Empty + AllowCustom on ListModels default still lets the
	// user type a free-text model id.
	StaticModels []picker.Option
	// AuthStatus, when non-nil, reports the agent's upstream credential state
	// (MADR 0074 D3). Nil means the agent contributes no auth block, and the
	// daemon renders it exactly as it did before the feature existed.
	AuthStatus func(ctx context.Context) (provider.AuthState, error)
	// AuthCatalogList, when non-nil, enumerates every upstream the agent
	// supports, configured or not (MADR 0074 D16). Nil means the phone can only
	// re-key what AuthStatus already reports.
	AuthCatalogList func(ctx context.Context) (provider.AuthCatalog, error)
	// SetCredential, when non-nil, writes an upstream credential to the agent's
	// native store (MADR 0074 D1). Nil means credentials are read-only here.
	SetCredential func(ctx context.Context, upstreamID, methodID, secret string, inputs map[string]string) error
	// ClearCredential removes one. modelID is the table target for stores
	// keyed by model (MADR 0085 D4); empty when CredentialModel is unset.
	ClearCredential func(ctx context.Context, upstreamID, modelID string) error
	// SafeAuthMethodIDs are ACP authenticate ids this daemon may invoke
	// without an operator pin (MADR 0085 D2). Empty → today's
	// AuthMethodID-only behaviour.
	SafeAuthMethodIDs []string
	// AuthPreference is the order in which advertised, safe methods are
	// tried when no method is pinned (MADR 0085 D2; MADR 0208 D2).
	AuthPreference []string
	// KeyAuthMethodID is the safe method used, advertised or not, when
	// HasHeadlessCredential reports a key.
	KeyAuthMethodID string
	// HasHeadlessCredential, when set, is D2 step 4: a usable API key
	// exists on disk or in env even if KeyAuthMethodID was not advertised.
	HasHeadlessCredential func() bool
	// CredentialModel resolves the single model id a store-keyed write should
	// target (MADR 0085 D4). Called by Provider.SetCredential and
	// ClearCredential. list is Provider.ListModels; cfgModel is Config.Model.
	CredentialModel func(ctx context.Context, list func(context.Context) (picker.Catalog, error), cfgModel string) (string, error)
	// SetActiveUpstream repoints the agent at another configured upstream
	// without re-authenticating (MADR 0074 D14). Nil means unsupported.
	SetActiveUpstream func(ctx context.Context, upstreamID string) error
	// StartDeviceAuth, when non-nil, runs an RFC 8628 flow for this agent
	// (MADR 0074 Strategy A). It receives the resolved binary so the spec does
	// not need to reach back into Config, and the phone's method id and inputs
	// for an agent that offers several methods or asks for answers (0214 PLAN
	// P6b Deviation 1).
	StartDeviceAuth func(ctx context.Context, bin, upstreamID, methodID string, inputs map[string]string, confirmDestructive bool) (provider.DeviceFlow, func(context.Context) error, error)
	// StaticModes is the fallback session-mode list for an agent that honors
	// session/set_mode but advertises no modes at session/new|load. Used only
	// when the agent reported none; an agent-supplied list always wins. Because
	// such an agent has no declared vocabulary to check against, SetMode
	// validates against this list instead (see session.SetMode).
	StaticModes []event.SessionMode
	// DefaultModeID is the mode treated as current at session start when
	// StaticModes is used. Empty falls back to the first entry.
	DefaultModeID string
	// SynthesizeAutoMode appends a daemon-enforced `auto` mode to the
	// advertised list, for agents whose own auto-approve is not settable per
	// session: an auto that is a flag on the agent's process could follow a
	// per-session switch only by restarting the engine under every other
	// session (MADR 0049 D1).
	//
	// The id is never forwarded to the agent — SetMode intercepts it — and an
	// agent that advertises its own `auto` is never shadowed. Off by default
	// so an agent with a native auto does not get a second one.
	SynthesizeAutoMode bool
	// ListModels, when non-nil, supplies a live (or merged) catalog. Called
	// from [Provider.ListModels] with the provider config.
	ListModels func(ctx context.Context, cfg Config) (picker.Catalog, error)
	// Commands declares how this agent satisfies the canonical slash-command
	// vocabulary (MADR 0023). An undeclared command falls back to the spec
	// default, which is safe but usually not the truth for a specific CLI —
	// notably because an ACP agent may advertise a command its shell only
	// renders in its own terminal UI.
	Commands command.Table
	// CommandCaveat is an optional session-wide note appended to /help, for a
	// quirk that is not per-command, such as part of an agent's advertised
	// catalog being usable only in its own terminal.
	CommandCaveat string
	// ExtensionNotifications registers handlers for extension notifications.
	ExtensionNotifications map[string]ExtensionNotificationHandler
	// MetaHandlers registers handlers for keys of the `_meta` object in a
	// session_info_update and in the prompt result, such as an agent's retry
	// marker (MADR 0214 D14, as corrected). A session_info_update
	// `_meta` key with no handler is declined, as every key was before.
	MetaHandlers map[string]MetaHandler
	// ClientCapabilities, when non-nil, is what initialize advertises to this
	// agent in place of defaultClientCapabilities: an agent can be given less
	// than Magic implements, such as no fs or terminal for an agent that runs
	// its own tools (MADR 0184, amendment, owner answer Q3). It must never
	// advertise more; the capability-honesty test checks every Spec.
	ClientCapabilities *acp.ClientCapabilities
	// CredentialEnv names environment variables whose values are removed from
	// the agent's stderr tail, beyond those stderrtail.Secrets already knows by
	// the endings of their names (MADR 0216 D27).
	CredentialEnv []string

	// The three hooks below are vendor fallbacks for model and thinking. The
	// standard path is a session config option of category `model` or
	// `thought_level`, set with session/set_config_option and read back from
	// its answer; a hook runs only for an agent that offers no such option
	// (MADR 0184 D6).

	// SetModelFallback switches the model and returns the model the agent
	// applied.
	SetModelFallback func(ctx context.Context, h Host, model string) (applied string, err error)
	// SetThinkingFallback sets the thinking level for modelID and returns the
	// level the agent reports applied.
	SetThinkingFallback func(ctx context.Context, h Host, modelID, level string) (applied string, err error)
	// HarvestSessionMeta reads the model and thinking level from session/new
	// or session/load `_meta`; either may be "".
	HarvestSessionMeta func(meta map[string]any) (model, effort string)

	// ExtensionRequests registers handlers for extension requests the agent
	// sends, by method name with its leading `_`.
	ExtensionRequests map[string]ExtensionRequestHandler
	// NewSessionState, when non-nil, makes the Spec's own state for each
	// session, which its hooks read with [Host.SpecState].
	NewSessionState func() any
	// InitializeMeta, when non-nil, reads initialize's `_meta`.
	InitializeMeta func(meta map[string]any) InitializeInfo
	// Ops are the session operations the Spec offers beyond standard ACP.
	Ops Ops
}

// Provider is an ACP CLI agent adapter parameterized by a Spec.
type Provider struct {
	spec Spec
	cfg  Config
	log  *slog.Logger

	catalogMu    sync.RWMutex
	catalogCache picker.Catalog
	catalogHas   bool

	// versionMu guards engineVersion, which is written from a spawn and read
	// from doctor/status paths on other goroutines.
	versionMu     sync.Mutex
	engineVersion string

	// warm is the single spare pre-initialized agent process (cfg.Prewarm).
	// Claimed by Start when the requested argv matches the default; refilled
	// in the background after each claim.
	warmMu  sync.Mutex
	warm    *session
	warming bool
	closed  bool
	// warmGen counts restarts (RestartSessionsWhenIdle): a spare that began
	// warming before one is discarded rather than kept.
	warmGen int

	// catalogs single-flights and TTLs the cold-start catalog harvest, so N
	// picker opens against a cold cache mean one agent spawn.
	catalogs *picker.Cache[string]

	// live is every session this provider opened and has not closed, for
	// RestartSessionsWhenIdle.
	liveMu sync.Mutex
	live   map[*session]struct{}
}

// New creates a provider with defaults for empty fields.
func New(spec Spec, cfg Config) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = spec.DefaultBin
	}
	if len(cfg.Args) == 0 && spec.DefaultArgs != nil {
		cfg.Args = spec.DefaultArgs(cfg)
	}
	return &Provider{
		spec:     spec,
		cfg:      cfg,
		log:      slog.Default().With(slog.String("component", "provider."+string(spec.ID))),
		catalogs: picker.NewCache[string](0),
	}
}

// NewWithLogger is like New but sets a logger.
func NewWithLogger(spec Spec, cfg Config, log *slog.Logger) *Provider {
	p := New(spec, cfg)
	if log != nil {
		p.log = log.With(slog.String("component", "provider."+string(spec.ID)))
	}
	return p
}

// ID implements [provider.Provider].
func (p *Provider) ID() provider.ID { return p.spec.ID }

// AuthStatus implements [provider.Auth] by delegating to the spec
// (MADR 0074 D3). A spec without the hook reports ErrAuthUnsupported, and the
// daemon then omits the auth block for that provider.
func (p *Provider) AuthStatus(ctx context.Context) (provider.AuthState, error) {
	if p.spec.AuthStatus == nil {
		return provider.AuthState{}, provider.ErrAuthUnsupported
	}
	return p.spec.AuthStatus(ctx)
}

// AuthCatalogList implements [provider.AuthCataloger] by delegating to the
// spec (MADR 0074 D16).
func (p *Provider) AuthCatalogList(ctx context.Context) (provider.AuthCatalog, error) {
	if p.spec.AuthCatalogList == nil {
		return provider.AuthCatalog{}, provider.ErrAuthUnsupported
	}
	return p.spec.AuthCatalogList(ctx)
}

// SetCredential implements [provider.AuthWriter] by delegating to the spec
// (MADR 0074 D1). When CredentialModel is set, inputs["model"] is filled
// with the resolved default (MADR 0085 D4).
func (p *Provider) SetCredential(ctx context.Context, upstreamID, methodID, secret string, inputs map[string]string) error {
	if p.spec.SetCredential == nil {
		return provider.ErrAuthUnsupported
	}
	inputs = cloneAuthInputs(inputs)
	if err := p.fillCredentialModel(ctx, inputs); err != nil {
		return err
	}
	return p.spec.SetCredential(ctx, upstreamID, methodID, secret, inputs)
}

// ClearCredential implements [provider.AuthWriter].
func (p *Provider) ClearCredential(ctx context.Context, upstreamID string) error {
	if p.spec.ClearCredential == nil {
		return provider.ErrAuthUnsupported
	}
	modelID, err := p.resolveCredentialModel(ctx)
	if err != nil {
		return err
	}
	return p.spec.ClearCredential(ctx, upstreamID, modelID)
}

func (p *Provider) fillCredentialModel(ctx context.Context, inputs map[string]string) error {
	id, err := p.resolveCredentialModel(ctx)
	if err != nil {
		return err
	}
	if id != "" {
		inputs["model"] = id
	}
	return nil
}

func (p *Provider) resolveCredentialModel(ctx context.Context) (string, error) {
	if p.spec.CredentialModel == nil {
		return "", nil
	}
	return p.spec.CredentialModel(ctx, p.ListModels, p.cfg.Model)
}

func cloneAuthInputs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	maps.Copy(out, in)
	return out
}

// SetActiveUpstream implements [provider.UpstreamSwitcher] (MADR 0074 D14).
func (p *Provider) SetActiveUpstream(ctx context.Context, upstreamID string) error {
	if p.spec.SetActiveUpstream == nil {
		return provider.ErrAuthUnsupported
	}
	return p.spec.SetActiveUpstream(ctx, upstreamID)
}

// SupportsDeviceAuth implements [provider.DeviceAuthCapable] (MADR 0083 D4).
func (p *Provider) SupportsDeviceAuth() bool {
	return p.spec.StartDeviceAuth != nil
}

// StartDeviceAuth implements [provider.DeviceAuth] (MADR 0074 Strategy A).
func (p *Provider) StartDeviceAuth(ctx context.Context, upstreamID, methodID string, inputs map[string]string, confirmDestructive bool) (provider.DeviceFlow, func(context.Context) error, error) {
	if p.spec.StartDeviceAuth == nil {
		return provider.DeviceFlow{}, nil, provider.ErrAuthUnsupported
	}
	return p.spec.StartDeviceAuth(ctx, p.cfg.Bin, upstreamID, methodID, cloneAuthInputs(inputs), confirmDestructive)
}

// CommandTable implements [command.Tabler].
func (p *Provider) CommandTable() command.Table { return p.spec.Commands }

// CommandCaveat implements [command.Caveater].
func (p *Provider) CommandCaveat() string { return p.spec.CommandCaveat }

// catalogProbeTimeout bounds the cold-start agent spawn used to harvest the
// model catalog. A full agent start was measured at ~3.8 s (MADR 0137 F5);
// initialize alone, which is all this needs, is well inside that.
const catalogProbeTimeout = 30 * time.Second

// ListModels implements [provider.ModelCatalog].
//
// The live catalog arrives in the agent's ACP initialize `_meta` and is cached
// provider-wide, so it exists only once an agent process has run. Before MADR
// 0043 D7 that meant the create-session picker showed a hardcoded static list
// until the user had already started a session — and that list disagreed with
// the agent (it offered models the agent no longer accepted and omitted the
// one it defaults to). Harvest it instead: spawning one process to read `initialize`
// is the only way to have a truthful catalog before the first session.
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	static := p.staticModelCatalog()

	p.catalogMu.RLock()
	hasCache := p.catalogHas
	cached := p.catalogCache
	p.catalogMu.RUnlock()

	var live picker.Catalog
	var err error
	switch {
	case hasCache:
		live = cached
	case p.spec.ListModels != nil:
		live, err = p.spec.ListModels(ctx, p.cfg)
		if err != nil {
			p.log.Debug("list models live failed; using static", slog.String("err", err.Error()))
			return static, nil
		}
	default:
		harvested, ok := p.harvestCatalog(ctx)
		if !ok {
			return static, nil
		}
		live = harvested
	}

	if len(live.Options) == 0 {
		return static, nil
	}
	// A live catalog is authoritative and must not be padded with the static
	// floor. Static is the fallback for having no agent, not a supplement to
	// having one. AllowCustom keeps an older install's ids typeable
	// (MADR 0043 D7 / 0081).
	return withCatalogDefault(live.Normalize(), p.cfg.Model), nil
}

// withCatalogDefault makes a configured model authoritative for the picker's
// pre-selection without discarding the live option list.
func withCatalogDefault(c picker.Catalog, model string) picker.Catalog {
	if model != "" {
		c.DefaultIDs = []string{model}
	}
	return c
}

// harvestCatalog spawns (or claims) one agent process purely to complete
// `initialize`, which is what populates the provider catalog cache, then
// releases it.
//
// Single-flighted and TTL'd through the shared catalog cache, so N picker opens
// against a cold cache mean one spawn. A warm spare is claimed rather than
// spawned when one is available, since it has already completed initialize.
func (p *Provider) harvestCatalog(ctx context.Context) (picker.Catalog, bool) {
	if !p.Ready() {
		return picker.Catalog{}, false
	}
	_, err := p.catalogs.Get(ctx, "models", func(ctx context.Context) (picker.Catalog, error) {
		ctx, cancel := context.WithTimeout(ctx, catalogProbeTimeout)
		defer cancel()
		procDir, err := p.resolveSessionCWD("")
		if err != nil {
			return picker.Catalog{}, err
		}
		// Spawn rather than claim the warm spare: a spare has already completed
		// initialize, so if one existed the cache would be populated and this
		// path would not have run. Claiming it here could only destroy a ready
		// process to learn nothing.
		p.log.Info("spawning a short-lived agent to read the model catalog",
			slog.String("bin", p.cfg.Bin))
		s, err := p.spawnAgent(ctx, p.cfg.Args, procDir)
		if err != nil {
			return picker.Catalog{}, err
		}
		s.markClosedAndKill()
		p.catalogMu.RLock()
		ok := p.catalogHas
		p.catalogMu.RUnlock()
		if !ok {
			return picker.Catalog{}, fmt.Errorf("agent reported no model state at initialize")
		}
		return picker.Catalog{}, nil
	})
	if err != nil {
		p.log.Debug("model catalog harvest failed; static catalog",
			slog.String("err", err.Error()))
		return picker.Catalog{}, false
	}
	p.catalogMu.RLock()
	defer p.catalogMu.RUnlock()
	return p.catalogCache, p.catalogHas
}

func (p *Provider) staticModelCatalog() picker.Catalog {
	def := p.cfg.Model
	return picker.SingleCatalog(picker.SourceStatic, slices.Clone(p.spec.StaticModels), def, true)
}

// Ready implements [provider.Provider].
func (p *Provider) Ready() bool {
	_, err := launch.Resolve(p.cfg.Bin)
	return err == nil
}

// spawnAgent launches the binary and completes the ACP initialize handshake.
// The returned session has no agent-side session yet (localID/cwd/agentID are
// bound later) and its exit watcher is already running, so a process that
// dies while idle (e.g. a pre-warmed spare) is observed.
//
// procDir becomes the agent process OS cwd (cmd.Dir). Callers must pass an
// absolute directory that will match the eventual ACP session cwd when the
// process is reused for a real session — MCP stdio children inherit it.
func (p *Provider) spawnAgent(ctx context.Context, args []string, procDir string) (*session, error) {
	// launch.Command, not procutil.Command directly: on Windows the configured
	// bin may resolve to an npm batch shim, whose argv has to be quoted into a
	// command line this project builds rather than one os/exec escapes by the
	// wrong rules (MADR 0159 D23/D24).
	resolved, err := launch.Resolve(p.cfg.Bin)
	if err != nil {
		return nil, err
	}
	cmd, err := launch.Command(context.Background(), resolved, args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = procDir
	log := p.log

	s := p.newAgentSession(procDir)
	// Bound stderr noise and scrape provider 429/quota lines mid-turn so a
	// silent agent retry does not leave the phone stuck on "running". In front
	// of that, keep the end of stderr for a failure to start and for the
	// agent's death; it forwards every byte, and the SDK's own tail stays unset
	// (MADR 0216 D22, D23).
	tail := stderrtail.New(&slogWriter{
		log:    log,
		level:  slog.LevelDebug,
		prefix: string(p.spec.ID) + "-stderr",
		onLine: s.noteEngineLogLine,
	})
	cmd.Stderr = tail
	secrets := stderrtail.Secrets(envOf(cmd), p.spec.CredentialEnv, p.mcpHeaderValues())
	// stderrTail is the tail without the secrets Magic holds (D4, D27). It is
	// final once the agent is reaped: Wait, and Kill, which returns Wait's
	// error, return only after os/exec's copy into tail has.
	stderrTail := func() string { return stderrtail.Scrub(tail.Tail(), secrets) }
	// agentproc starts the agent in a process group of its own on Unix, and in
	// a kill-on-close job on Windows, started suspended until it is in the job,
	// so its descendants end with it (MADR 0150 D2; 0207 D10). On Linux it also
	// sets a Pdeathsig, from a thread held for the agent's life. It ties the
	// process to the connection, and it reaps.
	//
	// wirecap.For returns nil unless MCREMOTE_WIRE_CAPTURE_DIR is set, and a nil
	// capture's Tap is a nil writer: no tap (MADR 0137 Phase 1). The logger is a
	// connection option, applied before the SDK's goroutines start reading
	// (acp-go-sdk2 WithLogger; MADR 0184 D2). The SDK reads before Start
	// returns, so s is complete before Start is called.
	s.wire = wirecap.For(string(p.spec.ID))
	proc, err := (&agentproc.Command{
		Cmd:               cmd,
		Grace:             agentGrace,
		ConnectionOptions: clientConnOptions(log, s),
		OutputTap:         s.wire.Tap(),
	}).Start(s)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}
	s.proc = proc
	conn := proc.Conn()
	s.conn = conn
	// killTree ends the agent and reaps it, before the exit watcher exists, and
	// closes the capture the watcher would have closed.
	killTree := func() {
		_ = proc.Kill()
		dropped, _ := proc.DroppedTapBytes()
		closeCapture(s.log, s.wire, dropped)
	}
	// startFailed is the error for a failure after killTree, with what the
	// agent last wrote to stderr: an agent that cannot start usually says why
	// there (MADR 0216 D3). It is logged as the other adapters log theirs
	// (MADR 0069 D7).
	startFailed := func(err error) error {
		t := stderrTail()
		provider.LogStderrTail(p.log, p.cfg.Bin, t)
		return stderrtail.Wrap(err, t)
	}

	if err := p.handshake(ctx, s, conn, func(err error) error {
		killTree()
		return startFailed(err)
	}); err != nil {
		return nil, err
	}

	// Watch process exit from here on. The watcher's Wait reaps; later failure
	// paths kill through markClosedAndKill, which does not wait. Its message
	// carries Wait's error unchanged: the exit status of an agent that stopped by
	// itself, or ErrKilled / ErrTerminated when it was stopped (MADR 0207 D11).
	//
	// Once Wait has returned, the tap has written what it held, so the wire
	// capture is closed here; nothing else closed it, which leaked one file per
	// session with capture on, and kept Windows from deleting it (MADR 0207 PLAN,
	// S6 deviation 1). watched closes last, recovered panic or not.
	go func() {
		defer close(s.watched)
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("process exit watcher panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				s.signalDisconnected("process exit watcher panic")
			}
		}()
		err := proc.Wait()
		dropped, _ := proc.DroppedTapBytes()
		closeCapture(s.log, s.wire, dropped)
		msg := fmt.Sprintf("%s process exited", p.spec.ID)
		if err != nil {
			msg = fmt.Sprintf("%s process exited: %v", p.spec.ID, err)
		}
		// The death carries what the agent last wrote, whatever its exit status
		// (MADR 0216 D2). An agent Magic stopped did not die: its session is
		// closed and reports nothing, so its tail is not logged either.
		if !errors.Is(err, agentproc.ErrKilled) && !errors.Is(err, agentproc.ErrTerminated) {
			if t := stderrTail(); t != "" {
				provider.LogStderrTail(p.log, p.cfg.Bin, t)
				msg = stderrtail.Message(msg, t)
			}
		}
		s.signalDisconnected(msg)
	}()

	// Watch connection death independently of process exit: the connection can
	// end while the agent process lives, and the session would then zombie. See
	// session.watchConnClose.
	go s.watchConnClose(conn)

	return s, nil
}

// newAgentSession is a session for an agent whose process runs in procDir,
// bound to the Spec, with no connection yet.
func (p *Provider) newAgentSession(procDir string) *session {
	s := &session{
		providerID:     p.spec.ID,
		provider:       p,
		procDir:        procDir,
		terms:          newTerminalHost(),
		log:            p.log,
		events:         make(chan event.Event, 256),
		done:           make(chan struct{}),
		watched:        make(chan struct{}),
		cfg:            p.cfg,
		pending:        make(map[string]*permWaiter),
		questions:      make(map[string]*questionWaiter),
		staticModes:    p.spec.StaticModes,
		defaultModeID:  p.spec.DefaultModeID,
		synthesizeAuto: p.spec.SynthesizeAutoMode,
	}
	p.bindSpec(s)
	return s
}

// handshake runs initialize and authentication on conn for s. A failure is
// returned through fail, which ends the agent and adds what it said.
func (p *Provider) handshake(ctx context.Context, s *session, conn *acp.ClientSideConnection, fail func(error) error) error {
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	initCtx, initCancel := context.WithTimeout(parent, p.startTimeout())
	defer initCancel()

	// The typed call's response keeps the top-level _meta block in
	// initResp.Meta, which is where an agent's vendor fields live (MADR 0167 F15).
	initResp, err := conn.Initialize(initCtx, p.initializeRequest())
	if err != nil {
		return fail(initializeError(err))
	}

	p.applyInitializeResponse(s, initResp)
	if err := p.checkMinVersion(p.EngineVersion()); err != nil {
		return fail(err)
	}
	advertised := s.advertisedAuth

	if len(p.spec.SafeAuthMethodIDs) > 0 {
		hasKey := false
		if p.spec.HasHeadlessCredential != nil {
			hasKey = p.spec.HasHeadlessCredential()
		}
		id, err := selectACPAuthMethod(advertised, p.cfg.AuthMethodID, p.spec.SafeAuthMethodIDs,
			p.spec.AuthPreference, p.spec.KeyAuthMethodID, hasKey)
		if err != nil {
			return fail(err)
		}
		if id != "" {
			if _, err := conn.Authenticate(initCtx, acp.AuthenticateRequest{MethodId: acp.AuthMethodId(id)}); err != nil {
				return fail(fmt.Errorf("acp authenticate (%s): %w", id, err))
			}
			s.log.Info("acp authenticated", slog.String("method_id", id))
		} else {
			s.authRequired = true
		}
	} else if len(advertised) > 0 && p.cfg.AuthMethodID != "" {
		if _, err := conn.Authenticate(initCtx, acp.AuthenticateRequest{
			MethodId: acp.AuthMethodId(p.cfg.AuthMethodID),
		}); err != nil {
			return fail(fmt.Errorf("acp authenticate (%s): %w", p.cfg.AuthMethodID, err))
		}
		s.log.Info("acp authenticated", slog.String("method_id", p.cfg.AuthMethodID))
	} else if len(advertised) > 0 {
		// Debug, not warn. This fires once per session start on any agent that
		// advertises auth methods while auth_method_id is unset — 90 times in
		// the operator's log, and true zero of those times: the agent
		// authenticated from its own credential store and session/new never
		// failed for this reason. A warning the operator is trained to ignore is how the
		// next real warning gets missed (MADR 0138 F12). If session/new does
		// fail, it fails loudly on its own.
		s.log.Debug("agent advertises auth methods but none is configured",
			slog.Int("count", len(advertised)))
	}
	return nil
}

// closeCapture closes the session's wire capture once the agent's process has
// been reaped, and warns when the capture lost part of the agent's output:
// dropped is agentproc's DroppedTapBytes for the output, final once Wait has
// returned (MADR 0207 amendment H, D26).
func closeCapture(log *slog.Logger, wire *wirecap.Capture, dropped uint64) {
	wire.Close()
	if dropped > 0 {
		log.Warn("wire capture is incomplete: the tap lost part of the agent's output",
			slog.Uint64("dropped_bytes", dropped))
	}
}

// envOf is the environment cmd starts its process with: its Env, or the
// daemon's own when Env is nil, as os/exec does. Neither launch nor procutil
// sets Env, so for an ACP agent it is the daemon's (MADR 0216 D27).
func envOf(cmd *exec.Cmd) []string {
	if cmd.Env != nil {
		return cmd.Env
	}
	return os.Environ()
}

// mcpHeaderValues are the configured MCP servers' header values, which
// session/new and session/load hand the agent (MADR 0216 D4).
func (p *Provider) mcpHeaderValues() []string {
	var out []string
	for _, m := range p.cfg.McpServers {
		for _, v := range m.Headers {
			out = append(out, v)
		}
	}
	return out
}

// initializeError reports a failed initialize. An agent that answers another
// ACP protocol version is named as such, with both versions and what to do;
// the *acp.ProtocolVersionError stays in the chain (D26).
func initializeError(err error) error {
	var pv *acp.ProtocolVersionError
	if errors.As(err, &pv) && pv.Answered != 0 {
		return fmt.Errorf("acp initialize: the agent answered ACP protocol version %d, and Magic speaks %d; "+
			"update the agent or mcremote so they speak one version: %w", pv.Answered, pv.Requested, err)
	}
	return fmt.Errorf("acp initialize: %w", err)
}

// buildMcpServers converts the configured MCP servers into ACP form, dropping
// any whose transport the agent did not advertise (mcpCapabilities). Returns an
// empty (non-nil) slice when none apply — the agent expects a present array.
func (p *Provider) buildMcpServers(caps acp.AgentCapabilities, log *slog.Logger) []acp.McpServer {
	out := make([]acp.McpServer, 0, len(p.cfg.McpServers))
	for _, m := range p.cfg.McpServers {
		headers := make([]acp.HttpHeader, 0, len(m.Headers))
		for k, v := range m.Headers {
			headers = append(headers, acp.HttpHeader{Name: k, Value: v})
		}
		switch m.Transport {
		case McpHTTP:
			if !caps.McpCapabilities.Http {
				log.Warn("dropping http MCP server: agent lacks mcpCapabilities.http", slog.String("name", m.Name))
				continue
			}
			out = append(out, acp.NewMcpServerHttp(m.Name, m.URL, headers))
		case McpSSE:
			if !caps.McpCapabilities.Sse {
				log.Warn("dropping sse MCP server: agent lacks mcpCapabilities.sse", slog.String("name", m.Name))
				continue
			}
			out = append(out, acp.NewMcpServerSse(m.Name, m.URL, headers))
		default:
			log.Warn("dropping MCP server with unknown transport",
				slog.String("name", m.Name), slog.String("transport", string(m.Transport)))
		}
	}
	return out
}

// resolveSessionCWD picks the absolute working directory for a new session
// (shared rules + errno-preserving validation: provider.ResolveSessionCWD,
// 0069 P1).
func (p *Provider) resolveSessionCWD(optsCWD string) (string, error) {
	return provider.ResolveSessionCWD(optsCWD, p.cfg.DefaultCWD, nil)
}

// EnsureWarm arms (or re-arms) the spare pre-initialized agent process in the
// background. Call at daemon startup and rely on Start to re-arm after each
// claim. No-op unless cfg.Prewarm.
//
// The spare is started with the same directory that Start uses when the phone
// leaves cwd empty (DefaultCWD or $HOME). Only sessions whose resolved cwd
// matches that process dir can claim it — the agent process OS cwd is sticky,
// and stdio MCP servers inherit it.
func (p *Provider) EnsureWarm() {
	if !p.cfg.Prewarm || !p.Ready() {
		return
	}
	p.warmMu.Lock()
	if p.closed || p.warm != nil || p.warming {
		p.warmMu.Unlock()
		return
	}
	p.warming = true
	gen := p.warmGen
	p.warmMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("prewarm panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		defer func() {
			p.warmMu.Lock()
			p.warming = false
			p.warmMu.Unlock()
		}()
		procDir, err := p.resolveSessionCWD("")
		if err != nil {
			p.log.Warn("prewarm failed", slog.String("err", err.Error()))
			return
		}
		s, err := p.spawnAgent(context.Background(), p.cfg.Args, procDir)
		if err != nil {
			p.log.Warn("prewarm failed", slog.String("err", err.Error()))
			return
		}
		if !shouldKeepWarm(s) {
			s.markClosedAndKill()
			p.log.Info("prewarm skipped: no headless-safe auth method")
			return
		}
		p.warmMu.Lock()
		if p.closed || p.warm != nil || p.warmGen != gen {
			p.warmMu.Unlock()
			s.markClosedAndKill()
			return
		}
		p.warm = s
		p.warmMu.Unlock()
		p.log.Info("agent prewarmed",
			slog.String("bin", p.cfg.Bin),
			slog.String("proc_dir", procDir),
		)
	}()
}

// claimWarm pops the spare process when it is alive and its process OS cwd
// matches wantDir (absolute). A cwd mismatch leaves the spare in place for a
// later session that can use it — typically empty-cwd / home sessions.
func (p *Provider) claimWarm(wantDir string) *session {
	p.warmMu.Lock()
	s := p.warm
	if s == nil {
		p.warmMu.Unlock()
		return nil
	}
	if s.procDir != wantDir {
		p.warmMu.Unlock()
		return nil
	}
	p.warm = nil
	p.warmMu.Unlock()
	s.mu.Lock()
	dead := s.closed
	s.mu.Unlock()
	if s.proc != nil {
		select {
		case <-s.proc.Done():
			dead = true
		default:
		}
	}
	if dead {
		s.markClosedAndKill()
		return nil
	}
	return s
}

// Shutdown releases the warm spare (daemon exit). Live sessions are closed by
// the session manager, not here.
func (p *Provider) Shutdown() {
	p.warmMu.Lock()
	p.closed = true
	s := p.warm
	p.warm = nil
	p.warmMu.Unlock()
	if s != nil {
		s.markClosedAndKill()
	}
}

// spawnArgs returns the process argv baked at New. Per-session model and
// thinking are ACP `_meta` on session/new|load (MADR 0106), not argv rebuilds.
func (p *Provider) spawnArgs(_ provider.StartOptions) []string {
	return append([]string{}, p.cfg.Args...)
}

// Start implements [provider.Provider]: spawns (or claims a prewarmed) ACP agent
// and completes session/new or session/load.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("%s binary %q not found in PATH: %w", p.spec.ID, p.cfg.Bin, provider.ErrNotImplemented)
	}

	cwd, err := p.resolveSessionCWD(opts.CWD)
	if err != nil {
		return nil, err
	}

	localID := opts.LocalSessionID
	if localID == "" {
		localID = uuid.NewString()
	}

	args := p.spawnArgs(opts)

	// Claim the pre-warmed process only when argv and process OS cwd both
	// match. Engine cold start is several seconds for some agents; we still
	// pay that cost for project sessions so stdio MCP (gopls, etc.) sees the
	// correct module root via the agent process cwd. Per-session model and
	// thinking ride session/new `_meta` (MADR 0106), so they no longer rebuild
	// argv and can claim the spare when cwd matches.
	var s *session
	if p.cfg.Prewarm && slices.Equal(args, p.cfg.Args) {
		s = p.claimWarm(cwd)
	}
	if s != nil {
		p.log.Info("claimed prewarmed agent",
			slog.String("session_id", localID),
			slog.String("proc_dir", s.procDir),
		)
	} else {
		s, err = p.spawnAgent(ctx, args, cwd)
		if err != nil {
			return nil, err
		}
	}
	if err := rejectUnauthenticated(s); err != nil {
		s.markClosedAndKill()
		return nil, err
	}
	// The spare is NOT re-armed here (MADR 0137 F5). Re-arming on Start put a
	// full ~3.8 s spawn — process launch, ACP initialize, model-catalog
	// harvest — in flight at the same moment as the user's first prompt,
	// competing with the turn they are waiting on. The turn-end path in
	// session.go re-arms instead, when nothing is waiting.
	//
	// The cold first spare is armed by the daemon at startup, which is where
	// EnsureWarm's own doc comment says the initial call belongs.

	return p.openSession(ctx, s, opts, cwd, localID)
}

// openSession binds s to the session the client asked for, and completes
// session/new or session/load on its connection.
func (p *Provider) openSession(ctx context.Context, s *session, opts provider.StartOptions, cwd, localID string) (provider.Session, error) {
	// Bind the session identity. Safe without external synchronization even
	// on a warm claim: the agent emits no session-scoped callbacks before
	// session/new|load below.
	//
	// thinkingLevel / currentModelID start as the intended `_meta` values
	// (opts then config). Harvest from session/new|load `_meta` overwrites
	// them with what the agent applied (MADR 0106).
	effectiveThinking := strings.TrimSpace(opts.ThinkingLevel)
	if effectiveThinking == "" {
		effectiveThinking = p.cfg.ReasoningEffort
	}
	effectiveModel := strings.TrimSpace(opts.Model)
	if effectiveModel == "" {
		effectiveModel = p.cfg.Model
	}
	s.mu.Lock()
	s.localID = localID
	s.cwd = cwd
	s.thinkingLevel = effectiveThinking
	s.currentModelID = effectiveModel
	s.log = p.log.With(slog.String("session_id", localID))
	s.mu.Unlock()

	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	initCtx, initCancel := context.WithTimeout(parent, p.startTimeout())
	defer initCancel()
	conn := s.conn
	killAndReap := s.markClosedAndKill

	// Filtered against the SDK's negotiated record, which its gate also reads:
	// the SDK refuses a session/new or session/load carrying a transport the
	// agent did not advertise, so an unadvertised server is dropped here with a
	// warning rather than failing the session (MADR 0184 D4).
	negotiated, _ := s.negotiation()
	mcpServers := p.buildMcpServers(negotiated.Response.AgentCapabilities, s.log)

	if opts.AgentSessionID != "" {
		// Gate on the advertised capability: an agent without loadSession would
		// reject session/load, so fail early with a clear message rather than a
		// raw protocol error.
		if !s.agentCaps.LoadSession {
			killAndReap()
			return nil, fmt.Errorf("acp: agent does not support session/load (loadSession capability absent); cannot resume %s", opts.AgentSessionID)
		}
		// Replaying a long conversation takes far longer than a fresh new.
		loadCtx, loadCancel := context.WithTimeout(parent, loadTimeout)
		defer loadCancel()
		initCtx = loadCtx
		// The agent replays the whole prior conversation as ordinary updates
		// during load; mark them Replay so they populate history without
		// being re-broadcast to clients that already display the transcript.
		s.mu.Lock()
		s.loading = true
		s.agentID = opts.AgentSessionID
		s.mu.Unlock()
		loadReq := acp.LoadSessionRequest{
			Cwd:        cwd,
			McpServers: mcpServers,
			SessionId:  acp.SessionId(opts.AgentSessionID),
			Meta:       p.sessionOpenMeta(opts),
		}
		loadResp, err := conn.LoadSession(initCtx, loadReq)
		// The last replayed prompt, while it still counts as replay.
		s.flushReplayedUser()
		s.mu.Lock()
		s.loading = false
		s.mu.Unlock()
		if err != nil {
			killAndReap()
			return nil, fmt.Errorf("acp session/load: %w", err)
		}
		s.mu.Lock()
		p.applySessionState(s, loadResp.Meta, loadResp.ConfigOptions)
		s.mu.Unlock()
		s.log.Info("acp session loaded", slog.String("agent_session_id", opts.AgentSessionID))
		s.emitCapabilities()
		s.emitModesOrStatic(loadResp.Modes)
		s.emitConfigOptions(loadResp.ConfigOptions)
	} else {
		newSess, err := conn.NewSession(initCtx, sessionOpenNew(cwd, mcpServers, p.sessionOpenMeta(opts)))
		if err != nil {
			killAndReap()
			return nil, fmt.Errorf("acp session/new: %w", err)
		}
		s.mu.Lock()
		s.agentID = string(newSess.SessionId)
		p.applySessionState(s, newSess.Meta, newSess.ConfigOptions)
		s.mu.Unlock()
		s.log.Info("acp session created", slog.String("agent_session_id", string(newSess.SessionId)))
		s.applyStartOptions(initCtx, effectiveModel, effectiveThinking)

		if p.spec.ConfigureSession != nil {
			if err := p.spec.ConfigureSession(initCtx, conn, newSess, opts, p.cfg, s.log); err != nil {
				killAndReap()
				return nil, fmt.Errorf("acp session configure: %w", err)
			}
		}
		s.emitCapabilities()
		s.emitModesOrStatic(newSess.Modes)
		// The options as recorded, which include any applyStartOptions set.
		s.mu.Lock()
		configOptions := s.configOptions
		s.mu.Unlock()
		s.emitConfigOptions(configOptions)
	}

	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "idle",
		AgentSessionID: s.agentID,
	})

	// From here the manager is guaranteed to attach its pump; control-event
	// delivery may now block instead of dropping oldest (see session.attached).
	s.mu.Lock()
	s.attached = true
	s.mu.Unlock()
	p.track(s)

	return s, nil
}

// startTimeout is the Spec's StartTimeout, or the package's when it sets none.
func (p *Provider) startTimeout() time.Duration {
	if p.spec.StartTimeout > 0 {
		return p.spec.StartTimeout
	}
	return startTimeout
}

// StartOn is Start on a connection it is given instead of a spawned agent:
// r is what the agent writes and w what it reads, and the caller owns both. It
// runs the same handshake and session/new or session/load. It is for a Spec's
// own tests and the conformance suite, on the fixture agent in acptest (MADR
// 0208, P6b deviation 1); the daemon starts agents with Start.
func (p *Provider) StartOn(ctx context.Context, opts provider.StartOptions, r io.Reader, w io.Writer) (provider.Session, error) {
	cwd, err := p.resolveSessionCWD(opts.CWD)
	if err != nil {
		return nil, err
	}
	localID := opts.LocalSessionID
	if localID == "" {
		localID = uuid.NewString()
	}
	s := p.newAgentSession(cwd)
	// No process, so no exit watcher.
	close(s.watched)
	s.conn = acp.NewClientSideConnection(s, w, r, clientConnOptions(p.log, s)...)
	if err := p.handshake(ctx, s, s.conn, func(err error) error {
		s.markClosedAndKill()
		return err
	}); err != nil {
		return nil, err
	}
	go s.watchConnClose(s.conn)
	if err := rejectUnauthenticated(s); err != nil {
		s.markClosedAndKill()
		return nil, err
	}
	return p.openSession(ctx, s, opts, cwd, localID)
}

// slogWriter adapts process stderr lines into slog (bounded; no file growth).
// Optional onLine fans complete lines to limit detection (MADR 0073 F1).
type slogWriter struct {
	log    *slog.Logger
	level  slog.Level
	prefix string
	onLine func(string)
	buf    []byte
}

func (w *slogWriter) Write(p []byte) (int, error) {
	var lines []string
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			// Cap a runaway line without a newline.
			if len(w.buf) > 4096 {
				line := string(w.buf[:4096]) + "…"
				w.log.Log(context.Background(), w.level, w.prefix, slog.String("line", line))
				lines = append(lines, line)
				w.buf = w.buf[:0]
			}
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		// Copy the tail down instead of re-slicing so consumed prefixes don't
		// pin the backing array.
		n := copy(w.buf, w.buf[i+1:])
		w.buf = w.buf[:n]
		if line != "" {
			w.log.Log(context.Background(), w.level, w.prefix, slog.String("line", line))
			lines = append(lines, line)
		}
	}
	if w.onLine != nil {
		for _, line := range lines {
			w.onLine(line)
		}
	}
	return len(p), nil
}

var _ io.Writer = (*slogWriter)(nil)

// clientConnOptions are the options every agent connection is built with.
//
// OverflowBlock is the SDK's default since acp-go-sdk2 v1.29.0, named here so
// these options state what production builds (MADR 0224 D2). When the SDK's
// notification queue is full, at its count or at its byte budget (64 MiB by
// default, which Magic takes: MADR 0224 D10), the reader pauses until a
// notification handler returns, so the agent's writes wait and nothing is
// dropped or closed. The SDK reads on, and past twice the queue's size or
// budget closes, only while a handler's own request or the initialize is in
// flight; no notification handler here sends a request (SessionUpdate). A
// request from the agent that would take what the connection holds past the
// budget is refused by the SDK with -32603, and its handler here does not run
// (MADR 0224 F13, F14).
//
// A pause holds only this session: each Magic session has its own agent
// process and connection (spawnAgent; MADR 0224 F6).
//
// The inbound validation hook records onto s (MADR 0207 D13). It runs in the
// SDK's dispatch, so it only stores, and the per-turn notice reads what it
// stored. A nil s records nothing, as the stalled-pump fixture builds its
// options before its session.
//
// The logger and the message bound are set here too (MADR 0184 D2). log goes
// to WithLogger, which applies it before any message is read; nil keeps the
// SDK's default. maxACPMessageSize is the SDK's own default, stated so the
// bound is visible in this repository: a longer line is answered -32600 and
// skipped, not fatal to the connection.
//
// One definition, shared with the stalled-pump and wire tests, so they exercise
// what production runs: naming another policy here turns
// TestABlockedHandlerPausesTheAgentAndLosesNothing red (MADR 0224 D4).
func clientConnOptions(log *slog.Logger, s *session) []acp.ConnectionOption {
	opts := []acp.ConnectionOption{
		acp.WithNotificationOverflowPolicy(acp.OverflowBlock),
		acp.WithInboundValidation(s.noteInbound),
		acp.WithLogger(log),
		acp.WithMaxMessageSize(maxACPMessageSize),
	}
	// Only for a Spec that registers a vendor notification method (MADR 0214
	// D14, corrected again): every other Spec's connection is as it was.
	if s != nil && s.hasVendorNotifications() {
		opts = append(opts, acp.WithReceivingMiddleware(s.vendorNotifications))
	}
	return opts
}

// maxACPMessageSize bounds one inbound ACP line, in bytes.
const maxACPMessageSize = 10 << 20

// clientName is the clientInfo name Magic sends at initialize.
const clientName = "mcremote"

// defaultClientCapabilities is what this package implements as an ACP client,
// and what initialize advertises unless a Spec says otherwise (MADR 0184 D4):
//
//   - fs.readTextFile and fs.writeTextFile: session.ReadTextFile and
//     WriteTextFile;
//   - terminal: the terminalHost behind session's terminal methods;
//   - session.configOptions.boolean: emitConfigOptions maps boolean options to
//     the phone, and SetConfigOption sends them back. acp-go-sdk2 refuses a
//     session/new answer carrying one unless this is advertised.
//   - session.notices: SessionUpdate maps a notice to agent_notice, which the
//     daemon keeps in history and the phone draws with its severity. Agents
//     send notices only when this is advertised, and the SDK refuses one
//     otherwise (MADR 0188 D4).
//   - session.compaction: SessionUpdate folds compaction_update and
//     compaction_summary_chunk by ID into compaction events, which the daemon
//     keeps in history and the phone draws as one row per compaction. Agents
//     send them only when this is advertised, and the SDK refuses one
//     otherwise (MADR 0190 D4).
//   - elicitation.form: CreateElicitation maps a session-scoped form onto the
//     question flow, checks the answer against the schema and returns typed
//     content, a decline or a cancel. URL mode is not advertised, so the SDK
//     refuses it (MADR 0191 D1, D6).
//   - plan: SessionUpdate maps plan_update and plan_removed onto
//     identified_plan events, keyed by plan ID, which the phone draws beside
//     the stable plan and removes one at a time. The capability is UNSTABLE
//     in ACP; TestPlanUpdateContentVariantsArePinned fails if the SDK changes
//     its shapes (MADR 0192 D1–D6).
//
// Nothing else is advertised until its behavior exists; the
// capability-honesty test holds each Spec to that.
func defaultClientCapabilities() acp.ClientCapabilities {
	return acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
		Session: &acp.ClientSessionCapabilities{
			ConfigOptions: &acp.SessionConfigOptionsCapabilities{Boolean: &acp.BooleanConfigOptionCapabilities{}},
			Notices:       &acp.NoticeCapabilities{},
			Compaction:    &acp.CompactionCapabilities{},
		},
		Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}},
		// UNSTABLE in ACP: may change in an SDK bump, which the plan tests pin.
		//lint:ignore SA1019 advertises the unstable ACP schema's plan capability (MADR 0192); 0207 amendment H
		Plan: &acp.PlanCapabilities{},
	}
}

// ClientCapabilities is what initialize advertises to this provider's agent:
// the Spec's own set when it has one, otherwise defaultClientCapabilities.
func (p *Provider) ClientCapabilities() acp.ClientCapabilities {
	if p.spec.ClientCapabilities != nil {
		return *p.spec.ClientCapabilities
	}
	return defaultClientCapabilities()
}

// implementedClientCapabilities are the capabilities this package has the
// behavior for, as dotted paths of the initialize clientCapabilities block.
// See defaultClientCapabilities for what implements each.
var implementedClientCapabilities = map[string]bool{
	"fs.readTextFile":               true,
	"fs.writeTextFile":              true,
	"terminal":                      true,
	"session.configOptions.boolean": true,
	"session.notices":               true,
	"session.compaction":            true,
	"elicitation.form":              true,
	"plan":                          true,
}

// capabilityContainers are the objects of the clientCapabilities block that
// group capabilities rather than being one. auth and fs are always present,
// and an empty one means "none of these"; session and session.configOptions
// are present whenever anything under them is.
var capabilityContainers = map[string]bool{
	"auth":                  true,
	"fs":                    true,
	"session":               true,
	"session.configOptions": true,
}

// CheckClientCapabilities returns one problem for every capability c
// advertises that this package does not implement, and nil when c is honest
// (MADR 0184 D4: a capability is advertised exactly when its behavior exists).
// A capability is advertised when its value is true, or a present object with
// no fields of its own, such as session.configOptions.boolean's {}, unless the
// object is one of the capabilityContainers.
func CheckClientCapabilities(c acp.ClientCapabilities) []string {
	b, err := json.Marshal(c)
	if err != nil {
		return []string{"clientCapabilities do not marshal: " + err.Error()}
	}
	var tree map[string]any
	if err := json.Unmarshal(b, &tree); err != nil {
		return []string{"clientCapabilities do not decode: " + err.Error()}
	}
	var problems []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case bool:
			if t && !implementedClientCapabilities[prefix] {
				problems = append(problems, prefix+" is advertised but not implemented")
			}
		case map[string]any:
			if len(t) == 0 && prefix != "" && !capabilityContainers[prefix] {
				if !implementedClientCapabilities[prefix] {
					problems = append(problems, prefix+" is advertised but not implemented")
				}
				return
			}
			for k, child := range t {
				if k == "_meta" {
					continue
				}
				path := k
				if prefix != "" {
					path = prefix + "." + k
				}
				walk(path, child)
			}
		case []any:
			if len(t) > 0 {
				problems = append(problems, prefix+" is advertised but not implemented")
			}
		}
	}
	walk("", tree)
	slices.Sort(problems)
	return problems
}

// initializeRequest is the initialize Magic sends every agent this provider
// spawns: its client capabilities, and clientInfo naming the daemon and its
// version (MADR 0184 D4).
func (p *Provider) initializeRequest() acp.InitializeRequest {
	version := p.cfg.ClientVersion
	if version == "" {
		version = "dev"
	}
	return acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: p.ClientCapabilities(),
		ClientInfo:         &acp.Implementation{Name: clientName, Version: version},
	}
}

// applyInitializeResponse records what the agent reported at initialize: the
// vendor model state and engine version from _meta, the capabilities, and the
// advertised auth methods.
//
// It is split out of spawnAgent so it can be tested without launching a
// process. Before it was, nothing in the package failed when the vendor block
// was discarded — at HEAD or after the typed-Initialize change (PLAN 0167 P2,
// deviation of 2026-09-22).
func (p *Provider) applyInitializeResponse(s *session, initResp acp.InitializeResponse) {
	var info InitializeInfo
	if p.spec.InitializeMeta != nil {
		info = p.spec.InitializeMeta(initResp.Meta)
	}
	if info.DefaultAuthMethodID != "" {
		s.log.Debug("acp defaultAuthMethodId",
			slog.String("default_auth_method_id", info.DefaultAuthMethodID))
	}
	// The model the agent says it is running, before any session exists. It is
	// what ModelReporter falls back to: the per-session harvest fires only when
	// the agent names its model in session `_meta`, and on the default-model
	// path the requested model is empty — which is why all seven turn records
	// in MADR 0138's table carried no model at all.
	if id := strings.TrimSpace(info.CurrentModel); id != "" {
		s.mu.Lock()
		s.engineModelID = id
		s.mu.Unlock()
	}
	if info.Models != nil {
		p.catalogMu.Lock()
		p.catalogCache = *info.Models
		p.catalogHas = true
		p.catalogMu.Unlock()
	}

	p.reportEngineVersion(&initResp, info.Version)

	s.agentCaps = initResp.AgentCapabilities
	s.log.Info("acp initialized",
		slog.Any("protocol_version", initResp.ProtocolVersion),
		slog.Bool("load_session", initResp.AgentCapabilities.LoadSession),
		slog.Bool("prompt_image", initResp.AgentCapabilities.PromptCapabilities.Image),
		slog.Int("auth_methods", len(initResp.AuthMethods)),
	)

	advertised := make([]string, 0, len(initResp.AuthMethods))
	for _, m := range initResp.AuthMethods {
		if m.Agent != nil && m.Agent.Id != "" {
			advertised = append(advertised, string(m.Agent.Id))
		}
	}
	s.advertisedAuth = advertised
}
