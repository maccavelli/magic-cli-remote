package chunkbuf_test

import (
	"os"
	"strings"
	"testing"
)

// Each provider's tool-lane mode has to match the shape of its own
// `tool_call_update`, and the two are decided in different files. This pins the
// pairing by reading the construction sites, so flipping one without the other
// fails here rather than silently discarding (append→replace) or duplicating
// (replace→append) an agent's command output.
//
//	kilo / opencode → snapshot payload, deduped by noteToolEmit → replace
//	goose           → summarizeTCContent(tu.Content, …)         → replace
//	grok            → summarizeToolContent(tu.Content, …)       → replace
//	codex           → Text: p.Delta                             → append
//
// MADR 0138 F2/F3; the evidence for each is quoted in that record.
func TestEachProviderUsesTheToolLaneModeItsPayloadNeeds(t *testing.T) {
	cases := []struct {
		provider string
		file     string
		want     string
		reason   string
	}{
		{
			provider: "codex",
			file:     "../provider/codex/session.go",
			want:     "chunkbuf.WithToolLaneAppend()",
			reason:   "codex sets Text to a delta; replacing discards command output",
		},
		{
			provider: "grok (acpagent)",
			file:     "../provider/acpagent/session.go",
			want:     "chunkbuf.WithToolLane()",
			reason:   "grok sends the full ACP content each time",
		},
		{
			provider: "goose (acphttp)",
			file:     "../provider/acphttp/session.go",
			want:     "chunkbuf.WithToolLane()",
			reason:   "goose sends the full ACP content each time",
		},
		{
			provider: "kilo/opencode (httpagent)",
			file:     "../provider/httpagent/session.go",
			want:     "chunkbuf.WithToolLane()",
			reason:   "httpagent emits a deduped snapshot",
		},
	}

	for _, tc := range cases {
		src, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("%s: %v", tc.provider, err)
		}
		body := string(src)
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s does not construct its buffer with %s (%s)", tc.provider, tc.want, tc.reason)
		}
		// Append and replace are spelled differently, and WithToolLaneAppend
		// contains WithToolLane as a substring — so a provider on replace must
		// be checked for the absence of the append spelling explicitly.
		if tc.want == "chunkbuf.WithToolLane()" && strings.Contains(body, "chunkbuf.WithToolLaneAppend()") {
			t.Errorf("%s uses the append lane; its updates are snapshots and would be duplicated (%s)",
				tc.provider, tc.reason)
		}
	}
}
