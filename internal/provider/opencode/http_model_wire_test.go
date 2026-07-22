package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Unlike the map-shape tests, this drives the real Create/Prompt request
// builders through the dialect and asserts the JSON keys that actually go on the
// wire. OpenCode 1.18 uses different model keys per endpoint (create: "id",
// prompt: "modelID"); a refactor that unifies them in http.go must fail here.
func TestCreateAndPromptModelKeysOnWire(t *testing.T) {
	type call struct {
		method, path string
		body         map[string]any
	}
	var calls []call

	h := &captureHost{
		model: "opencode/gpt-5-nano",
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
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
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

	// Create: POST /session with model {providerID, id}.
	create := modelOf("/session?")
	if _, ok := create["id"]; !ok {
		t.Errorf("create model missing \"id\" key: %+v", create)
	}
	if _, ok := create["modelID"]; ok {
		t.Errorf("create model must not use \"modelID\" key: %+v", create)
	}
	if create["id"] != "gpt-5-nano" || create["providerID"] != "opencode" {
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
	if prompt["modelID"] != "gpt-5-nano" || prompt["providerID"] != "opencode" {
		t.Errorf("prompt model = %+v", prompt)
	}
}
