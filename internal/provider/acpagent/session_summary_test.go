package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// rawJSON marshals v as the raw JSON value the SDK hands this package.
func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSummarizeToolContent_skipsHugeJSON(t *testing.T) {
	raw := rawJSON(t, map[string]any{
		"stdout": strings.Repeat("x", 5000),
		"nested": map[string]any{"a": 1, "b": 2},
	})
	s := summarizeToolContent(nil, nil, raw, 200)
	if strings.Contains(s, strings.Repeat("x", 100)) {
		t.Fatalf("dumped raw output into summary: %q", s)
	}
}

// TestSummarizeToolContentRawValues pins how each shape of raw JSON value
// reaches a tool card or a permission summary (PLAN 0184 A1 step 8). The SDK
// hands rawInput and rawOutput over undecoded, and an absent one as an empty
// value; the summary must never show the text "null" or a quoted string.
func TestSummarizeToolContentRawValues(t *testing.T) {
	cases := []struct {
		name   string
		raw    json.RawMessage
		want   string
		absent bool
	}{
		{name: "absent (nil)", raw: nil, absent: true},
		{name: "absent (empty)", raw: json.RawMessage{}, absent: true},
		{name: "JSON null", raw: json.RawMessage(`null`), absent: true},
		{name: "string", raw: json.RawMessage(`"ls -la"`), want: "in: ls -la"},
		{name: "blank string", raw: json.RawMessage(`"   "`), absent: true},
		{name: "number", raw: json.RawMessage(`42`), want: "in: 42"},
		{name: "boolean", raw: json.RawMessage(`true`), want: "in: true"},
		{name: "object", raw: json.RawMessage(`{"command":"ls"}`), absent: true},
		{name: "array", raw: json.RawMessage(`["a","b"]`), absent: true},
		{name: "large string", raw: rawJSON(t, strings.Repeat("y", 5000)), want: "in: " + strings.Repeat("y", 100)},
		{name: "malformed", raw: json.RawMessage(`{"unterminated`), absent: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeToolContent(nil, tc.raw, nil, 400)
			if strings.Contains(got, "null") || strings.Contains(got, `"`) {
				t.Fatalf("summary %q shows null or a quoted value", got)
			}
			if tc.absent && got != "" {
				t.Fatalf("summary = %q, want nothing", got)
			}
			if !tc.absent && !strings.HasPrefix(got, tc.want) {
				t.Fatalf("summary = %q, want it to start %q", got, tc.want)
			}
		})
	}
}

func TestSummarizeToolContent_prefersTextContent(t *testing.T) {
	content := []acp.ToolCallContent{
		acp.ToolContent(acp.TextBlock("hello from tool")),
	}
	s := summarizeToolContent(content, rawJSON(t, map[string]any{"cmd": "ls"}), rawJSON(t, "ignored-raw"), 200)
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
