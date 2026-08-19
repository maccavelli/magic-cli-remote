package acpagent

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestAppliedModelAndEffortFromMeta(t *testing.T) {
	xhigh := map[string]any{
		"x.ai/sessionDetail": map[string]any{"currentModelId": "grok-4.6"},
		"x.ai/sessionConfig": map[string]any{
			"options": []any{
				map[string]any{"id": "grok-4.6", "category": "model", "selected": true},
				map[string]any{"id": "grok-4.5", "category": "model", "selected": false},
				map[string]any{"id": "xhigh", "category": "mode", "selected": true},
				map[string]any{"id": "high", "category": "mode", "selected": false},
			},
		},
	}
	if got := appliedModelFromMeta(xhigh); got != "grok-4.6" {
		t.Errorf("xhigh model = %q, want grok-4.6", got)
	}
	if got := appliedEffortFromMeta(xhigh); got != "xhigh" {
		t.Errorf("xhigh effort = %q, want xhigh", got)
	}

	low := map[string]any{
		"x.ai/sessionDetail": map[string]any{"currentModelId": "grok-4.6"},
		"x.ai/sessionConfig": map[string]any{
			"options": []any{
				map[string]any{"id": "grok-4.6", "category": "model", "selected": true},
				map[string]any{"id": "low", "category": "mode", "selected": true},
				map[string]any{"id": "high", "category": "mode", "selected": false},
			},
		},
	}
	if got := appliedEffortFromMeta(low); got != "low" {
		t.Errorf("low effort = %q, want low", got)
	}

	high := map[string]any{
		"x.ai/sessionDetail": map[string]any{"currentModelId": "grok-4.6"},
		"x.ai/sessionConfig": map[string]any{
			"options": []any{
				map[string]any{"id": "grok-4.6", "category": "model", "selected": true},
				map[string]any{"id": "high", "category": "mode", "selected": true},
				map[string]any{"id": "low", "category": "mode", "selected": false},
			},
		},
	}
	if got := appliedEffortFromMeta(high); got != "high" {
		t.Errorf("baseline effort = %q, want high", got)
	}

	if got := appliedModelFromMeta(nil); got != "" {
		t.Errorf("nil meta model = %q", got)
	}
	if got := appliedEffortFromMeta(map[string]any{}); got != "" {
		t.Errorf("empty meta effort = %q", got)
	}
}

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
