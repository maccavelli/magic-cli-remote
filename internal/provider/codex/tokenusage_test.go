package codex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestTokenUsageDecodesTheShapeCodexActuallySends is the regression for a
// defect that cost codex its usage reporting entirely (MADR 0137, second
// correction).
//
// `tokenUsage.total` is an OBJECT and `modelContextWindow` is nested inside
// `tokenUsage`. mcremote decoded both as flat ints, so json.Unmarshal failed,
// the `err == nil` guard skipped the emit, and codex published no usage at
// all — no /context on the phone, and no cold/warm signal for the latency
// record. Nothing failed loudly; the event simply never existed.
//
// The fixture is the source, so a codex release that reshapes this is caught
// when the fixture is re-recorded rather than in production.
func TestTokenUsageDecodesTheShapeCodexActuallySends(t *testing.T) {
	data, err := os.ReadFile("testdata/wire/" + KnownGoodVersion + "/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var params json.RawMessage
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, `"thread/tokenUsage/updated"`) {
			continue
		}
		var frame struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		if frame.Method == "thread/tokenUsage/updated" {
			params = frame.Params
			break
		}
	}
	if params == nil {
		t.Fatal("no thread/tokenUsage/updated in the fixture: this test cannot " +
			"establish anything about the shape codex sends")
	}

	// The production type, not a copy of it: a duplicated struct is how the
	// original defect survived, since nothing that restated the shape could
	// disagree with the shape.
	var p codexTokenUsageParams
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("decode failed against the real wire shape: %v", err)
	}

	total := p.TokenUsage.Total
	if total.TotalTokens == 0 {
		t.Fatal("totalTokens decoded as 0: the object shape was not read")
	}
	if total.CachedInputTokens == 0 {
		t.Fatal("cachedInputTokens decoded as 0, but the fixture reports a " +
			"cache read; without it every codex turn reads as cold")
	}
	if p.TokenUsage.ModelContextWindow == 0 {
		t.Fatal("modelContextWindow decoded as 0: it is nested inside " +
			"tokenUsage, not alongside it")
	}

	u := total.usage(p.TokenUsage.ModelContextWindow)
	if u.Used != total.TotalTokens || u.Size != p.TokenUsage.ModelContextWindow {
		t.Fatalf("usage totals wrong: %+v", u)
	}
	if u.CacheRead != int64(total.CachedInputTokens) {
		t.Fatalf("CacheRead = %d, want %d", u.CacheRead, total.CachedInputTokens)
	}
	// Input is codex's whole input; the cached share is reported alongside it,
	// never added to it.
	if u.Input != int64(total.InputTokens) {
		t.Fatalf("Input = %d, want the reported inputTokens %d (the cached "+
			"share must not be added to it)", u.Input, total.InputTokens)
	}
}
