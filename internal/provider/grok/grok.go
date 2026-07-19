// Package grok implements the Grok Build ACP provider adapter.
package grok

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Provider is the Grok Build ACP adapter.
type Provider struct {
	cfg Config
	log *slog.Logger
}

// New creates a Grok provider with defaults for empty fields.
func New(cfg Config) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = "grok"
	}
	if len(cfg.Args) == 0 {
		cfg.Args = defaultArgs(cfg)
	}
	return &Provider{
		cfg: cfg,
		log: slog.Default().With(slog.String("component", "provider.grok")),
	}
}

// NewWithLogger is like New but sets a logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	p := New(cfg)
	if log != nil {
		p.log = log.With(slog.String("component", "provider.grok"))
	}
	return p
}

func defaultArgs(cfg Config) []string {
	// Global flags before the stdio subcommand.
	args := []string{"agent", "--no-leader"}
	if cfg.AlwaysApprove {
		args = append(args, "--always-approve")
	}
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	args = append(args, "stdio")
	return args
}

func (p *Provider) ID() provider.ID { return provider.IDGrok }

func (p *Provider) Ready() bool {
	_, err := exec.LookPath(p.cfg.Bin)
	return err == nil
}

func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("grok binary %q not found in PATH: %w", p.cfg.Bin, provider.ErrNotImplemented)
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
	if opts.Model != "" && p.cfg.Model == "" {
		args = defaultArgs(Config{
			AlwaysApprove: p.cfg.AlwaysApprove,
			Model:         opts.Model,
		})
	}

	// Process must outlive the Start() request context (WS handler returns immediately).
	cmd := exec.Command(p.cfg.Bin, args...)
	cmd.Dir = cwd
	cmd.Stderr = os.Stderr // surface agent diagnostics; can switch to slog later
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
	_ = ctx // reserved for future start deadlines

	s := &session{
		localID: localID,
		cwd:     cwd,
		cmd:     cmd,
		terms:   newTerminalHost(),
		log:     p.log.With(slog.String("session_id", localID)),
		events:  make(chan event.Event, 256),
		cfg:     p.cfg,
		pending: make(map[string]chan permResult),
	}

	conn := acp.NewClientSideConnection(s, stdin, stdout)
	conn.SetLogger(s.log)
	s.conn = conn

	// Initialize must use a non-cancelled parent for long-lived process.
	initCtx := context.Background()
	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	})
	if err != nil {
		_ = cmd.Process.Kill()
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
			_ = cmd.Process.Kill()
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
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("acp session/new: %w", err)
		}
		s.agentID = string(newSess.SessionId)
		s.log.Info("acp session created", slog.String("agent_session_id", s.agentID))
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
		msg := "grok process exited"
		if err != nil {
			msg = fmt.Sprintf("grok process exited: %v", err)
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
