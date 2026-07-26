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
