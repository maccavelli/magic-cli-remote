package acpagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestSummarizeToolContent_skipsHugeJSON(t *testing.T) {
	raw := map[string]any{
		"stdout": strings.Repeat("x", 5000),
		"nested": map[string]any{"a": 1, "b": 2},
	}
	s := summarizeToolContent(nil, nil, raw, 200)
	if strings.Contains(s, strings.Repeat("x", 100)) {
		t.Fatalf("dumped raw output into summary: %q", s)
	}
}

func TestSummarizeToolContent_prefersTextContent(t *testing.T) {
	content := []acp.ToolCallContent{
		acp.ToolContent(acp.TextBlock("hello from tool")),
	}
	s := summarizeToolContent(content, map[string]any{"cmd": "ls"}, "ignored-raw", 200)
	if !strings.Contains(s, "hello from tool") {
		t.Fatalf("got %q", s)
	}
	if strings.Contains(s, "ignored-raw") {
		t.Fatalf("should prefer content over raw: %q", s)
	}
}

func TestSummarizeToolContent_diff(t *testing.T) {
	content := []acp.ToolCallContent{
		acp.ToolDiffContent("/tmp/a.go", "new", "old"),
	}
	s := summarizeToolContent(content, nil, nil, 200)
	if !strings.Contains(s, "diff") || !strings.Contains(s, "a.go") {
		t.Fatalf("got %q", s)
	}
}

func TestIsBenignPromptErr(t *testing.T) {
	if !isBenignPromptErr(context.Canceled) {
		t.Fatal("context.Canceled should be benign")
	}
	if !isBenignPromptErr(errors.New("rpc error: context canceled")) {
		t.Fatal("canceled string should be benign")
	}
	if isBenignPromptErr(errors.New("disk full")) {
		t.Fatal("disk full should not be benign")
	}
}

func TestSanitizeUserFacingErr(t *testing.T) {
	long := strings.Repeat("e", 1000)
	s := sanitizeUserFacingErr(errors.New(long))
	if len(s) > 420 {
		t.Fatalf("not truncated: %d", len(s))
	}
}
