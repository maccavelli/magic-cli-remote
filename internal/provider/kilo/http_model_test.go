package kilo

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Kilo (an OpenCode fork) inherits the *different* model field names per
// endpoint (MADR 0075 §2.4):
//
//	POST /session                 → model: {providerID, id}
//	POST /session/{id}/prompt_async → model: {providerID, modelID}
//
// Confirmed against the kilo 7.4.20 OpenAPI dump and live spike: swapping
// either key is BadRequest. TestSplitModel from opencode's http_model_test.go
// is not ported — kilo/dialect_test.go:TestSplitModelFirstSlash already
// covers the same ground including the bare-name fallback (MADR 0076 plan
// P8 step 4).

func TestCreateModelBodyUsesID(t *testing.T) {
	body := map[string]string{"providerID": "kilo", "id": "kilo-auto/free"}
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
	if decoded["id"] != "kilo-auto/free" || decoded["providerID"] != "kilo" {
		t.Fatalf("create model body = %v", decoded)
	}
}

func TestPromptModelBodyUsesModelID(t *testing.T) {
	body := map[string]string{"providerID": "kilo", "modelID": "kilo-auto/free"}
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
	if decoded["modelID"] != "kilo-auto/free" || decoded["providerID"] != "kilo" {
		t.Fatalf("prompt model body = %v", decoded)
	}
}

// Compile-time documentation of the shapes used by Create vs Prompt so a
// future "unify them" refactor fails a test instead of shipping a dead
// path again.
func TestCreateAndPromptModelShapesDiffer(t *testing.T) {
	create := map[string]string{"providerID": "kilo", "id": "kilo-auto/free"}
	prompt := map[string]string{"providerID": "kilo", "modelID": "kilo-auto/free"}
	cb, _ := json.Marshal(create)
	pb, _ := json.Marshal(prompt)
	if string(cb) == string(pb) {
		t.Fatal("create and prompt model bodies must differ (id vs modelID)")
	}
}

// Unlike the map-shape tests above, this drives the real Create/Prompt request
// builders through the dialect and asserts the JSON keys that actually go on
// the wire. A refactor that unifies them in session.go must fail here.
func TestCreateAndPromptModelKeysOnWire(t *testing.T) {
	type call struct {
		method, path string
		body         map[string]any
	}
	var calls []call

	h := &captureHost{
		model: "kilo/kilo-auto/free",
		api: func(_ context.Context, method, path string, body, out any) error {
			var m map[string]any
			if body != nil {
				b, _ := json.Marshal(body)
				_ = json.Unmarshal(b, &m)
			}
			calls = append(calls, call{method, path, m})
			// Create decodes a session id from the response.
			if out != nil {
				resp, _ := json.Marshal(map[string]string{"id": "ses_new"})
				_ = json.Unmarshal(resp, out)
			}
			return nil
		},
	}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "kilo", defaultModelID: defaultModelID}
	s := d.NewSession(h).(*httpSession)
	ctx := context.Background()

	if _, err := s.Create(ctx, provider.StartOptions{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}

	modelOf := func(pathSubstr string) map[string]any {
		t.Helper()
		for _, c := range calls {
			if strings.Contains(c.path, pathSubstr) {
				model, ok := c.body["model"].(map[string]any)
				if !ok {
					t.Fatalf("%s call had no model object: %+v", pathSubstr, c.body)
				}
				return model
			}
		}
		t.Fatalf("no API call matched %q; calls=%+v", pathSubstr, calls)
		return nil
	}

	// Create: POST /session with model {providerID, id}. Model id contains a
	// slash itself ("kilo-auto/free"), so this also proves the wire model
	// object carries the id verbatim rather than being re-split (PD3).
	create := modelOf("/session?")
	if _, ok := create["id"]; !ok {
		t.Errorf("create model missing \"id\" key: %+v", create)
	}
	if _, ok := create["modelID"]; ok {
		t.Errorf("create model must not use \"modelID\" key: %+v", create)
	}
	if create["id"] != "kilo-auto/free" || create["providerID"] != "kilo" {
		t.Errorf("create model = %+v", create)
	}

	// Prompt: POST /session/{id}/prompt_async with model {providerID, modelID}.
	prompt := modelOf("prompt_async")
	if _, ok := prompt["modelID"]; !ok {
		t.Errorf("prompt model missing \"modelID\" key: %+v", prompt)
	}
	if _, ok := prompt["id"]; ok {
		t.Errorf("prompt model must not use \"id\" key: %+v", prompt)
	}
	if prompt["modelID"] != "kilo-auto/free" || prompt["providerID"] != "kilo" {
		t.Errorf("prompt model = %+v", prompt)
	}
}
