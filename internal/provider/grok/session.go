package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// session is one Grok ACP-backed conversation.
type session struct {
	localID string
	agentID string
	cwd     string
	conn    *acp.ClientSideConnection
	cmd     *exec.Cmd
	terms   *terminalHost
	log     *slog.Logger
	events  chan event.Event
	cfg     Config

	mu        sync.Mutex
	closed    bool
	prompting bool
	pending   map[string]chan permResult // permissionID -> result
}

type permResult struct {
	optionID  string
	cancelled bool
}

var _ provider.Session = (*session)(nil)
var _ provider.PermissionSession = (*session)(nil)
var _ acp.Client = (*session)(nil)

func (s *session) ID() string                 { return s.localID }
func (s *session) ProviderID() provider.ID    { return provider.IDGrok }
func (s *session) AgentSessionID() string     { return s.agentID }
func (s *session) Events() <-chan event.Event { return s.events }

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.prompting {
		s.mu.Unlock()
		return fmt.Errorf("prompt already in progress")
	}
	s.prompting = true
	s.mu.Unlock()

	var text strings.Builder
	blocks := make([]acp.ContentBlock, 0, len(parts))
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			text.WriteString(p.Text)
			blocks = append(blocks, acp.TextBlock(p.Text))
		}
	}

	s.emit(event.Event{
		Type:      event.TypeUserMessage,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Text:      text.String(),
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "running",
	})

	go func() {
		defer func() {
			s.mu.Lock()
			s.prompting = false
			s.mu.Unlock()
		}()

		resp, err := s.conn.Prompt(ctx, acp.PromptRequest{
			SessionId: acp.SessionId(s.agentID),
			Prompt:    blocks,
		})
		if err != nil {
			s.emit(event.Event{
				Type:      event.TypeError,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Error:     err.Error(),
			})
			s.emit(event.Event{
				Type:      event.TypeSessionStatus,
				SessionID: s.localID,
				Timestamp: time.Now().UTC(),
				Status:    "error",
			})
			return
		}
		s.emit(event.Event{
			Type:       event.TypeTurnComplete,
			SessionID:  s.localID,
			Timestamp:  time.Now().UTC(),
			Status:     string(resp.StopReason),
			StopReason: string(resp.StopReason),
		})
		s.emit(event.Event{
			Type:      event.TypeSessionStatus,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Status:    "idle",
		})
	}()
	return nil
}

func (s *session) Cancel(ctx context.Context) error {
	s.mu.Lock()
	// Cancel pending permissions as cancelled.
	for id, ch := range s.pending {
		select {
		case ch <- permResult{cancelled: true}:
		default:
		}
		delete(s.pending, id)
	}
	s.mu.Unlock()

	return s.conn.Cancel(ctx, acp.CancelNotification{
		SessionId: acp.SessionId(s.agentID),
	})
}

func (s *session) RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error {
	_ = ctx
	s.mu.Lock()
	ch, ok := s.pending[permissionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown or expired permission %q", permissionID)
	}
	select {
	case ch <- permResult{optionID: optionID, cancelled: cancelled}:
		return nil
	default:
		return fmt.Errorf("permission %q already resolved", permissionID)
	}
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for id, ch := range s.pending {
		select {
		case ch <- permResult{cancelled: true}:
		default:
		}
		delete(s.pending, id)
	}
	s.mu.Unlock()

	// Best-effort ACP close.
	_, _ = s.conn.CloseSession(ctx, acp.CloseSessionRequest{
		SessionId: acp.SessionId(s.agentID),
	})

	s.terms.CloseAll()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	close(s.events)
	return nil
}

func (s *session) emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if ev.AgentSessionID == "" {
		ev.AgentSessionID = s.agentID
	}
	select {
	case s.events <- ev:
	default:
		s.log.Warn("dropping event; slow consumer",
			slog.String("type", string(ev.Type)),
			slog.String("session_id", s.localID),
		)
	}
}

// --- acp.Client implementation ---

func (s *session) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	u := params.Update
	now := time.Now().UTC()
	switch {
	case u.AgentMessageChunk != nil:
		s.emit(event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: s.localID,
			Timestamp: now,
			Text:      contentText(u.AgentMessageChunk.Content),
		})
	case u.AgentThoughtChunk != nil:
		s.emit(event.Event{
			Type:      event.TypeThoughtChunk,
			SessionID: s.localID,
			Timestamp: now,
			Text:      contentText(u.AgentThoughtChunk.Content),
		})
	case u.UserMessageChunk != nil:
		s.emit(event.Event{
			Type:      event.TypeUserMessage,
			SessionID: s.localID,
			Timestamp: now,
			Text:      contentText(u.UserMessageChunk.Content),
		})
	case u.ToolCall != nil:
		tc := u.ToolCall
		status := string(tc.Status)
		s.emit(event.Event{
			Type:      event.TypeToolCall,
			SessionID: s.localID,
			Timestamp: now,
			ToolID:    string(tc.ToolCallId),
			ToolName:  tc.Title,
			Status:    status,
			Text:      tc.Title,
		})
	case u.ToolCallUpdate != nil:
		tu := u.ToolCallUpdate
		status := ""
		if tu.Status != nil {
			status = string(*tu.Status)
		}
		title := ""
		if tu.Title != nil {
			title = *tu.Title
		}
		// Prefer raw output text if present.
		text := title
		if tu.RawOutput != nil {
			if b, err := json.Marshal(tu.RawOutput); err == nil {
				text = string(b)
			}
		}
		s.emit(event.Event{
			Type:      event.TypeToolUpdate,
			SessionID: s.localID,
			Timestamp: now,
			ToolID:    string(tu.ToolCallId),
			ToolName:  title,
			Status:    status,
			Text:      text,
		})
	default:
		// Ignore plan/mode/etc. for now; still log at debug.
		s.log.Debug("unhandled session update")
	}
	return nil
}

func (s *session) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if s.cfg.AlwaysApprove {
		return autoAllow(params), nil
	}

	permID := uuid.NewString()
	opts := make([]event.PermissionOption, 0, len(params.Options))
	for _, o := range params.Options {
		opts = append(opts, event.PermissionOption{
			OptionID: string(o.OptionId),
			Name:     o.Name,
			Kind:     string(o.Kind),
		})
	}

	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}
	toolID := string(params.ToolCall.ToolCallId)

	ch := make(chan permResult, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}, nil
	}
	s.pending[permID] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, permID)
		s.mu.Unlock()
	}()

	s.emit(event.Event{
		Type:         event.TypePermission,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permID,
		Options:      opts,
		ToolID:       toolID,
		ToolName:     title,
		Text:         title,
		Status:       "pending",
	})

	select {
	case <-ctx.Done():
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}, nil
	case res := <-ch:
		if res.cancelled || res.optionID == "" {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
				},
			}, nil
		}
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{
					OptionId: acp.PermissionOptionId(res.optionID),
					Outcome:  "selected",
				},
			},
		}, nil
	}
}

func autoAllow(params acp.RequestPermissionRequest) acp.RequestPermissionResponse {
	// Prefer option kind containing "allow" / "approve", else first option.
	var chosen *acp.PermissionOption
	for i := range params.Options {
		o := &params.Options[i]
		k := strings.ToLower(string(o.Kind))
		n := strings.ToLower(o.Name)
		if strings.Contains(k, "allow") || strings.Contains(n, "allow") ||
			strings.Contains(k, "approve") || strings.Contains(n, "approve") {
			chosen = o
			break
		}
	}
	if chosen == nil && len(params.Options) > 0 {
		chosen = &params.Options[0]
	}
	if chosen == nil {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			},
		}
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{
				OptionId: chosen.OptionId,
				Outcome:  "selected",
			},
		},
	}
}

func (s *session) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	path := params.Path
	if !filepath.IsAbs(path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("path must be absolute: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	content := string(b)
	if params.Line != nil || params.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 0
		if params.Line != nil && *params.Line > 0 {
			start = min(max(*params.Line-1, 0), len(lines))
		}
		end := len(lines)
		if params.Limit != nil && *params.Limit > 0 && start+*params.Limit < end {
			end = start + *params.Limit
		}
		content = strings.Join(lines[start:end], "\n")
	}
	return acp.ReadTextFileResponse{Content: content}, nil
}

func (s *session) WriteTextFile(_ context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	path := params.Path
	if !filepath.IsAbs(path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("path must be absolute: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (s *session) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	// Default cwd to session cwd if not provided.
	if params.Cwd == nil || *params.Cwd == "" {
		cwd := s.cwd
		params.Cwd = &cwd
	}
	return s.terms.Create(ctx, params)
}

func (s *session) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return s.terms.Output(ctx, params)
}

func (s *session) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return s.terms.WaitForExit(ctx, params)
}

func (s *session) KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return s.terms.Kill(ctx, params)
}

func (s *session) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return s.terms.Release(ctx, params)
}

func contentText(c acp.ContentBlock) string {
	if c.Text != nil {
		return c.Text.Text
	}
	return ""
}
