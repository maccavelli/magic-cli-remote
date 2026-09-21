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

// notificationRoutes must cover every notification the embedded contract
// manifest declares; routing_test.go enforces that. An absent method routes to
// notificationRouteUnknown (the zero value) and is dropped with a Debug log,
// which is silent enough that seven notifications went unrouted for six Codex
// releases — see MADR 0163 F12. The name is deliberately version-neutral: its
// predecessor was called after 0.149.1 and outlived that pin (0163 F3).
var notificationRoutes = map[string]notificationRoute{
	"account/login/completed":                   notificationRouteProvider,
	"account/rateLimits/updated":                notificationRouteProvider,
	"account/updated":                           notificationRouteProvider,
	"app/list/updated":                          notificationRouteProvider,
	"autoApprovalReview/strictReviewRequired":   notificationRouteSession,
	"command/exec/outputDelta":                  notificationRouteProvider,
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
	"process/exited":                            notificationRouteProvider,
	"process/outputDelta":                       notificationRouteProvider,
	"project/changed":                           notificationRouteProvider,
	"remoteControl/status/changed":              notificationRouteProvider,
	"serverRequest/resolved":                    notificationRouteSession,
	"skills/changed":                            notificationRouteProvider,
	"thread/archived":                           notificationRouteSession,
	"thread/closed":                             notificationRouteSession,
	"thread/compacted":                          notificationRouteSession,
	"thread/deleted":                            notificationRouteSession,
	"thread/environment/connected":              notificationRouteProvider,
	"thread/environment/disconnected":           notificationRouteProvider,
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
	"thread/attachment/updated":                 notificationRouteProvider,
	"thread/realtime/item/completed":            notificationRouteProvider,
	"thread/realtime/item/started":              notificationRouteProvider,
	"thread/realtime/item/transcript/delta":     notificationRouteProvider,
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
	"mcpServer/event/stream/notification":       notificationRouteProvider,
	"modelProvider/authRecoveryCompleted":       notificationRouteSession,
	"modelProvider/authRecoveryStarted":         notificationRouteSession,
	"windows/worldWritableWarning":              notificationRouteProvider,
	"windowsSandbox/setupCompleted":             notificationRouteProvider,
}

func notificationRouteFor(method string) notificationRoute {
	return notificationRoutes[method]
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
	case "command/exec/outputDelta", "process/outputDelta", "process/exited", "thread/environment/connected", "thread/environment/disconnected":
		p.handleExecutionNotification(method, params)
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
		warning := codexWarning{method: method, threadID: body.ThreadID, kind: kind, message: message}
		delivered := 0
		for _, s := range sessions {
			if body.ThreadID != "" && body.ThreadID != s.agentID {
				continue
			}
			s.emitCodexWarning(warning)
			delivered++
		}
		// A warning nobody was listening for is not a warning that did not happen.
		// Engine start, initialize and the first resume all run before Start
		// registers the session (provider.go), so a warning raised in that window
		// found an empty session list and was dropped with no log and no trace —
		// which is how a live deprecationNotice went unobserved and left A13
		// passing either way (MADR 0163 amendment 2026-09-21).
		//
		// Only provider-global warnings are held. A thread-scoped warning whose
		// thread we do not know is deliberately let go: replaying it to whichever
		// session registers next would attribute one thread's warning to another,
		// which is worse than losing it.
		if delivered == 0 && body.ThreadID == "" {
			p.noteEarlyWarning(warning)
		}
	case "thread/attachment/updated",
		"mcpServer/event/stream/notification",
		"thread/realtime/item/started",
		"thread/realtime/item/transcript/delta",
		"thread/realtime/item/completed":
		// Declared and deliberately not consumed yet: thread attachments, MCP
		// event streams and realtime voice items are Deferred in PLAN 0163. A
		// declared drop is not the same as an unrouted one — it is greppable,
		// tested, and cannot be mistaken for a notification nobody knew about.
		p.log.Debug("codex: declared notification not consumed", slog.String("method", method))
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
