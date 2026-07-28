// Package acpagent implements the shared ACP CLI agent provider machinery:
// subprocess launch, the ACP client-side connection (initialize, session
// new/load, prompt, cancel, close), streaming update → event mapping, remote
// permissions, filesystem callbacks, and the terminal host. Concrete providers
// (grok, opencode) supply a Spec and reuse everything here.
package acpagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// startTimeout bounds ACP initialize + session/new. The process outlives
// the request context, but hung Start must not pin the WS handler forever.
const startTimeout = 30 * time.Second

// loadTimeout bounds session/load separately: the agent replays the entire
// prior conversation as notifications before responding, so long sessions
// legitimately need far more than a fresh session/new.
const loadTimeout = 120 * time.Second

// Spec is what varies between ACP CLI agents. Everything else — process
// lifecycle, the ACP handshake, event mapping, permissions, terminals — is
// shared.
type Spec struct {
	// ID is the provider identity ("grok", "opencode").
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
	// StaticModels is the fallback model picker catalog when ListModels is
	// nil or fails. Empty + AllowCustom on ListModels default still lets the
	// user type a free-text model id.
	StaticModels []picker.Option
	// StaticModes is the fallback session-mode list for an agent that honors
	// session/set_mode but advertises no modes at session/new|load. Used only
	// when the agent reported none; an agent-supplied list always wins. Because
	// such an agent has no declared vocabulary to check against, SetMode
	// validates against this list instead (see session.SetMode).
	StaticModes []event.SessionMode
	// DefaultModeID is the mode treated as current at session start when
	// StaticModes is used. Empty falls back to the first entry.
	DefaultModeID string
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
	// quirk that is not per-command (grok: part of its advertised catalog is
	// terminal-only).
	CommandCaveat string
	// ExtensionNotifications registers handlers for extension notifications.
	ExtensionNotifications map[string]ExtensionNotificationHandler
}

// Provider is an ACP CLI agent adapter parameterized by a Spec.
type Provider struct {
	spec Spec
	cfg  Config
	log  *slog.Logger

	catalogMu    sync.RWMutex
	catalogCache picker.Catalog
	catalogHas   bool

	// warm is the single spare pre-initialized agent process (cfg.Prewarm).
	// Claimed by Start when the requested argv matches the default; refilled
	// in the background after each claim.
	warmMu  sync.Mutex
	warm    *session
	warming bool
	closed  bool

	// catalogs single-flights and TTLs the cold-start catalog harvest, so N
	// picker opens against a cold cache mean one agent spawn.
	catalogs *picker.Cache[string]
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

// CommandTable implements [command.Tabler].
func (p *Provider) CommandTable() command.Table { return p.spec.Commands }

// CommandCaveat implements [command.Caveater].
func (p *Provider) CommandCaveat() string { return p.spec.CommandCaveat }

// catalogProbeTimeout bounds the cold-start agent spawn used to harvest the
// model catalog. Measured: a full grok start is ~3.8 s; initialize alone, which
// is all this needs, is well inside that.
const catalogProbeTimeout = 30 * time.Second

// ListModels implements [provider.ModelCatalog].
//
// The live catalog arrives in the agent's ACP initialize `_meta` and is cached
// provider-wide, so it exists only once an agent process has run. Before MADR
// 0043 D7 that meant the create-session picker showed a hardcoded static list
// until the user had already started a session — and that list disagreed with
// the agent (it offered models grok no longer accepts and omitted the one it
// defaults to). Harvest it instead: spawning one process to read `initialize`
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
	// list. Measured on grok: the agent offers exactly one model (grok-4.5)
	// while the static list names four it no longer accepts, and merging put
	// all five in the picker — four of which fail on use. Static is the
	// fallback for having no agent, not a supplement to having one. AllowCustom
	// keeps an older install's ids typeable (MADR 0043 D7).
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
	_, err := exec.LookPath(p.cfg.Bin)
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
	cmd := exec.Command(p.cfg.Bin, args...)
	cmd.Dir = procDir
	procutil.SetProcessGroup(cmd)
	log := p.log
	// Bound stderr noise: line-oriented slog at debug (not unbounded os.Stderr).
	cmd.Stderr = &slogWriter{log: log, level: slog.LevelDebug, prefix: string(p.spec.ID) + "-stderr"}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}

	s := &session{
		providerID:              p.spec.ID,
		provider:                p,
		extNotificationHandlers: p.spec.ExtensionNotifications,
		procDir:                 procDir,
		cmd:                     cmd,
		terms:                   newTerminalHost(),
		log:                     log,
		events:                  make(chan event.Event, 256),
		done:                    make(chan struct{}),
		cfg:                     p.cfg,
		pending:                 make(map[string]*permWaiter),
		questions:               make(map[string]*questionWaiter),
		staticModes:             p.spec.StaticModes,
		defaultModeID:           p.spec.DefaultModeID,
	}

	conn := acp.NewClientSideConnection(s, stdin, stdout)
	conn.SetLogger(s.log)
	s.conn = conn

	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	initCtx, initCancel := context.WithTimeout(parent, startTimeout)
	defer initCancel()

	initReq := acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	}
	var rawInit json.RawMessage
	if err := s.rawRequest(initCtx, "initialize", initReq, &rawInit); err != nil {
		// cmd.Wait (not Process.Wait) so exec closes the parent ends of the
		// stdio pipes — Process.Wait leaks two fds per failed spawn. Safe
		// here: the exit watcher starts only after initialize succeeds.
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
		return nil, fmt.Errorf("acp initialize: %w", err)
	}

	var initResp acp.InitializeResponse
	if err := json.Unmarshal(rawInit, &initResp); err != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
		return nil, fmt.Errorf("acp initialize decode: %w", err)
	}

	var initMeta grokInitializeMeta
	_ = json.Unmarshal(rawInit, &initMeta)
	if len(initMeta.Meta.ModelState.AvailableModels) > 0 {
		cat := modelsToCatalog(initMeta.Meta.ModelState.CurrentModelID, initMeta.Meta.ModelState.AvailableModels)
		p.catalogMu.Lock()
		p.catalogCache = cat
		p.catalogHas = true
		p.catalogMu.Unlock()
	}

	s.agentCaps = initResp.AgentCapabilities
	s.log.Info("acp initialized",
		slog.Any("protocol_version", initResp.ProtocolVersion),
		slog.Bool("load_session", initResp.AgentCapabilities.LoadSession),
		slog.Bool("prompt_image", initResp.AgentCapabilities.PromptCapabilities.Image),
		slog.Int("auth_methods", len(initResp.AuthMethods)),
	)

	// Authenticate before any session/new when the agent advertises auth
	// methods and one is configured. Agents that need no auth send an empty
	// list and this is skipped (grok today). The agent validates the method id
	// and returns an error for an unknown one, so no client-side precheck of
	// the (partly unstable) AuthMethod union is needed.
	if len(initResp.AuthMethods) > 0 && p.cfg.AuthMethodID != "" {
		if _, err := conn.Authenticate(initCtx, acp.AuthenticateRequest{
			MethodId: p.cfg.AuthMethodID,
		}); err != nil {
			_ = procutil.KillProcessGroup(cmd.Process)
			_ = cmd.Wait()
			return nil, fmt.Errorf("acp authenticate (%s): %w", p.cfg.AuthMethodID, err)
		}
		s.log.Info("acp authenticated", slog.String("method_id", p.cfg.AuthMethodID))
	} else if len(initResp.AuthMethods) > 0 {
		s.log.Warn("agent advertises auth methods but none configured; session/new may fail",
			slog.Int("count", len(initResp.AuthMethods)))
	}

	// Watch process exit from here on (the watcher owns cmd.Wait; later
	// failure paths must kill via markClosedAndKill, never Wait themselves).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("process exit watcher panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
				s.signalDisconnected("process exit watcher panic")
			}
		}()
		err := cmd.Wait()
		s.mu.Lock()
		// Reaped: from here the PID may be recycled, so Close must never
		// signal the process group again.
		s.procExited = true
		s.mu.Unlock()
		msg := fmt.Sprintf("%s process exited", p.spec.ID)
		if err != nil {
			msg = fmt.Sprintf("%s process exited: %v", p.spec.ID, err)
		}
		s.signalDisconnected(msg)
	}()

	// Watch connection death independently of process exit: the ACP SDK can
	// tear the connection down (e.g. its inbound notification queue overflowing
	// behind a blocked handler) while leaving the agent process alive. Without
	// this the session would zombie. See session.watchConnClose.
	go s.watchConnClose(conn)

	return s, nil
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
			out = append(out, acp.McpServer{Http: &acp.McpServerHttpInline{
				Type: "http", Name: m.Name, Url: m.URL, Headers: headers,
			}})
		case McpSSE:
			if !caps.McpCapabilities.Sse {
				log.Warn("dropping sse MCP server: agent lacks mcpCapabilities.sse", slog.String("name", m.Name))
				continue
			}
			out = append(out, acp.McpServer{Sse: &acp.McpServerSseInline{
				Type: "sse", Name: m.Name, Url: m.URL, Headers: headers,
			}})
		default:
			log.Warn("dropping MCP server with unknown transport",
				slog.String("name", m.Name), slog.String("transport", string(m.Transport)))
		}
	}
	return out
}

// resolveSessionCWD picks the absolute working directory for a new session:
// StartOptions.CWD, else Config.DefaultCWD, else the daemon user's home.
// Under systemd the daemon process cwd is an accident of the unit file, so
// empty always means home (or DefaultCWD), never os.Getwd().
func (p *Provider) resolveSessionCWD(optsCWD string) (string, error) {
	cwd := optsCWD
	if cwd == "" {
		cwd = p.cfg.DefaultCWD
	}
	if cwd == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir for session cwd: %w", err)
		}
		cwd = home
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory", cwd)
	}
	return cwd, nil
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
		p.warmMu.Lock()
		if p.closed || p.warm != nil {
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
	dead := s.procExited || s.closed
	s.mu.Unlock()
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

	args := append([]string{}, p.cfg.Args...)
	// Allow per-session model override by rebuilding args when model set on opts.
	if opts.Model != "" && p.cfg.Model == "" && p.spec.ModelArgs != nil {
		args = p.spec.ModelArgs(p.cfg, opts.Model)
	}

	// Claim the pre-warmed process only when argv and process OS cwd both
	// match. Engine cold start is several seconds for some agents; we still
	// pay that cost for project sessions so stdio MCP (gopls, etc.) sees the
	// correct module root via the agent process cwd.
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
	// Re-arm the spare for the next create (also covers the cold first one).
	defer p.EnsureWarm()

	// Bind the session identity. Safe without external synchronization even
	// on a warm claim: the agent emits no session-scoped callbacks before
	// session/new|load below.
	s.mu.Lock()
	s.localID = localID
	s.cwd = cwd
	s.log = p.log.With(slog.String("session_id", localID))
	s.mu.Unlock()

	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	initCtx, initCancel := context.WithTimeout(parent, startTimeout)
	defer initCancel()
	conn := s.conn
	killAndReap := s.markClosedAndKill

	mcpServers := p.buildMcpServers(s.agentCaps, s.log)

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
		loadResp, err := conn.LoadSession(initCtx, acp.LoadSessionRequest{
			Cwd:        cwd,
			McpServers: mcpServers,
			SessionId:  acp.SessionId(opts.AgentSessionID),
		})
		s.mu.Lock()
		s.loading = false
		s.mu.Unlock()
		if err != nil {
			killAndReap()
			return nil, fmt.Errorf("acp session/load: %w", err)
		}
		s.log.Info("acp session loaded", slog.String("agent_session_id", opts.AgentSessionID))
		s.emitCapabilities()
		s.emitModesOrStatic(loadResp.Modes)
		s.emitConfigOptions(loadResp.ConfigOptions)
	} else {
		newSess, err := conn.NewSession(initCtx, acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: mcpServers,
		})
		if err != nil {
			killAndReap()
			return nil, fmt.Errorf("acp session/new: %w", err)
		}
		s.mu.Lock()
		s.agentID = string(newSess.SessionId)
		s.mu.Unlock()
		s.log.Info("acp session created", slog.String("agent_session_id", string(newSess.SessionId)))

		if p.spec.ConfigureSession != nil {
			if err := p.spec.ConfigureSession(initCtx, conn, newSess, opts, p.cfg, s.log); err != nil {
				killAndReap()
				return nil, fmt.Errorf("acp session configure: %w", err)
			}
		}
		s.emitCapabilities()
		s.emitModesOrStatic(newSess.Modes)
		s.emitConfigOptions(newSess.ConfigOptions)
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

	return s, nil
}

// slogWriter adapts process stderr lines into slog (bounded; no file growth).
type slogWriter struct {
	log    *slog.Logger
	level  slog.Level
	prefix string
	buf    []byte
}

func (w *slogWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			// Cap a runaway line without a newline.
			if len(w.buf) > 4096 {
				w.log.Log(context.Background(), w.level, w.prefix, slog.String("line", string(w.buf[:4096])+"…"))
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
		}
	}
	return len(p), nil
}

var _ io.Writer = (*slogWriter)(nil)

// GrokAvailableModel represents an available model in grok's initialize response.
type GrokAvailableModel struct {
	ModelID string `json:"modelId"`
	Name    string `json:"name"`
	Meta    struct {
		TotalContextTokens      int    `json:"totalContextTokens"`
		SupportsReasoningEffort bool   `json:"supportsReasoningEffort"`
		ReasoningEffort         string `json:"reasoningEffort"`
	} `json:"_meta"`
}

type grokInitializeMeta struct {
	Meta struct {
		ModelState struct {
			CurrentModelID  string               `json:"currentModelId"`
			AvailableModels []GrokAvailableModel `json:"availableModels"`
		} `json:"modelState"`
	} `json:"_meta"`
}

func modelsToCatalog(currentID string, models []GrokAvailableModel) picker.Catalog {
	opts := make([]picker.Option, 0, len(models))
	for _, m := range models {
		label := m.Name
		if label == "" {
			label = m.ModelID
		}
		opts = append(opts, picker.Option{
			ID:    m.ModelID,
			Label: label,
			Group: "xai",
		})
	}
	var defaults []string
	if currentID != "" {
		defaults = []string{currentID}
	}
	return picker.Catalog{
		Kind:        picker.KindSingle,
		Source:      picker.SourceLive,
		Options:     opts,
		DefaultIDs:  defaults,
		AllowCustom: true,
		MaxSelect:   1,
	}.Normalize()
}

// HandleModelsUpdate handles _x.ai/models_update extension notifications.
func HandleModelsUpdate(ctx context.Context, s *session, params json.RawMessage) {
	var p struct {
		AvailableModels []GrokAvailableModel `json:"availableModels"`
		CurrentModelID  string               `json:"currentModelId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		if s.log != nil {
			s.log.Debug("models_update: parse failed", slog.String("err", err.Error()))
		}
		return
	}
	if len(p.AvailableModels) == 0 {
		return
	}
	cat := modelsToCatalog(p.CurrentModelID, p.AvailableModels)
	if s.provider != nil {
		s.provider.catalogMu.Lock()
		s.provider.catalogCache = cat
		s.provider.catalogHas = true
		s.provider.catalogMu.Unlock()
	}
}

// HandleMCPStatus handles _x.ai/mcp/server_status notifications.
func HandleMCPStatus(ctx context.Context, s *session, params json.RawMessage) {
	var p struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if p.Name == "" {
		return
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	found := false
	for i, st := range s.mcpStatus {
		if st.Name == p.Name {
			s.mcpStatus[i].State = p.Status
			found = true
			break
		}
	}
	if !found {
		s.mcpStatus = append(s.mcpStatus, provider.MCPServerStatus{
			Name:  p.Name,
			State: p.Status,
		})
	}
}

// HandleMCPInit handles _x.ai/mcp_initialized notifications.
func HandleMCPInit(ctx context.Context, s *session, params json.RawMessage) {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if len(s.mcpStatus) == 0 && len(s.cfg.McpServers) > 0 {
		for _, srv := range s.cfg.McpServers {
			s.mcpStatus = append(s.mcpStatus, provider.MCPServerStatus{
				Name:  srv.Name,
				State: "ready",
			})
		}
	}
}
