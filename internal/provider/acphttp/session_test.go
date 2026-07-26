package acphttp

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func newTestSession(t *testing.T) *session {
	t.Helper()
	p := &Provider{
		spec: Spec{
			ID: provider.IDGoose,
			StaticModes: []event.SessionMode{
				{ID: "auto", Name: "Auto"},
			},
			DefaultModeID: "auto",
		},
		cfg:   Config{},
		httpc: nil,
		log:   slog.Default(),
	}
	s := &session{
		p:            p,
		cfg:          Config{},
		localID:      "test-session",
		agentID:      "agent-1",
		cwd:          "/tmp",
		log:          slog.Default(),
		events:       make(chan event.Event, 64),
		done:         make(chan struct{}),
		pendingPerms: make(map[string]string),
		staticModes:  p.spec.StaticModes,
	}
	return s
}

func recvEvent(t *testing.T, ch <-chan event.Event) event.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return event.Event{}
	}
}

func recvNone(t *testing.T, ch <-chan event.Event, d time.Duration) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(d):
	}
}

// ─── handleUpdate tests ────────────────────────────────────────────────

func TestHandleUpdate_AgentMessageChunk(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello world"}}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeAssistantChunk {
		t.Fatalf("want AssistantChunk, got %s", ev.Type)
	}
	if ev.Text != "hello world" {
		t.Fatalf("want text='hello world', got %q", ev.Text)
	}
	if ev.SessionID != s.localID {
		t.Fatalf("want SessionID=%q, got %q", s.localID, ev.SessionID)
	}
}

func TestHandleUpdate_AgentThoughtChunk(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking..."}}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeThoughtChunk {
		t.Fatalf("want ThoughtChunk, got %s", ev.Type)
	}
	if ev.Text != "thinking..." {
		t.Fatalf("want text='thinking...', got %q", ev.Text)
	}
}

func TestHandleUpdate_UserMessageChunkIsIgnored(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"user said"}}`)
	s.handleUpdate(raw)

	recvNone(t, s.events, 50*time.Millisecond)
}

func TestHandleUpdate_ToolCall(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{
		"sessionUpdate":"tool_call",
		"title":"Read file",
		"toolCallId":"tc-1",
		"kind":"read",
		"status":"running",
		"content":[]
	}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeToolCall {
		t.Fatalf("want ToolCall, got %s", ev.Type)
	}
	if ev.ToolID != "tc-1" {
		t.Fatalf("want ToolID=tc-1, got %q", ev.ToolID)
	}
	if ev.ToolName != "Read file" {
		t.Fatalf("want ToolName='Read file', got %q", ev.ToolName)
	}
	if ev.Status != "running" {
		t.Fatalf("want Status=running, got %q", ev.Status)
	}
}

func TestHandleUpdate_ToolCallUpdate(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{
		"sessionUpdate":"tool_call_update",
		"toolCallId":"tc-1",
		"title":"Read file",
		"status":"completed"
	}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeToolUpdate {
		t.Fatalf("want ToolUpdate, got %s", ev.Type)
	}
	if ev.ToolID != "tc-1" {
		t.Fatalf("want ToolID=tc-1, got %q", ev.ToolID)
	}
	if ev.Status != "completed" {
		t.Fatalf("want Status=completed, got %q", ev.Status)
	}
}

func TestHandleUpdate_AvailableCommands(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{
		"sessionUpdate":"available_commands",
		"availableCommands":[
			{"name":"compact","description":"Summarize the session"},
			{"name":"status","description":"Show session status"}
		]
	}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeAvailableCommands {
		t.Fatalf("want TypeAvailableCommands, got %s", ev.Type)
	}
	if len(ev.Commands) != 2 {
		t.Fatalf("want 2 commands, got %d", len(ev.Commands))
	}
	if ev.Commands[0].Name != "compact" {
		t.Fatalf("want command=compact, got %q", ev.Commands[0].Name)
	}
}

func TestHandleUpdate_Plan(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{
		"sessionUpdate":"plan",
		"entries":[
			{"content":"Step 1","status":"completed","priority":"high"},
			{"content":"Step 2","status":"in_progress","priority":"medium"}
		]
	}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePlan {
		t.Fatalf("want TypePlan, got %s", ev.Type)
	}
	if len(ev.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ev.Entries))
	}
	if ev.Entries[0].Content != "Step 1" {
		t.Fatalf("want entry content='Step 1', got %q", ev.Entries[0].Content)
	}
	if ev.Entries[0].Status != "completed" {
		t.Fatalf("want status=completed, got %q", ev.Entries[0].Status)
	}
}

func TestHandleUpdate_PlanRemoved(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"plan_removed"}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePlan {
		t.Fatalf("want TypePlan, got %s", ev.Type)
	}
	if len(ev.Entries) != 0 {
		t.Fatalf("want 0 entries for removed plan, got %d", len(ev.Entries))
	}
}

func TestHandleUpdate_UsageUpdate(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"usage_update","used":100,"size":1000}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeUsage {
		t.Fatalf("want TypeUsage, got %s", ev.Type)
	}
	if ev.Usage == nil {
		t.Fatal("want non-nil Usage")
	}
	if ev.Usage.Used != 100 || ev.Usage.Size != 1000 {
		t.Fatalf("want Used=100, Size=1000, got Used=%d, Size=%d", ev.Usage.Used, ev.Usage.Size)
	}
}

func TestHandleUpdate_CurrentModeUpdate(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"current_mode_update","currentModeId":"approve"}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeMode {
		t.Fatalf("want TypeMode, got %s", ev.Type)
	}
	if ev.CurrentModeID != "approve" {
		t.Fatalf("want CurrentModeID=approve, got %q", ev.CurrentModeID)
	}
}

func TestHandleUpdate_ConfigOptionUpdate(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{
		"sessionUpdate":"config_option_update",
		"configOptions":[
			{
				"type":"select",
				"id":"model",
				"name":"Model",
				"currentValue":"gpt-4",
				"options":[
					{"value":"gpt-4","name":"GPT-4"},
					{"value":"gpt-3.5","name":"GPT-3.5"}
				]
			}
		]
	}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeSessionConfig {
		t.Fatalf("want TypeSessionConfig, got %s", ev.Type)
	}
	if len(ev.ConfigOptions) != 1 {
		t.Fatalf("want 1 config option, got %d", len(ev.ConfigOptions))
	}
	co := ev.ConfigOptions[0]
	if co.ID != "model" {
		t.Fatalf("want option id=model, got %q", co.ID)
	}
	if co.CurrentValue != "gpt-4" {
		t.Fatalf("want CurrentValue=gpt-4, got %q", co.CurrentValue)
	}
}

func TestHandleUpdate_UnknownType(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"unknown_type","foo":"bar"}`)
	s.handleUpdate(raw)

	recvNone(t, s.events, 50*time.Millisecond)
}

// ─── handlePermissionRequest tests ─────────────────────────────────────

func TestHandlePermissionRequest(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{
		"requestId":"req-1",
		"sessionId":"agent-1",
		"options":[
			{"optionId":"allow_once","name":"Allow Once","kind":"allow_once"},
			{"optionId":"deny","name":"Deny","kind":"deny"}
		],
		"toolCall":{
			"toolCallId":"tc-1",
			"title":"Run command",
			"content":[],
			"kind":"run",
			"status":"awaiting_permission"
		}
	}`)
	s.handlePermissionRequest(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePermission {
		t.Fatalf("want TypePermission, got %s", ev.Type)
	}
	if ev.PermissionID == "" {
		t.Fatal("want non-empty PermissionID")
	}
	if len(ev.Options) != 2 {
		t.Fatalf("want 2 options, got %d", len(ev.Options))
	}
	if ev.ToolID != "tc-1" {
		t.Fatalf("want ToolID=tc-1, got %q", ev.ToolID)
	}
	if ev.ToolName != "Run command" {
		t.Fatalf("want ToolName='Run command', got %q", ev.ToolName)
	}
	if ev.Status != "pending" {
		t.Fatalf("want Status=pending, got %q", ev.Status)
	}

	s.pendingPermsMu.Lock()
	reqID, ok := s.pendingPerms[ev.PermissionID]
	s.pendingPermsMu.Unlock()
	if !ok {
		t.Fatal("permission ID should be tracked in pendingPerms")
	}
	if reqID != "req-1" {
		t.Fatalf("want requestId=req-1, got %q", reqID)
	}
}

func TestHandlePermissionRequest_AlwaysApprove(t *testing.T) {
	s := newTestSession(t)
	s.cfg.AlwaysApprove = true

	raw := json.RawMessage(`{
		"requestId":"req-1",
		"sessionId":"agent-1",
		"options":[
			{"optionId":"allow_once","name":"Allow Once","kind":"allow_once"},
			{"optionId":"deny","name":"Deny","kind":"deny"}
		],
		"toolCall":{
			"toolCallId":"tc-1",
			"title":"Run command",
			"content":[]
		}
	}`)
	s.handlePermissionRequest(raw)

	recvNone(t, s.events, 50*time.Millisecond)
}

// ─── helper function tests ─────────────────────────────────────────────

func TestContentBlockText(t *testing.T) {
	tests := []struct {
		cb   acp.ContentBlock
		want string
	}{
		{acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: "hello"}}, "hello"},
		{acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: "  spaced  "}}, "spaced"},
		{acp.ContentBlock{}, ""},
		{acp.ContentBlock{Image: &acp.ContentBlockImage{Type: "image", Data: "base64"}}, ""},
	}
	for _, tc := range tests {
		got := contentBlockText(tc.cb)
		if got != tc.want {
			t.Fatalf("contentBlockText(%+v) = %q, want %q", tc.cb, got, tc.want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"", "b", "c"}, "b"},
		{[]string{"a", "b"}, "a"},
		{[]string{"", "", ""}, ""},
		{[]string{"  ", "trimmed"}, "trimmed"},
	}
	for _, tc := range tests {
		got := firstNonEmpty(tc.args...)
		if got != tc.want {
			t.Fatalf("firstNonEmpty(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestSummarizeTCContent(t *testing.T) {
	tests := []struct {
		name      string
		content   []acp.ToolCallContent
		rawInput  any
		rawOutput any
		max       int
		want      string
	}{
		{
			name: "text content only",
			content: []acp.ToolCallContent{
				{Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "output text"}},
				}},
			},
			want: "output text",
		},
		{
			name:     "raw input only",
			content:  nil,
			rawInput: `{"key":"value"}`,
			want:     `{"key":"value"}`,
		},
		{
			name:      "raw output truncated",
			content:   nil,
			rawOutput: `this is a very long string that should be truncated at the specified max length`,
			max:       20,
			want:      "this is a very long …",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.max
			if m == 0 {
				m = 400
			}
			got := summarizeTCContent(tc.content, tc.rawInput, tc.rawOutput, m)
			if got != tc.want {
				t.Fatalf("summarizeTCContent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapPlanEntries(t *testing.T) {
	entries := []acp.PlanEntry{
		{Content: "step 1", Status: "completed", Priority: "high"},
		{Content: "step 2", Status: "in_progress", Priority: "medium"},
	}
	got := mapPlanEntries(entries)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].Content != "step 1" || got[0].Status != "completed" || got[0].Priority != "high" {
		t.Fatalf("entry 0: %+v", got[0])
	}
	if got[1].Content != "step 2" || got[1].Status != "in_progress" || got[1].Priority != "medium" {
		t.Fatalf("entry 1: %+v", got[1])
	}
}

func TestBuildMcpServers(t *testing.T) {
	cfgs := []McpServer{
		{Name: "srv1", Transport: "http", URL: "http://localhost:8080", Headers: map[string]string{"X-Auth": "token"}},
		{Name: "srv2", Transport: "sse", URL: "http://localhost:8081"},
	}
	got := buildMcpServers(cfgs)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Http == nil {
		t.Fatal("srv1 should be Http")
	}
	if got[0].Http.Name != "srv1" {
		t.Fatalf("want name=srv1, got %q", got[0].Http.Name)
	}
	if got[0].Http.Url != "http://localhost:8080" {
		t.Fatalf("want url=http://localhost:8080, got %q", got[0].Http.Url)
	}
	if len(got[0].Http.Headers) != 1 {
		t.Fatalf("want 1 header, got %d", len(got[0].Http.Headers))
	}
	if got[1].Sse == nil {
		t.Fatal("srv2 should be Sse")
	}
	if got[1].Sse.Name != "srv2" {
		t.Fatalf("want name=srv2, got %q", got[1].Sse.Name)
	}
}

func TestConvertHeaders(t *testing.T) {
	h := map[string]string{"X-Auth": "token123", "X-Custom": "value"}
	got := convertHeaders(h)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	m := make(map[string]string)
	for _, v := range got {
		m[v.Name] = v.Value
	}
	if m["X-Auth"] != "token123" {
		t.Fatalf("want X-Auth=token123, got %q", m["X-Auth"])
	}
	if m["X-Custom"] != "value" {
		t.Fatalf("want X-Custom=value, got %q", m["X-Custom"])
	}
}
