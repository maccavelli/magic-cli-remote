package acpagent

import (
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
)

func TestNewSessionRequestIncludesSessionMeta(t *testing.T) {
	mcp := []acp.McpServer{}
	req := sessionOpenNew("/tmp", mcp, nil)
	if req.Meta != nil {
		t.Fatalf("empty meta: got %#v, want nil", req.Meta)
	}
	req = sessionOpenNew("/tmp", mcp, map[string]any{})
	if req.Meta != nil {
		t.Fatalf("zero-len meta: got %#v, want nil", req.Meta)
	}
	req = sessionOpenNew("/tmp", mcp, map[string]any{"modelId": "grok-4.5", "reasoningEffort": "low"})
	if req.Meta["modelId"] != "grok-4.5" || req.Meta["reasoningEffort"] != "low" {
		t.Fatalf("meta = %#v", req.Meta)
	}
	if _, ok := req.Meta["yoloMode"]; ok {
		t.Fatal("must not stamp yoloMode")
	}
}
