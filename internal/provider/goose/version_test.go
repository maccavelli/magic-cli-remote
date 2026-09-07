package goose

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestPinIsCorroboratedByItsFixture keeps the constant honest.
//
// A pin is a claim that the wire shapes were checked against that release, and
// the fixture directory is the evidence. goose states its own version inside
// the capture, so the claim can be checked rather than trusted: a bump that
// edits the constant and forgets the fixture, or re-records the fixture from a
// different engine, fails here.
//
// This also pins the re-recorded fixture's coverage. Before MADR 0137 step 7.9
// the capture hooked only the websocket, while `initialize` goes over
// POST /acp — so the fixture began at the session/new result and contained no
// agentInfo at all. If that regressed, the lookup below finds nothing.
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
				AgentInfo struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"agentInfo"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		if v := frame.Result.AgentInfo.Version; v != "" {
			found = v
			break
		}
	}
	if found == "" {
		t.Fatalf("no agentInfo.version in %s/frames.jsonl: the fixture does not "+
			"cover the initialize handshake, so it cannot corroborate the pin", dir)
	}
	if found != KnownGoodVersion {
		t.Fatalf("fixture reports agentInfo.version %q but KnownGoodVersion is %q: "+
			"the pin cites evidence from a different engine", found, KnownGoodVersion)
	}
}
