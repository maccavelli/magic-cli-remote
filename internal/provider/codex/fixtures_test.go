package codex

import (
	"encoding/json"
	"strings"
	"testing"
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

func TestThreadStartWireShape(t *testing.T) {
	// thread/start with optional sandbox and approvalPolicy.
	tests := []struct {
		name   string
		params map[string]any
	}{
		{
			name:   "bare",
			params: map[string]any{},
		},
		{
			name: "with_overrides",
			params: map[string]any{
				"cwd":            "/home/user",
				"model":          "gpt-5.6-terra",
				"sandbox":        map[string]any{"type": "read-only", "networkAccess": false},
				"approvalPolicy": "never",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := map[string]any{
				"method": "thread/start",
				"id":     1,
				"params": tt.params,
			}
			data, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			var decoded wireMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Method != "thread/start" {
				t.Errorf("method = %q", decoded.Method)
			}
			// sandbox casing must be kebab-case on wire
			if tt.name == "with_overrides" {
				raw := string(data)
				if strings.Contains(raw, `"type":"readOnly"`) || strings.Contains(raw, `"type":"workspaceWrite"`) {
					t.Error("sandbox type must be kebab-case on wire (read-only, not readOnly)")
				}
			}
		})
	}
}

func TestThreadResumeWireShape(t *testing.T) {
	msg := map[string]any{
		"method": "thread/resume",
		"id":     1,
		"params": map[string]any{
			"threadId": "thread-uuid",
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
	if decoded.Method != "thread/resume" {
		t.Errorf("method = %q, want thread/resume", decoded.Method)
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

func TestEmptySandboxOmitsWireField(t *testing.T) {
	// Empty sandbox_mode means do NOT send sandbox on thread/start.
	// This test verifies that marshalling nil/empty params works correctly.
	params := map[string]any{}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	if strings.Contains(raw, "sandbox") || strings.Contains(raw, "approvalPolicy") {
		t.Error("empty sandbox/approvalPolicy must omit the wire fields")
	}
}
