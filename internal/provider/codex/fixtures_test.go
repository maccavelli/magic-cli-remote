package codex

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestConnRequestEncoding(t *testing.T) {
	// Verify JSON-RPC frames are newline-delimited without jsonrpc field.
	msg := map[string]any{
		"method": "initialize",
		"id":     1,
		"params": map[string]any{
			"clientInfo": map[string]string{
				"name": "mcremote",
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	line := string(data) + "\n"

	if strings.Contains(string(data), `"jsonrpc"`) {
		t.Error("Codex app-server frames must NOT include jsonrpc field")
	}
	if !strings.HasSuffix(line, "\n") {
		t.Error("frames must end with newline")
	}
}

func TestConnNotificationEncoding(t *testing.T) {
	// Notifications have method and params, no id.
	msg := map[string]any{
		"method": "initialized",
		"params": nil,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.ID) > 0 && string(decoded.ID) != "null" {
		t.Error("notification must not have an id field")
	}
}

func TestWireMessageParsing(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		isResp  bool
		isNotif bool
		isReq   bool
	}{
		{
			name:   "response",
			line:   `{"id":1,"result":{"status":"ok"}}`,
			isResp: true,
		},
		{
			name:    "notification",
			line:    `{"method":"turn/completed","params":{"threadId":"abc"}}`,
			isNotif: true,
		},
		{
			name:  "server_request",
			line:  `{"method":"item/commandExecution/requestApproval","id":2,"params":{"command":"ls"}}`,
			isReq: true,
		},
		{
			name:   "error_response",
			line:   `{"id":3,"error":{"code":-32600,"message":"unknown variant"}}`,
			isResp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg wireMessage
			if err := json.Unmarshal([]byte(tt.line), &msg); err != nil {
				t.Fatal(err)
			}
			hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
			hasMethod := msg.Method != ""
			hasResult := msg.Result != nil
			hasError := msg.Error != nil

			if tt.isResp && (!hasID || hasMethod) {
				t.Error("expected response (id + result/error, no method)")
			}
			if tt.isResp && !hasResult && !hasError {
				t.Error("response must have result or error")
			}
			if tt.isNotif && (hasID || !hasMethod) {
				t.Error("expected notification (method, no id)")
			}
			if tt.isReq && (!hasMethod || !hasID) {
				t.Error("expected server request (method + id)")
			}
		})
	}
}

func TestTurnStartResultShape(t *testing.T) {
	// Match the shape from the spike (summary.json, turn_start_result).
	frame := `{
		"id": 5,
		"result": {
			"turn": {
				"id": "019f9fa6-8c3b-77c0-bb8b-63992477f3d1",
				"items": [],
				"itemsView": "notLoaded",
				"status": "inProgress",
				"error": null,
				"startedAt": null,
				"completedAt": null,
				"durationMs": null
			}
		}
	}`
	var msg wireMessage
	if err := json.Unmarshal([]byte(frame), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Result == nil {
		t.Fatal("expected result field")
	}
	var resp struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(msg.Result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Turn.ID == "" {
		t.Error("turn.id must not be empty")
	}
	if resp.Turn.Status != "inProgress" {
		t.Errorf("turn.status = %q, want inProgress", resp.Turn.Status)
	}
}

func TestTurnCompletedShape(t *testing.T) {
	// Match the shape from the spike (summary.json, turn_completed).
	frame := `{
		"method": "turn/completed",
		"params": {
			"threadId": "019f9fa6-857a-7c60-92c0-a97b7ffc76f6",
			"turn": {
				"id": "019f9fa6-8c3b-77c0-bb8b-63992477f3d1",
				"items": [],
				"itemsView": "notLoaded",
				"status": "completed",
				"error": null,
				"startedAt": 1785089920,
				"completedAt": 1785089921,
				"durationMs": 1096
			}
		}
	}`
	var msg wireMessage
	if err := json.Unmarshal([]byte(frame), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Method != "turn/completed" {
		t.Errorf("method = %q, want turn/completed", msg.Method)
	}
	hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
	if hasID {
		t.Error("turn/completed is a notification (no id)")
	}
	var p struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.Turn.Status != "completed" {
		t.Errorf("turn.status = %q, want completed", p.Turn.Status)
	}
}

func TestAgentMessageDeltaShape(t *testing.T) {
	// Match agent message delta notification shape.
	frame := `{
		"method": "item/agentMessage/delta",
		"params": {
			"threadId": "thread-uuid",
			"turnId": "turn-uuid",
			"itemId": "item-uuid",
			"delta": "Hello, world!"
		}
	}`
	var msg wireMessage
	if err := json.Unmarshal([]byte(frame), &msg); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.Delta == "" {
		t.Error("expected non-empty delta")
	}
}

func TestTurnSteerWireShape(t *testing.T) {
	// turn/steer requires expectedTurnId (spike-proven).
	msg := map[string]any{
		"method": "turn/steer",
		"id":     1,
		"params": map[string]any{
			"threadId":       "thread-uuid",
			"expectedTurnId": "turn-uuid",
			"input":          []any{map[string]any{"type": "text", "text": "steer me"}},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "turn/steer" {
		t.Errorf("method = %q, want turn/steer", decoded.Method)
	}
	var params struct {
		ExpectedTurnID string `json:"expectedTurnId"`
	}
	if err := json.Unmarshal(decoded.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.ExpectedTurnID == "" {
		t.Error("turn/steer must include expectedTurnId")
	}
}

func TestTurnStartedNotificationShape(t *testing.T) {
	// turn/started notifies that steering is now available.
	frame := `{
		"method": "turn/started",
		"params": {
			"threadId": "thread-uuid",
			"turnId": "turn-uuid"
		}
	}`
	var msg wireMessage
	if err := json.Unmarshal([]byte(frame), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Method != "turn/started" {
		t.Errorf("method = %q, want turn/started", msg.Method)
	}
	hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
	if hasID {
		t.Error("turn/started is a notification (no id)")
	}
}

// captureThreadRequest drives one session create through a real conn and
// returns the JSON-RPC frame the session actually put on the wire.
//
// The previous version of these tests hand-built the params map inside the test
// and asserted against that, so it could not observe what the production code
// sends — which is how the object-shaped `sandbox` bug survived (MADR 0044
// Finding 5). Always drive create/resume, never a local literal.
func captureThreadRequest(t *testing.T, cfg Config, opts provider.StartOptions) (method string, params map[string]any) {
	t.Helper()

	engineR, sessionW := io.Pipe() // session writes → we read
	sessionR, engineW := io.Pipe() // we write → session reads
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})

	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})

	// Build through newSession, not a struct literal: the session seeds its
	// live policy from config there, and a hand-rolled literal silently misses
	// whatever the constructor gains next. That drift is the same failure mode
	// as the fixture bug this helper was written to prevent (MADR 0044).
	if opts.LocalSessionID == "" {
		opts.LocalSessionID = "local-1"
	}
	p := &Provider{cfg: cfg, log: testLogger(t), eng: &engine{generation: 1}, sessions: make(map[string]*session)}
	s := newSession(p, cfg, opts, testLogger(t))

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- s.create(ctx, c)
	}()

	var req struct {
		ID     int64           `json:"id"`
		Method string          `json:"method"`
		Params map[string]any  `json:"params"`
		Raw    json.RawMessage `json:"-"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}

	// Unblock create so the goroutine does not leak.
	resp, err := json.Marshal(map[string]any{
		"id":     req.ID,
		"result": map[string]any{"thread": map[string]any{"id": "thread-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engineW.Write(append(resp, '\n')); err != nil {
		t.Fatalf("write response: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("create did not return")
	}
	return req.Method, req.Params
}

// TestThreadStartSandboxIsEnumString pins the shape codex 0.145 actually
// accepts. thread/start takes SandboxMode as a plain kebab-case *string*; the
// object form is rejected with -32600 "expected map with a single key" and
// fails session create (MADR 0044 D7).
//
// Config pairs must match an advertised mode (or AllowFullAccess for
// full-access): seedPolicy remaps unmatched pairs to default (MADR 0047).
func TestThreadStartSandboxIsEnumString(t *testing.T) {
	cases := []struct {
		name     string
		cfg      Config
		sandbox  string
		approval string
	}{
		{
			name:     "read-only",
			cfg:      Config{SandboxMode: "read-only", ApprovalPolicy: "on-request"},
			sandbox:  "read-only",
			approval: "on-request",
		},
		{
			name:     "workspace-write",
			cfg:      Config{SandboxMode: "workspace-write", ApprovalPolicy: "never"},
			sandbox:  "workspace-write",
			approval: "never",
		},
		{
			name: "danger-full-access",
			cfg: Config{
				SandboxMode: "danger-full-access", ApprovalPolicy: "never",
				AllowFullAccess: true,
			},
			sandbox:  "danger-full-access",
			approval: "never",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method, params := captureThreadRequest(t, tc.cfg,
				provider.StartOptions{CWD: "/home/user"})

			if method != "thread/start" {
				t.Fatalf("method = %q, want thread/start", method)
			}
			got, ok := params["sandbox"].(string)
			if !ok {
				t.Fatalf("sandbox = %#v (%T), want a plain enum string",
					params["sandbox"], params["sandbox"])
			}
			if got != tc.sandbox {
				t.Errorf("sandbox = %q, want %q", got, tc.sandbox)
			}
			if ap, _ := params["approvalPolicy"].(string); ap != tc.approval {
				t.Errorf("approvalPolicy = %#v, want %q", params["approvalPolicy"], tc.approval)
			}
			if source, _ := params["threadSource"].(string); source != "mcremote" {
				t.Errorf("threadSource = %#v, want mcremote", params["threadSource"])
			}
		})
	}
}

// TestThreadResumeCarriesPolicy: a resumed thread must run under the same
// policy as a new one, not the engine's own defaults.
func TestThreadResumeCarriesPolicy(t *testing.T) {
	method, params := captureThreadRequest(t,
		Config{SandboxMode: "workspace-write", ApprovalPolicy: "on-request"},
		provider.StartOptions{CWD: "/home/user", AgentSessionID: "thread-uuid"})

	if method != "thread/resume" {
		t.Fatalf("method = %q, want thread/resume", method)
	}
	if params["threadId"] != "thread-uuid" {
		t.Errorf("threadId = %#v", params["threadId"])
	}
	if got, ok := params["sandbox"].(string); !ok || got != "workspace-write" {
		t.Errorf("sandbox = %#v, want %q as a string", params["sandbox"], "workspace-write")
	}
	if got, _ := params["approvalPolicy"].(string); got != "on-request" {
		t.Errorf("approvalPolicy = %#v, want %q", params["approvalPolicy"], "on-request")
	}
	if _, ok := params["threadSource"]; ok {
		t.Errorf("thread/resume must omit threadSource: %#v", params)
	}
}

func TestInterruptWireShape(t *testing.T) {
	msg := map[string]any{
		"method": "turn/interrupt",
		"id":     1,
		"params": map[string]any{
			"threadId": "thread-uuid",
			"turnId":   "turn-uuid",
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded wireMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "turn/interrupt" {
		t.Errorf("method = %q, want turn/interrupt", decoded.Method)
	}
}

// TestEmptyConfigSeedsDefaultPolicyOnWire: empty provider config no longer
// omits policy fields (that left untrusted projects in readOnly with an empty
// current mode). seedPolicy fills the default pair so thread/start is explicit
// (MADR 0047 D2). Sending "" is still forbidden — codex rejects it as an
// unknown SandboxMode variant.
func TestEmptyConfigSeedsDefaultPolicyOnWire(t *testing.T) {
	_, params := captureThreadRequest(t, Config{}, provider.StartOptions{CWD: "/home/user"})

	if got, ok := params["sandbox"].(string); !ok || got != "workspace-write" {
		t.Errorf("sandbox = %#v, want %q", params["sandbox"], "workspace-write")
	}
	if got, _ := params["approvalPolicy"].(string); got != "on-request" {
		t.Errorf("approvalPolicy = %#v, want %q", params["approvalPolicy"], "on-request")
	}
}
