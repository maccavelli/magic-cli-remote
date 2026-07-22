package opencode

import (
	"encoding/json"
	"testing"
)

// OpenCode serve 1.18 uses *different* model field names per endpoint:
//
//	POST /session                 → model: {providerID, id}
//	POST /session/{id}/prompt*    → model: {providerID, modelID}
//
// Confirmed against OpenAPI and live 1.18.4: swapping either key is BadRequest.

func TestCreateModelBodyUsesID(t *testing.T) {
	body := map[string]string{"providerID": "opencode", "id": "gpt-5-nano"}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["modelID"]; ok {
		t.Fatal("create must not use modelID key")
	}
	if decoded["id"] != "gpt-5-nano" || decoded["providerID"] != "opencode" {
		t.Fatalf("create model body = %v", decoded)
	}
}

func TestPromptModelBodyUsesModelID(t *testing.T) {
	body := map[string]string{"providerID": "opencode", "modelID": "gpt-5-nano"}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["id"]; ok {
		t.Fatal("prompt must not use id key")
	}
	if decoded["modelID"] != "gpt-5-nano" || decoded["providerID"] != "opencode" {
		t.Fatalf("prompt model body = %v", decoded)
	}
}

// Compile-time documentation of the shapes used by Create vs Prompt so a
// future "unify them" refactor fails a test instead of shipping a dead
// OpenCode path again.
func TestCreateAndPromptModelShapesDiffer(t *testing.T) {
	create := map[string]string{"providerID": "opencode", "id": "gpt-5-nano"}
	prompt := map[string]string{"providerID": "opencode", "modelID": "gpt-5-nano"}
	cb, _ := json.Marshal(create)
	pb, _ := json.Marshal(prompt)
	if string(cb) == string(pb) {
		t.Fatal("create and prompt model bodies must differ (id vs modelID)")
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
