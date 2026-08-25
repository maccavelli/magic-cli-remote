package codex

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// item types that are genuine agent actions and should render as tool cards.
// MADR 0035 D1: a single source of truth, consulted by both item/started and
// item/completed so the two handlers cannot disagree about which items
// produce cards.
var itemsRenderedAsTools = map[string]struct{}{
	"commandExecution": {},
	"fileChange":       {},
	"mcpToolCall":      {},
	"webSearch":        {},
	"dynamicToolCall":  {},
	"imageGeneration":  {},
	"imageView":        {},
	"hookPrompt":       {},
	// collabAgentToolCall and subAgentActivity are deliberately absent: they
	// describe sub-agents, which are panel state rather than transcript items
	// (MADR 0051 D8/D10). They are routed by noteSubagentItem before this
	// allowlist is consulted.
}

var knownNonToolItems = map[string]struct{}{
	"userMessage": {}, "agentMessage": {}, "plan": {}, "reasoning": {},
	"collabAgentToolCall": {}, "subAgentActivity": {}, "contextCompaction": {},
	"enteredReviewMode": {}, "exitedReviewMode": {}, "sleep": {},
}

func (s *session) markItemStarted(id string) bool {
	if id == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startedItems == nil {
		s.startedItems = make(map[string]struct{})
	}
	if _, exists := s.startedItems[id]; exists {
		return false
	}
	s.startedItems[id] = struct{}{}
	return true
}

func (s *session) markItemCompleted(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	if s.completedItems == nil {
		s.completedItems = make(map[string]struct{})
	}
	s.completedItems[id] = struct{}{}
	s.mu.Unlock()
}

func shouldSurfaceUnsupportedItem(itemType string, item json.RawMessage) bool {
	if itemType == "" {
		return false
	}
	if _, ok := knownNonToolItems[itemType]; ok {
		return false
	}
	if _, ok := itemsRenderedAsTools[itemType]; ok {
		return false
	}
	status, _ := itemStatus(item)
	return status != ""
}

func (s *session) emitUnsupportedItem(itemID, itemType string, item json.RawMessage, now time.Time) {
	status, _ := itemStatus(item)
	s.emit(event.Event{Type: event.TypeCodexUnsupportedItem, SessionID: s.localID, Timestamp: now,
		AgentSessionID: s.agentID, Codex: &event.CodexPayload{Key: "item:" + itemID,
			Kind: "unsupported_item", Status: codexToolStatus(status), Title: "Unsupported Codex activity",
			Text: "Codex completed an unsupported " + itemType + " item."}})
}

// itemAsNotice renders a one-line system notice for state changes worth
// recording but not worth a card. Returns the event and ok=true when the
// type is handled.
func itemAsNotice(itemType string) (event.Event, bool) {
	switch itemType {
	case "contextCompaction":
		return event.Event{
			Type:      event.TypeNotice,
			Text:      "Context compacted",
			Timestamp: time.Now().UTC(),
		}, true
	case "enteredReviewMode":
		return event.Event{
			Type:      event.TypeNotice,
			Text:      "Entered review mode",
			Timestamp: time.Now().UTC(),
		}, true
	case "exitedReviewMode":
		return event.Event{
			Type:      event.TypeNotice,
			Text:      "Exited review mode",
			Timestamp: time.Now().UTC(),
		}, true
	}
	return event.Event{}, false
}

// codexToolStatus maps the codex item-status vocabulary to the daemon's
// tool-status vocabulary. The mobile reducer (chat_models.dart) treats
// `running` and `pending` as the "still working" set; `failed` and
// `completed` are terminal. `declined` (commandExecution) folds to
// `failed` — a denial is a kind of failure for the agent's plan.
func codexToolStatus(s string) string {
	switch s {
	case "inProgress", "running":
		return "running"
	case "completed":
		return "completed"
	case "failed", "declined":
		return "failed"
	case "pending":
		return "pending"
	}
	return s
}

// codexToolKindForItem returns the daemon's tool-kind vocabulary for a
// given codex item type, or "" if the type is not a tool. Used by the
// mobile classifyTool to group actions ("Ran N commands", etc.).
func codexToolKindForItem(itemType string) string {
	switch itemType {
	case "commandExecution":
		return "execute"
	case "fileChange":
		return "edit"
	case "webSearch":
		return "fetch"
	case "mcpToolCall", "dynamicToolCall", "imageGeneration", "imageView", "hookPrompt":
		return "other"
	}
	return ""
}

// extractItemID reads the id field out of a v2 ThreadItem payload, returning
// the id and the v2 item type ("type" field on item). v1 callers can fall
// back to a separate id field; v2 only carries id on the item.
func extractItemID(item json.RawMessage) (string, string) {
	var probe struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(item, &probe); err != nil {
		return "", ""
	}
	return probe.ID, probe.Type
}

// codexPlanStatus normalizes the v2 TurnPlanStepStatus to the daemon's
// plan vocabulary. Unknown values fall through as "pending" so a future
// status name does not break the panel; the schema's enum is
// pending|inProgress|completed, so a fall-through is a quiet new case.
func codexPlanStatus(s string) string {
	switch s {
	case "pending":
		return "pending"
	case "inProgress":
		return "in_progress"
	case "completed":
		return "completed"
	}
	return "pending"
}

// codexPlanEntries converts codex plan steps into the daemon's PlanEntry
// shape. codex does not put priority on the wire, so the daemon default
// "medium" is used (acpcommon.PlanEntries does the same).
func codexPlanEntries(in []struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}) []event.PlanEntry {
	out := make([]event.PlanEntry, 0, len(in))
	for _, s := range in {
		out = append(out, event.PlanEntry{
			Content:  s.Step,
			Status:   codexPlanStatus(s.Status),
			Priority: "medium",
		})
	}
	return out
}

// emitToolStarted produces a tool_call event for the given item type. It is
// only called for types in itemsRenderedAsTools. The detail and tool name
// differ per type; the common fields (id, kind, status="running", session)
// are filled in here.
func (s *session) emitToolStarted(itemType, itemID string, item json.RawMessage, now time.Time) {
	name, detail := toolCardDetail(itemType, item)
	status := "running"
	// Some item types carry an initial status on the item itself (e.g.,
	// commandExecution: "inProgress"). Keep that in case the mobile reducer
	// wants the more specific vocabulary later, but the daemon contract is
	// `running`.
	if probe, ok := itemStatus(item); ok && probe != "" {
		status = codexToolStatus(probe)
	}
	s.emit(event.Event{
		Type:           event.TypeToolCall,
		SessionID:      s.localID,
		Timestamp:      now,
		ToolID:         itemID,
		ToolName:       name,
		ToolKind:       codexToolKindForItem(itemType),
		Text:           detail,
		Status:         status,
		AgentSessionID: s.agentID,
	})
}

// toolCardDetail returns the human-facing card title and detail body for a
// tool item. Centralised so the per-type field extraction does not leak
// into the dispatch.
func toolCardDetail(itemType string, item json.RawMessage) (name, detail string) {
	switch itemType {
	case "commandExecution":
		var p struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(item, &p)
		return "command", p.Command
	case "fileChange":
		var p struct {
			Changes []struct {
				Path string `json:"path"`
			} `json:"changes"`
		}
		_ = json.Unmarshal(item, &p)
		// fileChange may carry multiple changes in v2; show the first path
		// and a count if there are more.
		if len(p.Changes) == 0 {
			return "file", ""
		}
		if len(p.Changes) == 1 {
			return "file", p.Changes[0].Path
		}
		return "file", p.Changes[0].Path + " (+" + itoa(len(p.Changes)-1) + " more)"
	case "mcpToolCall":
		var p struct {
			Server string `json:"server"`
			Tool   string `json:"tool"`
		}
		_ = json.Unmarshal(item, &p)
		return firstOr(p.Server+"/"+p.Tool, "mcp"), ""
	case "webSearch":
		return "web search", ""
	case "dynamicToolCall":
		return "tool", ""
	case "imageGeneration":
		return "image generation", ""
	case "imageView":
		return "image view", ""
	case "hookPrompt":
		return "hook", ""
	}
	return itemType, ""
}

// itemStatus reads the `status` field on a v2 ThreadItem if present, for
// items that carry one (commandExecution, fileChange, mcpToolCall, ...).
func itemStatus(item json.RawMessage) (string, bool) {
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(item, &probe); err != nil || probe.Status == "" {
		return "", false
	}
	return probe.Status, true
}

func toolCompletionDetail(itemType string, item json.RawMessage) string {
	switch itemType {
	case "commandExecution":
		var body struct {
			ExitCode   *int   `json:"exitCode"`
			DurationMS *int64 `json:"durationMs"`
		}
		if json.Unmarshal(item, &body) == nil && body.ExitCode != nil {
			text := "Exit code " + itoa(*body.ExitCode)
			if body.DurationMS != nil {
				text += " · " + itoa(int(*body.DurationMS)) + " ms"
			}
			return text
		}
	case "fileChange":
		var body struct {
			Changes []struct {
				Path string `json:"path"`
			} `json:"changes"`
		}
		if json.Unmarshal(item, &body) == nil && len(body.Changes) > 0 {
			return filepath.Base(body.Changes[0].Path)
		}
	case "mcpToolCall", "dynamicToolCall":
		var body struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(item, &body) == nil && body.Error != nil {
			return body.Error.Message
		}
	}
	return ""
}

func itoa(n int) string {
	// small, allocation-light integer -> string for the "more" count.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
