// Package acpagent implements the shared ACP CLI agent provider machinery:
// subprocess launch, the ACP client-side connection (initialize, session
// new/load, prompt, cancel, close), streaming update → event mapping, remote
// permissions, filesystem callbacks, and the terminal host. Concrete providers
// (grok, opencode) supply a Spec and reuse everything here.
package acpagent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
}

// Provider is an ACP CLI agent adapter parameterized by a Spec.
type Provider struct {
	spec Spec
	cfg  Config
	log  *slog.Logger

	// warm is the single spare pre-initialized agent process (cfg.Prewarm).
	// Claimed by Start when the requested argv matches the default; refilled
	// in the background after each claim.
	warmMu  sync.Mutex
	warm    *session
	warming bool
	closed  bool
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
		spec: spec,
		cfg:  cfg,
		log:  slog.Default().With(slog.String("component", "provider."+string(spec.ID))),
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

// ListModels implements [provider.ModelCatalog].
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	static := p.staticModelCatalog()
	if p.spec.ListModels != nil {
		live, err := p.spec.ListModels(ctx, p.cfg)
		if err != nil {
			p.log.Debug("list models live failed; using static", slog.String("err", err.Error()))
			return static, nil
		}
		if len(static.Options) == 0 {
			return live.Normalize(), nil
		}
		return picker.MergeLiveStatic(live, static), nil
	}
	return static, nil
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
		providerID:    p.spec.ID,
		cmd:           cmd,
		terms:         newTerminalHost(),
		log:           log,
		events:        make(chan event.Event, 256),
		done:          make(chan struct{}),
		cfg:           p.cfg,
		pending:       make(map[string]*permWaiter),
		questions:     make(map[string]*questionWaiter),
		staticModes:   p.spec.StaticModes,
		defaultModeID: p.spec.DefaultModeID,
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

	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	})
	if err != nil {
		// cmd.Wait (not Process.Wait) so exec closes the parent ends of the
		// stdio pipes — Process.Wait leaks two fds per failed spawn. Safe
		// here: the exit watcher starts only after initialize succeeds.
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
		return nil, fmt.Errorf("acp initialize: %w", err)
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

// EnsureWarm arms (or re-arms) the spare pre-initialized agent process in the
// background. Call at daemon startup and rely on Start to re-arm after each
// claim. No-op unless cfg.Prewarm.
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
			p.warmMu.Lock()
			p.warming = false
			p.warmMu.Unlock()
		}()
		// The engine resolves the working directory per ACP session (the cwd
		// parameter of session/new|load), so the process itself can start
		// anywhere stable.
		procDir, err := os.UserHomeDir()
		if err != nil {
			procDir = "/"
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
		p.log.Info("agent prewarmed", slog.String("bin", p.cfg.Bin))
	}()
}

// claimWarm pops the spare process if it is present and still alive.
func (p *Provider) claimWarm() *session {
	p.warmMu.Lock()
	s := p.warm
	p.warm = nil
	p.warmMu.Unlock()
	if s == nil {
		return nil
	}
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

	cwd := opts.CWD
	if cwd == "" {
		cwd = p.cfg.DefaultCWD
	}
	if cwd == "" {
		// The daemon user's home, not os.Getwd(): under systemd the process
		// cwd is an accident of the unit file, and sessions should start
		// somewhere predictable when the phone leaves the field empty.
		var err error
		cwd, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for session cwd: %w", err)
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("cwd %q is not a directory", cwd)
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

	// Claim the pre-warmed process when the argv matches (engine cold start —
	// several seconds for Bun-based agents — already paid in the background).
	var s *session
	if p.cfg.Prewarm && slices.Equal(args, p.cfg.Args) {
		s = p.claimWarm()
	}
	if s != nil {
		p.log.Info("claimed prewarmed agent", slog.String("session_id", localID))
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
