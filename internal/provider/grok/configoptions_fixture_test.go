package grok

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// TestGrok1050FramesDecodeAsStandardConfigOptions reads the grok 1.0.50
// capture (PLAN 0184 A3 step 6) with the SDK's own types, which reject an
// object missing a required key: every session/new|load answer,
// set_config_option answer and config_option_update in it must decode, and
// each must carry a select option in both the `model` and `thought_level`
// categories. That is what makes acpagent take the standard path for this
// grok instead of the Spec fallbacks.
func TestGrok1050FramesDecodeAsStandardConfigOptions(t *testing.T) {
	f, err := os.Open("testdata/wire/1.0.50/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	counts := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for line := 1; sc.Scan(); line++ {
		var frame struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(sc.Bytes(), &frame); err != nil {
			t.Fatalf("line %d: %v", line, err)
		}
		var opts []acp.SessionConfigOption
		var kind string
		switch {
		case frame.Method == "session/update":
			var n acp.SessionNotification
			if err := json.Unmarshal(frame.Params, &n); err != nil {
				t.Fatalf("line %d: session/update: %v", line, err)
			}
			if n.Update.ConfigOptionUpdate == nil {
				continue
			}
			kind, opts = "config_option_update", n.Update.ConfigOptionUpdate.ConfigOptions
		case len(frame.Result) > 0:
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(frame.Result, &keys); err != nil {
				t.Fatalf("line %d: result: %v", line, err)
			}
			switch _, hasSession := keys["sessionId"]; {
			case len(keys) == 1:
				var r acp.SetSessionConfigOptionResponse
				if err := json.Unmarshal(frame.Result, &r); err != nil {
					t.Fatalf("line %d: set_config_option answer: %v", line, err)
				}
				kind, opts = "set_config_option", r.ConfigOptions
			case hasSession:
				var r acp.NewSessionResponse
				if err := json.Unmarshal(frame.Result, &r); err != nil {
					t.Fatalf("line %d: session/new answer: %v", line, err)
				}
				kind, opts = "session/new", r.ConfigOptions
			default:
				var r acp.LoadSessionResponse
				if err := json.Unmarshal(frame.Result, &r); err != nil {
					t.Fatalf("line %d: session/load answer: %v", line, err)
				}
				kind, opts = "session/load", r.ConfigOptions
			}
		default:
			continue // a -32602 refusal
		}
		counts[kind]++
		for _, category := range []acp.SessionConfigOptionCategory{
			acp.SessionConfigOptionCategoryModel, acp.SessionConfigOptionCategoryThoughtLevel,
		} {
			found := false
			for _, o := range opts {
				if o.Select != nil && o.Select.Category != nil && *o.Select.Category == category {
					found = true
				}
			}
			if !found {
				t.Errorf("line %d (%s): no %s select option", line, kind, category)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"session/new", "session/load", "set_config_option", "config_option_update"} {
		if counts[kind] == 0 {
			t.Errorf("fixture has no %s frame; counts %v", kind, counts)
		}
	}
}
