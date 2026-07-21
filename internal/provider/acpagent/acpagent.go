// Package acpagent implements the shared ACP CLI agent provider machinery:
// subprocess launch, the ACP client-side connection (initialize, session
// new/load, prompt, cancel, close), streaming update → event mapping, remote
// permissions, filesystem callbacks, and the terminal host. Concrete providers
// (grok, opencode) supply a Spec and reuse everything here.
package acpagent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// startTimeout bounds ACP initialize + session/new|load. The process outlives
// the request context, but hung Start must not pin the WS handler forever.
const startTimeout = 30 * time.Second

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
}

// Provider is an ACP CLI agent adapter parameterized by a Spec.
type Provider struct {
	spec Spec
	cfg  Config
	log  *slog.Logger
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

func (p *Provider) ID() provider.ID { return p.spec.ID }

func (p *Provider) Ready() bool {
	_, err := exec.LookPath(p.cfg.Bin)
	return err == nil
}

func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("%s binary %q not found in PATH: %w", p.spec.ID, p.cfg.Bin, provider.ErrNotImplemented)
	}

	cwd := opts.CWD
	if cwd == "" {
		cwd = p.cfg.DefaultCWD
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
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

	// Process must outlive the Start() request context (WS handler returns immediately).
	cmd := exec.Command(p.cfg.Bin, args...)
	cmd.Dir = cwd
	procutil.SetProcessGroup(cmd)
	log := p.log.With(slog.String("session_id", localID))
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
		providerID: p.spec.ID,
		localID:    localID,
		cwd:        cwd,
		cmd:        cmd,
		terms:      newTerminalHost(),
		log:        log,
		events:     make(chan event.Event, 256),
		cfg:        p.cfg,
		pending:    make(map[string]chan permResult),
	}

	conn := acp.NewClientSideConnection(s, stdin, stdout)
	conn.SetLogger(s.log)
	s.conn = conn

	// Bound initialize + session create/load so a hung agent cannot pin the handler.
	// Parent may be cancelled; fall back to Background so Start still gets a deadline.
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
		_ = procutil.KillProcessGroup(cmd.Process)
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	s.log.Info("acp initialized",
		slog.Any("protocol_version", initResp.ProtocolVersion),
	)

	if opts.AgentSessionID != "" {
		_, err := conn.LoadSession(initCtx, acp.LoadSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
			SessionId:  acp.SessionId(opts.AgentSessionID),
		})
		if err != nil {
			_ = procutil.KillProcessGroup(cmd.Process)
			_, _ = cmd.Process.Wait()
			return nil, fmt.Errorf("acp session/load: %w", err)
		}
		s.agentID = opts.AgentSessionID
		s.log.Info("acp session loaded", slog.String("agent_session_id", s.agentID))
	} else {
		newSess, err := conn.NewSession(initCtx, acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		if err != nil {
			_ = procutil.KillProcessGroup(cmd.Process)
			_, _ = cmd.Process.Wait()
			return nil, fmt.Errorf("acp session/new: %w", err)
		}
		s.agentID = string(newSess.SessionId)
		s.log.Info("acp session created", slog.String("agent_session_id", s.agentID))

		if p.spec.ConfigureSession != nil {
			if err := p.spec.ConfigureSession(initCtx, conn, newSess, opts, p.cfg, s.log); err != nil {
				_ = procutil.KillProcessGroup(cmd.Process)
				_, _ = cmd.Process.Wait()
				return nil, fmt.Errorf("acp session configure: %w", err)
			}
		}
	}

	// Watch process exit.
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		msg := fmt.Sprintf("%s process exited", p.spec.ID)
		if err != nil {
			msg = fmt.Sprintf("%s process exited: %v", p.spec.ID, err)
		}
		s.emit(event.Event{
			Type:      event.TypeError,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Error:     msg,
		})
		s.emit(event.Event{
			Type:      event.TypeSessionStatus,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Status:    "disconnected",
		})
	}()

	s.emit(event.Event{
		Type:           event.TypeSessionStatus,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Status:         "idle",
		AgentSessionID: s.agentID,
	})

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
		i := -1
		for j, b := range w.buf {
			if b == '\n' {
				i = j
				break
			}
		}
		if i < 0 {
			// Cap a runaway line without a newline.
			if len(w.buf) > 4096 {
				w.log.Log(context.Background(), w.level, w.prefix, slog.String("line", string(w.buf[:4096])+"…"))
				w.buf = w.buf[:0]
			}
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if line != "" {
			w.log.Log(context.Background(), w.level, w.prefix, slog.String("line", line))
		}
	}
	return len(p), nil
}

var _ io.Writer = (*slogWriter)(nil)
