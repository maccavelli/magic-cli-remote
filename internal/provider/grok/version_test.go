package grok

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestPinIsCorroboratedByItsFixture keeps the constant honest, and pins the
// fact that grok reports its version in a vendor slot.
//
// grok sends no standard ACP `agentInfo` — the lookup below deliberately reads
// `_meta.agentVersion` instead, and a future grok that moved to agentInfo would
// fail here rather than silently leaving the pin uncheckable
// (MADR 0137, ninth amendment).
func TestPinIsCorroboratedByItsFixture(t *testing.T) {
	dir := "testdata/wire/" + KnownGoodVersion
	data, err := os.ReadFile(dir + "/frames.jsonl")
	if err != nil {
		t.Fatalf("KnownGoodVersion is %q but its fixture is unreadable; a pin "+
			"without a fixture is a claim that nothing changed: %v", KnownGoodVersion, err)
	}
	found := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var frame struct {
			Result struct {
				Meta struct {
					AgentVersion string `json:"agentVersion"`
				} `json:"_meta"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		if v := frame.Result.Meta.AgentVersion; v != "" {
			found = v
			break
		}
	}
	if found == "" {
		t.Fatalf("no _meta.agentVersion in %s/frames.jsonl: the fixture cannot "+
			"corroborate the pin", dir)
	}
	if found != KnownGoodVersion {
		t.Fatalf("fixture reports agentVersion %q but KnownGoodVersion is %q: "+
			"the pin cites evidence from a different engine", found, KnownGoodVersion)
	}
}
