package codex

import (
	"encoding/json"
	"testing"
)

// mcpApprovalParams is an mcpServer/elicitation/request as codex 0.167.0 builds
// it for an MCP tool call (core/src/mcp_tool_call.rs,
// build_mcp_tool_approval_elicitation_meta), with meta as its `_meta`.
func mcpApprovalParams(meta string) json.RawMessage {
	return json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","serverName":"docs","mode":"form",` +
		`"message":"Allow the docs MCP server to run tool \"search_pages\"?",` +
		`"requestedSchema":{"type":"object","properties":{}},"_meta":` + meta + `}`)
}

// TestMCPApprovalNamesTheTool is MADR 0236 D4 (Q2 A): the operator approving an
// MCP tool call is told which tool it is, and is warned when the server marks the
// tool destructive. Before this, the sheet said only the server's name (0236 F8).
func TestMCPApprovalNamesTheTool(t *testing.T) {
	tests := []struct {
		name, meta, want string
	}{
		{"title, name and a destructive hint",
			`{"codex_approval_kind":"mcp_tool_call","tool_name":"search_pages","tool_title":"Search pages","tool_annotations":{"readOnlyHint":false,"destructiveHint":true}}`,
			"docs: Search pages (search_pages). This tool can change or delete data."},
		{"a connector names the source",
			`{"codex_approval_kind":"mcp_tool_call","tool_name":"create_doc","tool_title":"Create document","connector_name":"Drive"}`,
			"Drive: Create document (create_doc)"},
		{"a name alone, and a hint that is not destructive",
			`{"codex_approval_kind":"mcp_tool_call","tool_name":"search_pages","tool_annotations":{"destructiveHint":false}}`,
			"docs: search_pages"},
		{"no keys keeps the server name", `{}`, "docs"},
		{"no _meta keeps the server name", `null`, "docs"},
		{"an unreadable _meta keeps the server name", `"not an object"`, "docs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb, err := parseCallback("mcpServer/elicitation/request", json.RawMessage(`7`), mcpApprovalParams(tc.meta), 0)
			if err != nil {
				t.Fatalf("parseCallback: %v", err)
			}
			if cb.detail != tc.want {
				t.Errorf("detail = %q, want %q", cb.detail, tc.want)
			}
		})
	}

	// And the phone sees it: the permission event carries the detail as its text.
	s, _ := permSession(t)
	s.handleApprovalRequest("mcpServer/elicitation/request", json.RawMessage(`8`),
		mcpApprovalParams(`{"tool_name":"search_pages","tool_title":"Search pages"}`))
	found := false
	for _, ev := range drainEvents(s) {
		if ev.Text == "docs: Search pages (search_pages)" {
			found = true
		}
	}
	if !found {
		t.Error("no permission event carried the tool's name to the phone")
	}
}
