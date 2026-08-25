package codex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

type notificationRoute uint8

const (
	notificationRouteUnknown notificationRoute = iota
	notificationRouteSession
	notificationRouteProvider
)

// codex01491NotificationRoutes is exhaustive for the installed stable schema.
// Unknown methods are never forwarded and their params are never logged.
var codex01491NotificationRoutes = map[string]notificationRoute{
	"account/login/completed":                   notificationRouteProvider,
	"account/rateLimits/updated":                notificationRouteProvider,
	"account/updated":                           notificationRouteProvider,
	"app/list/updated":                          notificationRouteProvider,
	"autoApprovalReview/strictReviewRequired":   notificationRouteSession,
	"command/exec/outputDelta":                  notificationRouteSession,
	"configWarning":                             notificationRouteProvider,
	"deprecationNotice":                         notificationRouteProvider,
	"error":                                     notificationRouteProvider,
	"externalAgentConfig/import/completed":      notificationRouteProvider,
	"externalAgentConfig/import/progress":       notificationRouteProvider,
	"fs/changed":                                notificationRouteProvider,
	"fuzzyFileSearch/sessionCompleted":          notificationRouteProvider,
	"fuzzyFileSearch/sessionUpdated":            notificationRouteProvider,
	"guardianWarning":                           notificationRouteProvider,
	"hook/completed":                            notificationRouteSession,
	"hook/started":                              notificationRouteSession,
	"item/agentMessage/delta":                   notificationRouteSession,
	"item/autoApprovalReview/completed":         notificationRouteSession,
	"item/autoApprovalReview/started":           notificationRouteSession,
	"item/commandExecution/outputDelta":         notificationRouteSession,
	"item/commandExecution/terminalInteraction": notificationRouteSession,
	"item/completed":                            notificationRouteSession,
	"item/fileChange/outputDelta":               notificationRouteSession,
	"item/fileChange/patchUpdated":              notificationRouteSession,
	"item/mcpToolCall/progress":                 notificationRouteSession,
	"item/plan/delta":                           notificationRouteSession,
	"item/reasoning/summaryPartAdded":           notificationRouteSession,
	"item/reasoning/summaryTextDelta":           notificationRouteSession,
	"item/reasoning/textDelta":                  notificationRouteSession,
	"item/started":                              notificationRouteSession,
	"mcpServer/oauthLogin/completed":            notificationRouteProvider,
	"mcpServer/startupStatus/updated":           notificationRouteProvider,
	"model/rerouted":                            notificationRouteSession,
	"model/safetyBuffering/updated":             notificationRouteSession,
	"model/verification":                        notificationRouteSession,
	"process/exited":                            notificationRouteSession,
	"process/outputDelta":                       notificationRouteSession,
	"project/changed":                           notificationRouteProvider,
	"remoteControl/status/changed":              notificationRouteProvider,
	"serverRequest/resolved":                    notificationRouteSession,
	"skills/changed":                            notificationRouteProvider,
	"thread/archived":                           notificationRouteSession,
	"thread/closed":                             notificationRouteSession,
	"thread/compacted":                          notificationRouteSession,
	"thread/deleted":                            notificationRouteSession,
	"thread/environment/connected":              notificationRouteSession,
	"thread/environment/disconnected":           notificationRouteSession,
	"thread/goal/cleared":                       notificationRouteSession,
	"thread/goal/updated":                       notificationRouteSession,
	"thread/name/updated":                       notificationRouteSession,
	"thread/project/updated":                    notificationRouteSession,
	"thread/queue/changed":                      notificationRouteSession,
	"thread/realtime/closed":                    notificationRouteSession,
	"thread/realtime/error":                     notificationRouteSession,
	"thread/realtime/itemAdded":                 notificationRouteSession,
	"thread/realtime/outputAudio/delta":         notificationRouteSession,
	"thread/realtime/sdp":                       notificationRouteSession,
	"thread/realtime/started":                   notificationRouteSession,
	"thread/realtime/transcript/delta":          notificationRouteSession,
	"thread/realtime/transcript/done":           notificationRouteSession,
	"thread/reverted":                           notificationRouteSession,
	"thread/settings/updated":                   notificationRouteSession,
	"thread/started":                            notificationRouteSession,
	"thread/status/changed":                     notificationRouteSession,
	"thread/tokenUsage/updated":                 notificationRouteSession,
	"thread/unarchived":                         notificationRouteSession,
	"turn/completed":                            notificationRouteSession,
	"turn/diff/updated":                         notificationRouteSession,
	"turn/moderationMetadata":                   notificationRouteSession,
	"turn/plan/updated":                         notificationRouteSession,
	"turn/started":                              notificationRouteSession,
	"warning":                                   notificationRouteProvider,
	"windows/worldWritableWarning":              notificationRouteProvider,
	"windowsSandbox/setupCompleted":             notificationRouteProvider,
}

func notificationRouteFor(method string) notificationRoute {
	return codex01491NotificationRoutes[method]
}

func (p *Provider) sessionsSnapshot() []*session {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		out = append(out, s)
	}
	return out
}

func (p *Provider) handleProviderNotification(method string, params json.RawMessage) {
	p.noteRuntimeProviderNotification(method, params)
	sessions := p.sessionsSnapshot()
	switch method {
	case "account/rateLimits/updated":
		for _, s := range sessions {
			s.emitRateLimit(params)
		}
	case "mcpServer/startupStatus/updated":
		for _, s := range sessions {
			s.emitMCPStartup(params)
		}
	case "warning", "guardianWarning", "configWarning", "deprecationNotice", "error", "windows/worldWritableWarning":
		var body struct {
			Message  string `json:"message"`
			Text     string `json:"text"`
			Error    string `json:"error"`
			Summary  string `json:"summary"`
			Details  string `json:"details"`
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(params, &body)
		message := firstNonEmpty(body.Message, body.Summary, body.Text, body.Error)
		if body.Details != "" {
			message += ": " + body.Details
		}
		if message == "" {
			message = "Codex reported " + method
		}
		kind := map[string]string{"guardianWarning": "guardian", "configWarning": "config", "deprecationNotice": "deprecation"}[method]
		if kind == "" {
			kind = "warning"
		}
		for _, s := range sessions {
			if body.ThreadID != "" && body.ThreadID != s.agentID {
				continue
			}
			s.emit(event.Event{Type: event.TypeCodexWarning, SessionID: s.localID, Timestamp: time.Now().UTC(), AgentSessionID: s.agentID,
				Codex: &event.CodexPayload{Key: "warning:" + method + ":" + body.ThreadID, Kind: kind, Status: "completed", Title: "Codex warning", Text: message}})
		}
	default:
		p.log.Debug("codex: provider notification observed", slog.String("method", method))
	}
}

func (s *session) emitRateLimit(params json.RawMessage) {
	var p struct {
		RateLimits struct {
			Primary *struct {
				UsedPercent int   `json:"usedPercent"`
				ResetsAt    int64 `json:"resetsAt"`
			} `json:"primary"`
		} `json:"rateLimits"`
	}
	if json.Unmarshal(params, &p) != nil || p.RateLimits.Primary == nil {
		return
	}
	primary := p.RateLimits.Primary
	var resetAt time.Time
	if primary.ResetsAt > 0 {
		resetAt = time.Unix(primary.ResetsAt, 0).UTC()
	}
	if primary.UsedPercent >= 100 {
		s.emit(event.Event{Type: event.TypeError, SessionID: s.localID, Timestamp: time.Now().UTC(), ErrorKind: "rate_limit", RetryAt: resetAt,
			Error: fmt.Sprintf("Codex rate limit reached (%d%% of the current window). Wait for the window to reset, or try again later.", primary.UsedPercent)})
		return
	}
	if primary.UsedPercent >= 90 {
		text := fmt.Sprintf("Approaching codex rate limit (%d%% of the current window).", primary.UsedPercent)
		if !resetAt.IsZero() {
			text += fmt.Sprintf(" Resets at %s.", resetAt.Local().Format(time.Kitchen))
		}
		s.emit(event.Event{Type: event.TypeNotice, SessionID: s.localID, Timestamp: time.Now().UTC(), Text: text})
	}
}

func (s *session) emitMCPStartup(params json.RawMessage) {
	var p struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(params, &p) != nil || p.Status != "failed" {
		return
	}
	message := firstNonEmpty(p.Error, "MCP server failed to start")
	s.emit(event.Event{Type: event.TypeNotice, SessionID: s.localID, Timestamp: time.Now().UTC(), Text: fmt.Sprintf("MCP server %q failed to start: %s", p.Name, message)})
}
