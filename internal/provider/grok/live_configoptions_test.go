//go:build live_grok

package grok_test

import "testing"

// Run with: go test -tags live_grok ./internal/provider/grok/ -run AdvertisesStandardConfigOptions -count=1
//
// PLAN 0184 A3 step 6: grok 1.0.50 answers session/new with standard `model`
// and `thought_level` select config options, so acpagent's standard
// session/set_config_option path drives it and grok's Spec fallbacks do not
// run. grok 1.0.13 offered none. Fixture: testdata/wire/1.0.50/frames.jsonl.
// Opens a session only; spends no tokens.
func TestLiveGrokAdvertisesStandardConfigOptions(t *testing.T) {
	p := startACP(t, nil)
	res := rpcResult(t, p, 2)
	opts, _ := res["configOptions"].([]any)
	byCategory := map[string]string{}
	for _, raw := range opts {
		o, _ := raw.(map[string]any)
		if o["type"] != "select" {
			continue
		}
		category, _ := o["category"].(string)
		id, _ := o["id"].(string)
		byCategory[category] = id
	}
	for _, want := range []string{"model", "thought_level"} {
		if byCategory[want] == "" {
			t.Errorf("session/new offers no %q select option (got %v); acpagent falls back to grok's untyped methods", want, byCategory)
		}
	}
}
