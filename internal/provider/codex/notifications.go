package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// handleNotification is the single notification decode boundary. App-server
// framing and routing call only this entry point; focused projections live in
// this file while session behavior remains in handleDecodedNotification.
func (s *session) handleNotification(method string, params json.RawMessage) {
	now := time.Now().UTC()
	// One atomic store per notification; the stall ticker reads this value.
	s.lastActivity.Store(now.UnixNano())
	if s.handleProjectedNotification(method, params, now) {
		return
	}
	s.handleDecodedNotification(method, params, now)
}

// handleProjectedNotification decodes notification families added by the
// 0.149.1 fidelity surface. Returning true means the method was consumed.
func (s *session) handleProjectedNotification(method string, params json.RawMessage, now time.Time) bool {
	emit := func(typ event.Type, payload event.CodexPayload) {
		s.emit(event.Event{Type: typ, SessionID: s.localID, Timestamp: now, AgentSessionID: s.agentID, Codex: &payload})
	}
	switch method {
	case "item/reasoning/summaryPartAdded":
		var p struct {
			ItemID       string `json:"itemId"`
			SummaryIndex int    `json:"summaryIndex"`
		}
		if json.Unmarshal(params, &p) == nil {
			emit(event.TypeCodexProgress, event.CodexPayload{Key: "reasoning:" + p.ItemID, Kind: "reasoning_summary", Status: "running", Title: "Reasoning", Text: "Reasoning summary updated"})
		}
		return true
	case "item/reasoning/summaryTextDelta":
		var p struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil && p.Delta != "" {
			s.emit(event.Event{Type: event.TypeThoughtChunk, SessionID: s.localID, Timestamp: now, Text: p.Delta, ToolID: p.ItemID, AgentSessionID: s.agentID})
		}
		return true
	case "item/mcpToolCall/progress":
		var p struct {
			ItemID  string `json:"itemId"`
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &p) == nil && p.Message != "" {
			emit(event.TypeCodexProgress, event.CodexPayload{Key: "item:" + p.ItemID, Kind: "mcp_progress", Status: "running", Title: "Tool progress", Text: p.Message})
		}
		return true
	case "item/fileChange/outputDelta":
		var p struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil && p.Delta != "" {
			s.emit(event.Event{Type: event.TypeToolUpdate, SessionID: s.localID, Timestamp: now, ToolID: p.ItemID, Text: p.Delta, Status: "running", AgentSessionID: s.agentID})
		}
		return true
	case "item/fileChange/patchUpdated":
		var p struct {
			ItemID  string `json:"itemId"`
			Changes []struct {
				Path string `json:"path"`
			} `json:"changes"`
		}
		if json.Unmarshal(params, &p) == nil {
			paths := make([]string, 0, len(p.Changes))
			for _, change := range p.Changes {
				if base := filepath.Base(change.Path); base != "." {
					paths = append(paths, base)
				}
			}
			emit(event.TypeCodexProgress, event.CodexPayload{Key: "item:" + p.ItemID, Kind: "patch_updated", Status: "running", Title: "Patch updated", Text: strings.Join(paths, ", "), Values: paths})
		}
		return true
	case "item/commandExecution/terminalInteraction":
		var p struct {
			ItemID    string `json:"itemId"`
			ProcessID string `json:"processId"`
			Stdin     string `json:"stdin"`
		}
		if json.Unmarshal(params, &p) == nil {
			emit(event.TypeCodexTerminalInteraction, event.CodexPayload{Key: "item:" + p.ItemID, Kind: "terminal_interaction", Status: "running", Title: "Terminal interaction", Text: "Input sent to process " + p.ProcessID})
		}
		return true
	case "item/autoApprovalReview/started":
		var p struct {
			ReviewID     string `json:"reviewId"`
			TargetItemID string `json:"targetItemId"`
		}
		if json.Unmarshal(params, &p) == nil {
			emit(event.TypeCodexProgress, event.CodexPayload{Key: "review:" + p.ReviewID, Kind: "guardian_review", Status: "running", Title: "Guardian review", Text: "Reviewing " + p.TargetItemID})
		}
		return true
	case "item/autoApprovalReview/completed":
		var p struct {
			ReviewID       string `json:"reviewId"`
			DecisionSource string `json:"decisionSource"`
			Review         struct {
				Status string `json:"status"`
			}
		}
		if json.Unmarshal(params, &p) == nil {
			emit(event.TypeCodexProgress, event.CodexPayload{Key: "review:" + p.ReviewID, Kind: "guardian_review", Status: "completed", Title: "Guardian review", Text: firstNonEmpty(p.Review.Status, p.DecisionSource), Resolved: true})
			if p.Review.Status == "denied" {
				s.mu.Lock()
				generation := s.engineGeneration
				s.mu.Unlock()
				s.trackGuardianDenial(p.ReviewID, generation, params)
			}
		}
		return true
	case "model/rerouted":
		var p struct {
			TurnID    string `json:"turnId"`
			FromModel string `json:"fromModel"`
			ToModel   string `json:"toModel"`
			Reason    string `json:"reason"`
		}
		if json.Unmarshal(params, &p) == nil {
			emit(event.TypeCodexModelReroute, event.CodexPayload{Key: "turn:" + p.TurnID + ":model", Kind: "model_reroute", Status: "completed", Title: "Model rerouted", FromModel: p.FromModel, ToModel: p.ToModel, Reason: p.Reason, Text: p.FromModel + " → " + p.ToModel})
		}
		return true
	case "model/verification":
		var p struct {
			TurnID        string   `json:"turnId"`
			Verifications []string `json:"verifications"`
		}
		if json.Unmarshal(params, &p) == nil {
			emit(event.TypeCodexModelVerification, event.CodexPayload{Key: "turn:" + p.TurnID + ":verification", Kind: "model_verification", Status: "completed", Title: "Model verification", Text: strings.Join(p.Verifications, ", "), Values: p.Verifications, Resolved: true})
		}
		return true
	case "model/safetyBuffering/updated":
		var p struct {
			TurnID      string   `json:"turnId"`
			Model       string   `json:"model"`
			FasterModel string   `json:"fasterModel"`
			UseCases    []string `json:"useCases"`
			Reasons     []string `json:"reasons"`
			Show        bool     `json:"showBufferingUi"`
		}
		if json.Unmarshal(params, &p) == nil {
			status := "completed"
			if p.Show {
				status = "running"
			}
			emit(event.TypeCodexProgress, event.CodexPayload{Key: "turn:" + p.TurnID + ":safety", Kind: "safety_buffering", Status: status, Title: "Safety buffering", Text: strings.Join(p.Reasons, ", "), Values: p.UseCases, ToModel: p.FasterModel, Resolved: !p.Show})
		}
		return true
	}
	return false
}
