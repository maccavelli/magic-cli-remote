//go:build live_opencode

package opencode_test

// The engine facts acp_auth.go rests on (0214 PLAN P6b Deviation 1), pinned
// against opencode v2 in an isolated home. The key is made up: connect/key only
// stores it, and no provider is contacted.
//
// Run with: go test -tags live_opencode ./internal/provider/opencode/ -run LiveV2Integrations -count=1 -v

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// madeUpKey is shaped like an OpenRouter key and is not one.
const madeUpKey = "sk-or-v1-00000000000000000000000000000000"

// TestLiveV2IntegrationsWaitForActivation: in a location not used before,
// integration.get answers 404 for an integration that exists until plugins
// have activated there, and only the list waits for that. After a list, get
// answers, a connect adds a credential, and the credential's removal shows in
// get.
func TestLiveV2IntegrationsWaitForActivation(t *testing.T) {
	h := newV2Home(t)
	c := startV2Control(t, h)
	at := func(name string) string {
		dir := filepath.Join(h.home, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		c.dir = dir
		return "?location%5Bdirectory%5D=" + url.QueryEscape(dir)
	}
	credentials := func(q string) []map[string]any {
		t.Helper()
		st, _, raw := c.do("GET", "/api/integration/openrouter"+q, nil, true)
		if st != http.StatusOK {
			t.Fatalf("GET /api/integration/openrouter: %d %s", st, raw)
		}
		var out []map[string]any
		in, _ := data(raw).(map[string]any)
		conns, _ := in["connections"].([]any)
		for _, x := range conns {
			if m, _ := x.(map[string]any); m["type"] == "credential" {
				out = append(out, m)
			}
		}
		return out
	}

	// Until plugins have activated in a location the get answers 404
	// IntegrationNotFoundError, and once they have, 200. Which the first get
	// meets is a race with activation: the Linux test host has seen both, on
	// one command (0214 PLAN P9 Deviation 2). Either is the fact; anything
	// else is not.
	q := at("cold")
	switch st, _, raw := c.do("GET", "/api/integration/openrouter"+q, nil, true); {
	case st == http.StatusNotFound && strings.Contains(string(raw), "IntegrationNotFoundError"):
	case st == http.StatusOK && strings.Contains(string(raw), `"id":"openrouter"`):
	default:
		t.Errorf("the first get in a new location answered %d %s; 2.2.3 answers 404 IntegrationNotFoundError "+
			"until plugins activate there, and 200 once they have. If opencode changed this, acp_auth.go's "+
			"list-first ordering can be revisited", st, raw)
	}

	q = at("warm")
	st, _, raw := c.do("GET", "/api/integration"+q, nil, true)
	if st != http.StatusOK {
		t.Fatalf("GET /api/integration: %d %s", st, raw)
	}
	if list, _ := data(raw).([]any); len(list) == 0 {
		t.Fatalf("the list is empty: %s", raw)
	}
	if got := credentials(q); len(got) != 0 {
		t.Fatalf("a new home holds credentials: %v", got)
	}
	if st, _, raw := c.do("POST", "/api/integration/openrouter/connect/key"+q, map[string]any{"key": madeUpKey}, true); st != http.StatusNoContent {
		t.Fatalf("connect/key after a list: %d %s", st, raw)
	}
	got := credentials(q)
	if len(got) != 1 || got[0]["method"] != "key" {
		b, _ := json.Marshal(got)
		t.Fatalf("after the connect, credentials %s; want one key", b)
	}
	id, _ := got[0]["id"].(string)
	if st, _, raw := c.do("DELETE", "/api/credential/"+url.PathEscape(id), nil, true); st != http.StatusNoContent {
		t.Fatalf("DELETE the credential: %d %s", st, raw)
	}
	if got := credentials(q); len(got) != 0 {
		t.Errorf("after the removal, credentials %v; want none", got)
	}
	if code := c.close(); code != 0 {
		t.Errorf("the control plane exited %d after its lease closed, want 0", code)
	}
}
