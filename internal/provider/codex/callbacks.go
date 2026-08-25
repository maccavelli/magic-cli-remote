package codex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

type callbackKind string

const (
	callbackCommand    callbackKind = "command"
	callbackFile       callbackKind = "file"
	callbackGranular   callbackKind = "granular_permission"
	callbackMCP        callbackKind = "mcp_elicitation"
	callbackLegacyFile callbackKind = "legacy_apply_patch"
	callbackLegacyExec callbackKind = "legacy_exec_command"
)

// pendingCallback retains the exact response dialect and engine generation
// for one Codex server request. Granular grants deliberately do not share a
// response builder with decision callbacks.
type pendingCallback struct {
	rpcID            json.RawMessage
	kind             callbackKind
	requestedGrant   map[string]any
	allowedDecisions []string
	tool             string
	detail           string
	generation       int
}

type pendingQuestion struct {
	rpcID      json.RawMessage
	fieldIDs   []string
	secretIDs  map[string]struct{}
	generation int
}

// LogValue ensures callback metadata remains useful without ever exposing a
// secret answer. Values are intentionally absent from pendingQuestion itself.
func (q pendingQuestion) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("field_count", len(q.fieldIDs)),
		slog.Int("secret_field_count", len(q.secretIDs)),
		slog.Int("engine_generation", q.generation),
	)
}

type decisionResponse struct {
	Decision string `json:"decision"`
}

type granularPermissionResponse struct {
	Permissions      map[string]any `json:"permissions"`
	Scope            string         `json:"scope"`
	StrictAutoReview *bool          `json:"strictAutoReview,omitempty"`
}

type mcpElicitationResponse struct {
	Action  string `json:"action"`
	Content any    `json:"content,omitempty"`
}

type toolRequestUserInputResponse struct {
	Answers map[string]toolRequestUserInputAnswer `json:"answers"`
}

type toolRequestUserInputAnswer struct {
	Answers []string `json:"answers"`
}

func parseCallback(method string, id, params json.RawMessage, generation int) (pendingCallback, error) {
	cb := pendingCallback{rpcID: slices.Clone(id), generation: generation}
	var common struct {
		Command            any               `json:"command"`
		Reason             string            `json:"reason"`
		Description        string            `json:"description"`
		FilePath           string            `json:"filePath"`
		Path               string            `json:"path"`
		GrantRoot          string            `json:"grantRoot"`
		ServerName         string            `json:"serverName"`
		AvailableDecisions []json.RawMessage `json:"availableDecisions"`
		Permissions        map[string]any    `json:"permissions"`
	}
	if err := json.Unmarshal(params, &common); err != nil {
		return pendingCallback{}, fmt.Errorf("decode %s: %w", method, err)
	}
	switch method {
	case "item/commandExecution/requestApproval":
		cb.kind, cb.tool = callbackCommand, "command"
		cb.detail = firstNonEmpty(displayCommand(common.Command), common.Reason)
		cb.allowedDecisions = simpleDecisionIDs(common.AvailableDecisions)
		if len(cb.allowedDecisions) == 0 {
			cb.allowedDecisions = []string{"accept", "acceptForSession", "decline", "cancel"}
		}
	case "item/fileChange/requestApproval":
		cb.kind, cb.tool = callbackFile, "file"
		cb.detail = firstNonEmpty(common.FilePath, common.Path, common.GrantRoot, common.Reason)
		cb.allowedDecisions = []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/permissions/requestApproval":
		cb.kind, cb.tool = callbackGranular, "permission"
		cb.detail = firstNonEmpty(common.Reason, common.Description, "Codex requests additional permissions")
		cb.requestedGrant = cloneJSONMap(common.Permissions)
		cb.allowedDecisions = []string{"accept", "acceptForSession", "acceptStrict", "acceptStrictForSession", "decline", "cancel"}
	case "mcpServer/elicitation/request":
		cb.kind, cb.tool = callbackMCP, "mcp"
		cb.detail = firstNonEmpty(common.ServerName, "MCP server requests input")
		cb.allowedDecisions = []string{"accept", "decline", "cancel"}
	case "applyPatchApproval":
		cb.kind, cb.tool = callbackLegacyFile, "file"
		cb.detail = firstNonEmpty(common.Reason, common.GrantRoot, "Codex requests a patch")
		cb.allowedDecisions = []string{"approved", "approved_for_session", "decline", "cancel"}
	case "execCommandApproval":
		cb.kind, cb.tool = callbackLegacyExec, "command"
		cb.detail = firstNonEmpty(displayCommand(common.Command), common.Reason, "Codex requests a command")
		cb.allowedDecisions = []string{"approved", "approved_for_session", "decline", "cancel"}
	default:
		return pendingCallback{}, fmt.Errorf("unsupported callback method %q", method)
	}
	return cb, nil
}

func simpleDecisionIDs(raw []json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var id string
		if json.Unmarshal(item, &id) == nil && id != "" {
			out = append(out, id)
		}
	}
	return out
}

func displayCommand(command any) string {
	switch value := command.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, part := range value {
			if text, ok := part.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func callbackOptions(cb pendingCallback) []event.PermissionOption {
	options := make([]event.PermissionOption, 0, len(cb.allowedDecisions))
	for _, id := range cb.allowedDecisions {
		option := event.PermissionOption{OptionID: id, Name: id}
		switch id {
		case "accept", "approved":
			option.Name, option.Kind = "Allow once", "allow_once"
		case "acceptForSession", "approved_for_session":
			option.Name, option.Kind = "Allow for session", "allow_always"
		case "acceptStrict":
			option.Name, option.Kind = "Allow once with strict review", "allow_once"
		case "acceptStrictForSession":
			option.Name, option.Kind = "Allow for session with strict review", "allow_always"
		case "decline":
			option.Name, option.Kind = "Deny", "deny"
		case "cancel":
			option.Name, option.Kind = "Cancel", "cancel"
		}
		options = append(options, option)
	}
	return options
}

func callbackResponse(cb pendingCallback, optionID string, cancelled bool) (any, bool, error) {
	if cb.kind == callbackGranular {
		return granularResponse(cb, optionID, cancelled)
	}
	if cb.kind == callbackMCP {
		action := optionID
		if cancelled {
			action = "cancel"
		}
		if !slices.Contains(cb.allowedDecisions, action) {
			return nil, false, fmt.Errorf("unsupported MCP action %q", action)
		}
		return mcpElicitationResponse{Action: action}, action == "cancel", nil
	}
	if cancelled {
		return decisionResponse{Decision: callbackCancelDecision(cb.kind)}, true, nil
	}
	decision := optionID
	if !slices.Contains(cb.allowedDecisions, decision) {
		return nil, false, fmt.Errorf("unsupported %s decision %q", cb.kind, decision)
	}
	if cb.kind == callbackLegacyFile || cb.kind == callbackLegacyExec {
		switch decision {
		case "decline":
			decision = "abort"
		case "cancel":
			decision = "abort"
		}
	}
	return decisionResponse{Decision: decision}, cancelled || decision == "cancel" || decision == "abort", nil
}

func granularResponse(cb pendingCallback, optionID string, cancelled bool) (granularPermissionResponse, bool, error) {
	response := granularPermissionResponse{Permissions: map[string]any{}, Scope: "turn"}
	if cancelled || optionID == "cancel" {
		return response, true, nil
	}
	if optionID == "decline" {
		return response, false, nil
	}
	if !slices.Contains(cb.allowedDecisions, optionID) {
		return granularPermissionResponse{}, false, fmt.Errorf("unsupported granular permission choice %q", optionID)
	}
	response.Permissions = cloneJSONMap(cb.requestedGrant)
	strict := false
	switch optionID {
	case "acceptForSession":
		response.Scope = "session"
	case "acceptStrict":
		strict = true
	case "acceptStrictForSession":
		strict = true
		response.Scope = "session"
	}
	response.StrictAutoReview = &strict
	return response, false, nil
}

func callbackCancelDecision(kind callbackKind) string {
	if kind == callbackLegacyFile || kind == callbackLegacyExec {
		return "abort"
	}
	return "cancel"
}

func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(in)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func questionResponse(p pendingQuestion, answers provider.QuestionAnswers) (toolRequestUserInputResponse, error) {
	if len(answers) != len(p.fieldIDs) {
		return toolRequestUserInputResponse{}, fmt.Errorf("answer ids do not match question fields")
	}
	out := toolRequestUserInputResponse{Answers: make(map[string]toolRequestUserInputAnswer, len(answers))}
	for _, id := range p.fieldIDs {
		values, ok := answers[id]
		if !ok {
			return toolRequestUserInputResponse{}, fmt.Errorf("missing answer for question field %q", id)
		}
		out.Answers[id] = toolRequestUserInputAnswer{Answers: slices.Clone(values)}
	}
	return out, nil
}

func clearSecretAnswers(answers provider.QuestionAnswers, secretIDs map[string]struct{}) {
	for id := range secretIDs {
		for i := range answers[id] {
			answers[id][i] = ""
		}
		answers[id] = nil
	}
}

func (s *session) noteAnsweredQuestionLocked(questionID string) {
	if s.answeredQuestions == nil {
		s.answeredQuestions = make(map[string]struct{})
	}
	if _, exists := s.answeredQuestions[questionID]; exists {
		return
	}
	s.answeredQuestions[questionID] = struct{}{}
	s.answeredQuestionOrder = append(s.answeredQuestionOrder, questionID)
	for len(s.answeredQuestionOrder) > maxAnsweredPerms {
		delete(s.answeredQuestions, s.answeredQuestionOrder[0])
		s.answeredQuestionOrder = s.answeredQuestionOrder[1:]
	}
}

func (s *session) emitQuestionResolved(questionID string, cancelled bool) {
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	s.emit(event.Event{
		Type:           event.TypeQuestionResolved,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		QuestionID:     questionID,
		Status:         status,
		AgentSessionID: s.agentID,
	})
}

func (s *session) resolveServerRequest(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	s.mu.Lock()
	var permissionID, questionID string
	for id, pending := range s.pendingPerms {
		if jsonRawEqual(pending.rpcID, p.RequestID) {
			permissionID = id
			s.dropPendingLocked(id)
			break
		}
	}
	if permissionID == "" {
		for id, pending := range s.pendingQuestions {
			if jsonRawEqual(pending.rpcID, p.RequestID) {
				questionID = id
				delete(s.pendingQuestions, id)
				break
			}
		}
	}
	s.mu.Unlock()
	if permissionID != "" {
		s.emitPermissionResolved(permissionID, false, "", "")
	}
	if questionID != "" {
		s.emitQuestionResolved(questionID, false)
	}
}

func jsonRawEqual(a, b json.RawMessage) bool {
	var av, bv any
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
