package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigProvenanceProjectsLayersWithoutPathsOrValues(t *testing.T) {
	raw := []byte(`{"config":{"default_permissions":"team","approvals_reviewer":"auto_review","developer_instructions":"secret"},"origins":{"default_permissions":{"name":{"type":"project","dotCodexFolder":"/secret/repo/.codex"},"version":"v-project"},"approvals_reviewer":{"name":{"type":"system","file":"/etc/codex/config.toml"},"version":"v-managed"}},"layers":[{"name":{"type":"user","file":"/home/me/.codex/config.toml"},"version":"v-user","config":{"token":"secret"}},{"name":{"type":"system","file":"/etc/codex/config.toml"},"version":"v-managed","config":{"approvals_reviewer":"user"}}]}`)
	got, err := projectConfigState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestedProfileID != "team" || got.EffectiveReviewer != "auto_review" {
		t.Fatalf("state = %+v", got)
	}
	if len(got.Layers) != 2 || got.Layers[0].Kind != "user" || got.Layers[1].Kind != "managed" {
		t.Fatalf("layers = %+v", got.Layers)
	}
	b, _ := json.Marshal(got)
	for _, forbidden := range []string{"/secret", "/home/me", "/etc/codex", "developer_instructions", "token"} {
		if json.Valid(b) && strings.Contains(string(b), forbidden) {
			t.Fatalf("projection leaked %q: %s", forbidden, b)
		}
	}
}

func TestConfigWriteRequestIsAtomicVersionedAndHotReloaded(t *testing.T) {
	params, err := buildPermissionConfigWrite("team", "auto_review", "v7")
	if err != nil {
		t.Fatal(err)
	}
	if params["expectedVersion"] != "v7" || params["reloadUserConfig"] != true {
		t.Fatalf("write envelope = %+v", params)
	}
	edits := params["edits"].([]map[string]any)
	if len(edits) != 2 || edits[0]["keyPath"] != "default_permissions" || edits[1]["keyPath"] != "approvals_reviewer" {
		t.Fatalf("edits = %+v", edits)
	}
	if _, err := buildPermissionConfigWrite("team", "bogus", "v7"); err == nil {
		t.Fatal("invalid reviewer accepted")
	}
}
