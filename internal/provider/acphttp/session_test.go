package acphttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpcommon"
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
		p:                 p,
		cfg:               Config{},
		localID:           "test-session",
		agentID:           "agent-1",
		cwd:               "/tmp",
		log:               slog.Default(),
		events:            make(chan event.Event, 64),
		done:              make(chan struct{}),
		pendingPerms:      make(map[string]json.RawMessage),
		pendingPermTimers: make(map[string]*time.Timer),
		staticModes:       p.spec.StaticModes,
	}
	return s
}

// withFramer installs fr as the provider's live framer (engine up).
func (s *session) withFramer(fr rpcFramer) {
	s.p.mu.Lock()
	s.p.fr = fr
	s.p.mu.Unlock()
}

// fakeFramer is a scripted rpcFramer: requests are recorded and answered from
// respond/errOn by method. A method listed in blockOn waits for release (or
// ctx) before answering, so tests can hold a turn open.
type fakeFramer struct {
	mu                 sync.Mutex
	methods            []string
	params             []any
	notifications      []string
	notificationParams []any
	respond            map[string]json.RawMessage
	errOn              map[string]error
	blockOn            map[string]chan struct{}
}

func (f *fakeFramer) sendNotification(_ context.Context, method string, params any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifications = append(f.notifications, method)
	f.notificationParams = append(f.notificationParams, params)
	return f.errOn[method]
}

func (f *fakeFramer) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	f.methods = append(f.methods, method)
	f.params = append(f.params, params)
	gate := f.blockOn[method]
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errOn[method]; err != nil {
		return nil, err
	}
	if raw, ok := f.respond[method]; ok {
		return raw, nil
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeFramer) sendResponse(_ context.Context, _ json.RawMessage, _ any, _ *rpcErrorBody) error {
	return nil
}

// calls reports how many requests were made for method.
func (f *fakeFramer) calls(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.methods {
		if m == method {
			n++
		}
	}
	return n
}

func (f *fakeFramer) notification(method string) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, got := range f.notifications {
		if got == method {
			return f.notificationParams[i], true
		}
	}
	return nil, false
}

// waitCalls polls until method has been requested n times (turn RPCs run on
// their own goroutine), failing after two seconds.
func waitCalls(t *testing.T, f *fakeFramer, method string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.calls(method) != n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := f.calls(method); got != n {
		t.Fatalf("want %d %s calls, got %d", n, method, got)
	}
}

// drainEvents collects every event currently buffered without blocking.
func drainEvents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// recvType pulls events until one of type ty arrives, failing after a second.
func recvType(t *testing.T, s *session, ty event.Type) event.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == ty {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", ty)
			return event.Event{}
		}
	}
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
		"sessionUpdate":"available_commands_update",
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
	rpcID := json.RawMessage(`"411a1c91-59bf-45fd-8474-563c5e65f04e"`)
	raw := json.RawMessage(`{
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
	s.handlePermissionRequest(rpcID, raw)

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
	gotID, ok := s.pendingPerms[ev.PermissionID]
	s.pendingPermsMu.Unlock()
	if !ok {
		t.Fatal("permission ID should be tracked in pendingPerms")
	}
	if string(gotID) != string(rpcID) {
		t.Fatalf("want rpc id %s, got %s", rpcID, gotID)
	}
}

func TestHandlePermissionRequest_AlwaysApprove(t *testing.T) {
	s := newTestSession(t)
	s.cfg.AlwaysApprove = true
	// No framer installed — autoAllow is a no-op when there is no socket
	// (ErrEngineDown, discarded), but must not emit a phone-facing
	// permission_request.
	rpcID := json.RawMessage(`"req-uuid"`)
	raw := json.RawMessage(`{
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
	s.handlePermissionRequest(rpcID, raw)

	recvNone(t, s.events, 50*time.Millisecond)
}

func TestHandleUpdate_SessionInfoTitle(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"session_info_update","title":"My session"}`)
	s.handleUpdate(raw)

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeSessionTitle {
		t.Fatalf("want TypeSessionTitle, got %s", ev.Type)
	}
	if ev.Title != "My session" {
		t.Fatalf("want title='My session', got %q", ev.Title)
	}
}

// ─── helper function tests ─────────────────────────────────────────────

func TestContentBlockText(t *testing.T) {
	// Chunk text must pass through VERBATIM: streaming fragments carry the
	// message's spaces and newlines at their edges, and trimming each
	// fragment glued the reassembled reply together (the "run-together
	// text" bug). Only non-text blocks yield "".
	tests := []struct {
		cb   acp.ContentBlock
		want string
	}{
		{acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: "hello"}}, "hello"},
		{acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: "  spaced  "}}, "  spaced  "},
		{acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: "\n\n"}}, "\n\n"},
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

func TestPlanEntriesUsesCommonNormalization(t *testing.T) {
	entries := []acp.PlanEntry{
		{Content: "step 1", Status: "completed", Priority: "high"},
		{Content: "step 2", Status: "in_progress", Priority: "medium"},
		{Content: "unknown", Status: "other", Priority: "urgent"},
	}
	got := acpcommon.PlanEntries(entries)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].Content != "step 1" || got[0].Status != "completed" || got[0].Priority != "high" {
		t.Fatalf("entry 0: %+v", got[0])
	}
	if got[1].Content != "step 2" || got[1].Status != "in_progress" || got[1].Priority != "medium" {
		t.Fatalf("entry 1: %+v", got[1])
	}
	if got[2].Status != event.PlanStatusPending || got[2].Priority != event.PlanPriorityMedium {
		t.Fatalf("entry 2 = %+v, want pending/medium", got[2])
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

func TestMcpServersForCapabilitiesReportsUnsupportedTransport(t *testing.T) {
	cfgs := []McpServer{
		{Name: "http-server", Transport: "http", URL: "https://mcp.example.com/http"},
		{Name: "sse-server", Transport: "sse", URL: "https://mcp.example.com/sse"},
	}
	got, skipped := mcpServersForCapabilities(cfgs, acp.AgentCapabilities{
		McpCapabilities: acp.McpCapabilities{Http: true},
	})
	if len(got) != 1 || got[0].Http == nil || got[0].Http.Name != "http-server" {
		t.Fatalf("supported servers = %#v, want HTTP server only", got)
	}
	if len(skipped) != 1 || skipped[0].Name != "sse-server" || skipped[0].Transport != "sse" {
		t.Fatalf("skipped = %#v, want SSE server", skipped)
	}
}

func TestCreateReportsUnsupportedMcpTransportWithoutLeakingHeaders(t *testing.T) {
	s := newTestSession(t)
	s.cfg.McpServers = []McpServer{{
		Name:      "events",
		Transport: "sse",
		URL:       "https://mcp.example.com/sse",
		Headers:   map[string]string{"Authorization": "Bearer secret"},
	}}
	s.p.mu.Lock()
	s.p.agentCaps = acp.AgentCapabilities{McpCapabilities: acp.McpCapabilities{Http: true}}
	s.p.mu.Unlock()
	fr := &fakeFramer{respond: map[string]json.RawMessage{
		"session/new": json.RawMessage(`{"sessionId":"new-agent"}`),
	}}
	s.withFramer(fr)
	if err := s.create(context.Background()); err != nil {
		t.Fatalf("create: %v", err)
	}
	notice := recvType(t, s, event.TypeNotice)
	if !strings.Contains(notice.Text, `"events"`) || !strings.Contains(notice.Text, `"sse"`) {
		t.Fatalf("notice = %q", notice.Text)
	}
	if strings.Contains(notice.Text, "secret") || strings.Contains(notice.Text, "Authorization") {
		t.Fatalf("notice leaked sensitive MCP header: %q", notice.Text)
	}
}

func TestEmitCapabilitiesUsesNegotiatedAgentCapabilities(t *testing.T) {
	s := newTestSession(t)
	s.emitCapabilities(acp.AgentCapabilities{
		LoadSession: true,
		PromptCapabilities: acp.PromptCapabilities{
			Image:           true,
			EmbeddedContext: true,
		},
		SessionCapabilities: acp.SessionCapabilities{
			List:  &acp.SessionListCapabilities{},
			Close: &acp.SessionCloseCapabilities{},
		},
		McpCapabilities: acp.McpCapabilities{Http: true, Acp: true},
	})
	ev := recvEvent(t, s.events)
	if ev.Capabilities == nil {
		t.Fatal("capabilities event missing payload")
	}
	got := ev.Capabilities
	if !got.Image || got.Audio || !got.LoadSession || !got.EmbeddedContext ||
		!got.ListSessions || !got.CloseSession || !got.MCPHTTP || got.MCPSSE || !got.MCPACP {
		t.Fatalf("capabilities = %#v", got)
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

// ─── chunk whitespace (Bug: run-together text) ──────────────────────────

func TestHandleUpdate_ChunksPreserveWhitespace(t *testing.T) {
	// Coalescing off so each chunk arrives as its own event.
	s := newTestSession(t)
	zero := time.Duration(0)
	s.cfg.StreamCoalesce = &zero

	chunks := []string{"Hello", " world", "\n\n", "Next paragraph", " with trailing space "}
	for _, c := range chunks {
		raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":` +
			strconv.Quote(c) + `}}`)
		s.handleUpdate(raw)
		ev := recvEvent(t, s.events)
		if ev.Type != event.TypeAssistantChunk {
			t.Fatalf("want AssistantChunk, got %s", ev.Type)
		}
		if ev.Text != c {
			t.Fatalf("chunk mangled: want %q, got %q", c, ev.Text)
		}
	}
}

func TestHandleUpdate_EmptyChunkIsNoise(t *testing.T) {
	s := newTestSession(t)
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":""}}`)
	s.handleUpdate(raw)
	recvNone(t, s.events, 50*time.Millisecond)
}

// ─── turn lifecycle (Bug: missing user bubble / stuck "running") ────────

func TestPromptEmitsUserMessageAndTurnLifecycle(t *testing.T) {
	s := newTestSession(t)
	fr := &fakeFramer{
		respond: map[string]json.RawMessage{
			"session/prompt": json.RawMessage(`{"stopReason":"end_turn"}`),
		},
	}
	s.withFramer(fr)

	err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hello agent"}})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeUserMessage || ev.Text != "hello agent" {
		t.Fatalf("want user_message 'hello agent', got %s %q", ev.Type, ev.Text)
	}
	ev = recvEvent(t, s.events)
	if ev.Type != event.TypeSessionStatus || ev.Status != "running" {
		t.Fatalf("want status running, got %s %q", ev.Type, ev.Status)
	}
	ev = recvType(t, s, event.TypeTurnComplete)
	if ev.StopReason != "end_turn" {
		t.Fatalf("want stopReason end_turn, got %q", ev.StopReason)
	}
	ev = recvEvent(t, s.events)
	if ev.Type != event.TypeSessionStatus || ev.Status != "idle" {
		t.Fatalf("want status idle, got %s %q", ev.Type, ev.Status)
	}
	if fr.calls("session/prompt") != 1 {
		t.Fatalf("want 1 session/prompt call, got %d", fr.calls("session/prompt"))
	}
}

func TestPromptTurnCompletesOnAnyStopReason(t *testing.T) {
	// Gating turn_complete on "end_turn" left the phone stuck "running"
	// after cancellations and refusals: every stop reason ends the turn.
	tests := []struct {
		wire string
		want string
	}{
		{`{"stopReason":"cancelled"}`, "cancelled"},
		{`{"stopReason":"refusal"}`, "refusal"},
		{`{"stopReason":"max_tokens"}`, "max_tokens"},
		{`{}`, "end_turn"}, // missing reason defaults to end_turn
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			s := newTestSession(t)
			s.withFramer(&fakeFramer{
				respond: map[string]json.RawMessage{"session/prompt": json.RawMessage(tt.wire)},
			})
			if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
				t.Fatalf("Prompt: %v", err)
			}
			ev := recvType(t, s, event.TypeTurnComplete)
			if ev.StopReason != tt.want {
				t.Fatalf("want stopReason %q, got %q", tt.want, ev.StopReason)
			}
		})
	}
}

func TestPromptErrorLandsInTranscript(t *testing.T) {
	s := newTestSession(t)
	s.withFramer(&fakeFramer{
		errOn: map[string]error{"session/prompt": errors.New("quota exceeded: try again at 14:00")},
	})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	ev := recvType(t, s, event.TypeTurnComplete)
	if ev.StopReason != "error" {
		t.Fatalf("want stopReason error, got %q", ev.StopReason)
	}
	ev = recvType(t, s, event.TypeError)
	if !strings.Contains(ev.Error, "quota exceeded") {
		t.Fatalf("error event lost the cause: %q", ev.Error)
	}
	if ev.ErrorKind != "quota" {
		t.Fatalf("want ErrorKind quota, got %q", ev.ErrorKind)
	}
	ev = recvType(t, s, event.TypeSessionStatus)
	if ev.Status != "error" {
		t.Fatalf("want status error, got %q", ev.Status)
	}
}

// TestStderrLimitAbortsHangingPrompt pins MADR 0073 F1: when goose logs a
// weekly quota 429 and then sleeps for 3600s without answering session/prompt,
// scraping that line must end the turn with a classified TypeError instead of
// leaving the phone on status=running forever.
func TestStderrLimitAbortsHangingPrompt(t *testing.T) {
	s := newTestSession(t)
	gate := make(chan struct{})
	s.withFramer(&fakeFramer{
		blockOn: map[string]chan struct{}{"session/prompt": gate},
	})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	// Wait until the turn is parked on session/prompt.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		busy := s.turnBusy && s.turnCancel != nil
		s.mu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never became busy")
		}
		time.Sleep(5 * time.Millisecond)
	}

	s.noteProviderLimit("Weekly usage limit reached. Resets in 4 days. GoUsageLimitError")

	ev := recvType(t, s, event.TypeTurnComplete)
	if ev.StopReason != "error" {
		t.Fatalf("stopReason = %q, want error", ev.StopReason)
	}
	ev = recvType(t, s, event.TypeError)
	if ev.ErrorKind != "quota" {
		t.Fatalf("ErrorKind = %q, want quota (msg %q)", ev.ErrorKind, ev.Error)
	}
	if !strings.Contains(strings.ToLower(ev.Error), "usage limit") &&
		!strings.Contains(strings.ToLower(ev.Error), "quota") {
		t.Fatalf("error text not natural-language limit: %q", ev.Error)
	}
	if ev.RetryAt.IsZero() {
		t.Fatal("RetryAt should parse 'Resets in 4 days'")
	}
	ev = recvType(t, s, event.TypeSessionStatus)
	if ev.Status != "error" {
		t.Fatalf("status = %q, want error", ev.Status)
	}
	close(gate) // release the framer in case anything is still waiting
}

func TestOnEngineLogLineClassifiesGooseJSON(t *testing.T) {
	s := newTestSession(t)
	s.p.sessions = map[string]*session{s.localID: s}
	gate := make(chan struct{})
	s.withFramer(&fakeFramer{
		blockOn: map[string]chan struct{}{"session/prompt": gate},
	})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		busy := s.turnBusy && s.turnCancel != nil
		s.mu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never became busy")
		}
		time.Sleep(5 * time.Millisecond)
	}

	line := `{"level":"WARN","fields":{"message":"Provider request failed with status: 429 Too Many Requests. Payload: {\"error\":{\"type\":\"GoUsageLimitError\",\"message\":\"Weekly usage limit reached. Resets in 4 days.\"}}"}}`
	s.p.onEngineLogLine(line)

	ev := recvType(t, s, event.TypeError)
	if ev.ErrorKind != "quota" {
		t.Fatalf("ErrorKind = %q, want quota (msg %q)", ev.ErrorKind, ev.Error)
	}
	close(gate)
}

func TestRPCTransportDownReturnsErrEngineDown(t *testing.T) {
	s := newTestSession(t) // no framer installed: engine dead/not started
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("Prompt: want ErrEngineDown, got %v", err)
	}
	if err := s.Cancel(context.Background()); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("Cancel: want ErrEngineDown, got %v", err)
	}
	if err := s.SetMode(context.Background(), "auto"); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("SetMode: want ErrEngineDown, got %v", err)
	}
	if err := s.SetConfigOption(context.Background(), "model", "select", "x"); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("SetConfigOption: want ErrEngineDown, got %v", err)
	}
	// A failed prompt must not leave a partial transcript (no bubble, no
	// stuck "running").
	recvNone(t, s.events, 50*time.Millisecond)
}

// ─── prompt queue (Bug: turnBusy write-only, prompts raced) ─────────────

func TestQueuedPromptEmitsUserMessageAndDrains(t *testing.T) {
	s := newTestSession(t)
	gate := make(chan struct{})
	fr := &fakeFramer{
		respond: map[string]json.RawMessage{
			"session/prompt": json.RawMessage(`{"stopReason":"end_turn"}`),
		},
		blockOn: map[string]chan struct{}{"session/prompt": gate},
	}
	s.withFramer(fr)

	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "first"}}); err != nil {
		t.Fatalf("Prompt 1: %v", err)
	}
	if ev := recvEvent(t, s.events); ev.Type != event.TypeUserMessage || ev.Text != "first" {
		t.Fatalf("want user_message 'first', got %s %q", ev.Type, ev.Text)
	}
	recvType(t, s, event.TypeSessionStatus) // running
	waitCalls(t, fr, "session/prompt", 1)   // turn 1 is in flight, blocked on gate

	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "second"}}); err != nil {
		t.Fatalf("Prompt 2 (queue): %v", err)
	}
	// Queued prompt echoes its bubble immediately, exactly once.
	if ev := recvEvent(t, s.events); ev.Type != event.TypeUserMessage || ev.Text != "second" {
		t.Fatalf("want queued user_message 'second', got %s %q", ev.Type, ev.Text)
	}
	if ev := recvEvent(t, s.events); ev.Type != event.TypeNotice {
		t.Fatalf("want queue notice, got %s", ev.Type)
	}
	if fr.calls("session/prompt") != 1 {
		t.Fatalf("second prompt must not hit the wire yet; calls=%d", fr.calls("session/prompt"))
	}

	close(gate) // release turn 1; queue drains turn 2
	recvType(t, s, event.TypeTurnComplete)
	waitCalls(t, fr, "session/prompt", 2)
	// Drain must not re-echo the bubble: no third user_message.
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeUserMessage {
			t.Fatalf("duplicate user_message after drain: %q", ev.Text)
		}
	}
}

func TestQueueFullReturnsTurnBusy(t *testing.T) {
	s := newTestSession(t)
	gate := make(chan struct{})
	fr := &fakeFramer{
		blockOn: map[string]chan struct{}{"session/prompt": gate},
	}
	s.withFramer(fr)
	t.Cleanup(func() { close(gate) })

	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "active"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	for i := 0; i < maxPromptQueue; i++ {
		if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "queued"}}); err != nil {
			t.Fatalf("queue %d: %v", i, err)
		}
	}
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "one too many"}}); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("want ErrTurnBusy, got %v", err)
	}
}

// ─── coalescing + delivery guarantees (MADR 0024; never drop control) ───

func TestStreamCoalescingMergesChunks(t *testing.T) {
	s := newTestSession(t)
	win := 40 * time.Millisecond
	s.cfg.StreamCoalesce = &win

	s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"c1"}}`))
	if ev := recvEvent(t, s.events); ev.Text != "c1" {
		t.Fatalf("leading chunk must pass immediately, got %q", ev.Text)
	}
	s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"c2"}}`))
	s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"c3"}}`))
	// Within the window nothing else arrives; the flush merges verbatim.
	ev := recvType(t, s, event.TypeAssistantChunk)
	if ev.Text != "c2c3" {
		t.Fatalf("want coalesced %q, got %q", "c2c3", ev.Text)
	}

	// A control event is a boundary: pending text lands ahead of it, and a
	// timed flush must NOT have granted a fresh immediate emit.
	s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"c4"}}`))
	s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})
	if ev := recvEvent(t, s.events); ev.Type != event.TypeAssistantChunk || ev.Text != "c4" {
		t.Fatalf("pending text must land before the boundary, got %s %q", ev.Type, ev.Text)
	}
	if ev := recvEvent(t, s.events); ev.Type != event.TypeTurnComplete {
		t.Fatalf("want turn_complete after the flush, got %s", ev.Type)
	}
}

func TestStreamCoalescingDisabledPassesVerbatim(t *testing.T) {
	s := newTestSession(t)
	zero := time.Duration(0)
	s.cfg.StreamCoalesce = &zero

	for _, c := range []string{"a", " b", "\n"} {
		s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":` +
			strconv.Quote(c) + `}}`))
		ev := recvEvent(t, s.events)
		if ev.Text != c {
			t.Fatalf("want %q, got %q", c, ev.Text)
		}
	}
}

func TestControlEventSurvivesBackpressure(t *testing.T) {
	s := newTestSession(t)
	zero := time.Duration(0)
	s.cfg.StreamCoalesce = &zero
	s.events = make(chan event.Event, 1) // one slot, no consumer: full after the chunk

	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "fills the buffer"})
	emitted := make(chan struct{})
	go func() {
		defer close(emitted)
		s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})
	}()
	select {
	case <-emitted:
		t.Fatal("control event was dropped or delivered into a full channel")
	case <-time.After(80 * time.Millisecond):
	}
	if ev := recvEvent(t, s.events); ev.Type != event.TypeAssistantChunk {
		t.Fatalf("want chunk first, got %s", ev.Type)
	}
	select {
	case <-emitted:
	case <-time.After(time.Second):
		t.Fatal("control event never landed after the channel drained")
	}
	if ev := recvEvent(t, s.events); ev.Type != event.TypeTurnComplete {
		t.Fatalf("want turn_complete, got %s", ev.Type)
	}
}

func TestCloseDrainsPendingChunks(t *testing.T) {
	s := newTestSession(t)
	win := time.Hour // never flushes on its own
	s.cfg.StreamCoalesce = &win
	s.withFramer(&fakeFramer{})

	s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"tail"}}`))
	recvEvent(t, s.events) // leading chunk "tail" passes immediately
	s.handleUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":" end"}}`))

	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeAssistantChunk || ev.Text != " end" {
		t.Fatalf("Close must land the buffered tail, got %s %q", ev.Type, ev.Text)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestFailAllUnblocksInFlightRPC(t *testing.T) {
	fr := newWSFramer(nil, slog.Default())
	ch := make(chan rpcResponse, 1)
	fr.mu.Lock()
	fr.pending[1] = ch
	fr.mu.Unlock()

	fr.failAll()
	select {
	case res := <-ch:
		if !errors.Is(res.err, errConnLost) {
			t.Fatalf("want errConnLost, got %v", res.err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight RPC was not unblocked")
	}
	if len(fr.pending) != 0 {
		t.Fatalf("pending map not cleared: %d entries", len(fr.pending))
	}
}

// TestToolLaneSupersedesNonTerminalUpdates is MADR 0057 M-2: non-terminal
// tool_call_update for the same id collapses to the latest within the window.
func TestToolLaneSupersedesNonTerminalUpdates(t *testing.T) {
	s := newTestSession(t)
	win := time.Hour
	s.cfg.StreamCoalesce = &win

	const n = 8
	for i := 0; i < n; i++ {
		s.emit(event.Event{
			Type:   event.TypeToolUpdate,
			ToolID: "bash-1",
			Status: event.ToolStatusRunning,
			Text:   "line " + strconv.Itoa(i),
		})
	}
	s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	var updates []event.Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeToolUpdate {
				updates = append(updates, ev)
			}
			if ev.Type == event.TypeTurnComplete {
				if len(updates) != 1 {
					t.Fatalf("tool_call_update count = %d, want 1 (superseded)", len(updates))
				}
				if updates[0].Text != "line "+strconv.Itoa(n-1) {
					t.Fatalf("text = %q, want last line", updates[0].Text)
				}
				return
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	t.Fatalf("timeout; tool updates seen = %d", len(updates))
}

func TestToolLaneTerminalFlushesImmediately(t *testing.T) {
	s := newTestSession(t)
	win := time.Hour
	s.cfg.StreamCoalesce = &win

	s.emit(event.Event{
		Type:   event.TypeToolUpdate,
		ToolID: "bash-1",
		Status: event.ToolStatusRunning,
		Text:   "partial",
	})
	s.emit(event.Event{
		Type:   event.TypeToolUpdate,
		ToolID: "bash-1",
		Status: event.ToolStatusCompleted,
		Text:   "done",
	})

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeToolUpdate || ev.Status != event.ToolStatusCompleted {
		t.Fatalf("want immediate terminal update, got %+v", ev)
	}
	if ev.Text != "done" && ev.Text != "partial" {
		// mergeTool may keep earlier text if next empty; next has done.
		if ev.Text != "done" {
			t.Fatalf("text = %q, want done", ev.Text)
		}
	}
}
