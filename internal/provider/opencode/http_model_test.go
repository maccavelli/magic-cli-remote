package opencode

import (
	"encoding/json"
	"testing"
)

// OpenCode serve 1.18 accepts model as {providerID, id} on both create and
// prompt; modelID is BadRequest. Keep the shapes identical.
func TestModelBodyUsesIDNotModelID(t *testing.T) {
	createBody := map[string]string{"providerID": "opencode", "id": "gpt-5-nano"}
	promptBody := map[string]string{"providerID": "opencode", "id": "gpt-5-nano"}

	cb, _ := json.Marshal(createBody)
	pb, _ := json.Marshal(promptBody)
	if string(cb) != string(pb) {
		t.Fatalf("create/prompt model bodies differ: %s vs %s", cb, pb)
	}
	var decoded map[string]string
	if err := json.Unmarshal(cb, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["modelID"]; ok {
		t.Fatal("must not use modelID key")
	}
	if decoded["id"] != "gpt-5-nano" {
		t.Fatalf("id=%q", decoded["id"])
	}
}

func TestSplitModel(t *testing.T) {
	p, m := splitModel("opencode/gpt-5-nano")
	if p != "opencode" || m != "gpt-5-nano" {
		t.Fatalf("got %q %q", p, m)
	}
	p, m = splitModel("solo")
	// Bare names are treated as opencode Zen models.
	if p != "opencode" || m != "solo" {
		t.Fatalf("solo -> %q %q", p, m)
	}
}
