package acphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// The shape goose 1.44.0 really reports at session/new (MADR 0043 §2.3): a
// `provider` select of 71 values and a `model` select already scoped to the
// current one.
func googleModelOption() event.ConfigOption {
	return event.ConfigOption{
		ID:           "model",
		Name:         "Model",
		Kind:         "select",
		CurrentValue: "gemini-3.6-flash",
		Values: []event.ConfigOptionValue{
			{ID: "gemini-3.5-flash-lite", Name: "Gemini 3.5 Flash Lite"},
			{ID: "gemini-3.6-flash", Name: "Gemini 3.6 Flash"},
			{ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash"},
		},
	}
}

func providerOption() event.ConfigOption {
	return event.ConfigOption{
		ID:           "provider",
		Name:         "Provider",
		Kind:         "select",
		CurrentValue: "google",
		Values: []event.ConfigOptionValue{
			{ID: "anthropic", Name: "Anthropic"},
			{ID: "google", Name: "Google"},
			{ID: "openai", Name: "OpenAI"},
		},
	}
}

func ids(cat picker.Catalog) []string {
	out := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestModelCatalogQualifiesIDs is the core of MADR 0043 D6. Goose model ids are
// only meaningful inside a provider — `gpt-4o` exists under `openai` and not
// under `google` — so an unqualified id chosen at create-session time would be
// applied against whatever provider the engine happened to be on.
func TestModelCatalogQualifiesIDs(t *testing.T) {
	cat := modelCatalogFromOption(googleModelOption(), "google")
	for _, id := range ids(cat) {
		if len(id) < 7 || id[:7] != "google/" {
			t.Fatalf("model id %q is not provider-qualified", id)
		}
	}
	if len(cat.DefaultIDs) != 1 || cat.DefaultIDs[0] != "google/gemini-3.6-flash" {
		t.Fatalf("default = %v, want the qualified current value", cat.DefaultIDs)
	}
	if !cat.AllowCustom {
		t.Error("allow_custom must stay on so a known id is never blocked")
	}
}

// TestModelCatalogKeepsEngineOrder: goose reports no release dates, so ranking
// its models by anything but the engine's own order would be a recency claim
// the data does not support. Only the current model is pinned first.
func TestModelCatalogKeepsEngineOrder(t *testing.T) {
	cat := modelCatalogFromOption(googleModelOption(), "google")
	want := []string{
		"google/gemini-3.6-flash", // current, pinned
		"google/gemini-3.5-flash-lite",
		"google/gemini-2.0-flash",
	}
	got := ids(cat)
	if len(got) != len(want) {
		t.Fatalf("options = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("options = %v, want %v", got, want)
		}
	}
}

// TestModelCatalogWithoutProviderLeavesIDsBare covers an agent that reports a
// model option but no provider one: qualifying with an empty provider would
// produce ids beginning with "/".
func TestModelCatalogWithoutProviderLeavesIDsBare(t *testing.T) {
	cat := modelCatalogFromOption(googleModelOption(), "")
	for _, id := range ids(cat) {
		if id[0] == '/' {
			t.Fatalf("id %q was qualified with an empty provider", id)
		}
	}
}

// TestProviderCatalogMarksOnlyTheCurrentConnected: the engine lists every
// provider it can speak to, not the ones with credentials, and offers no way to
// tell them apart. Marking more than the current one connected would put a
// green light on providers that fail the moment they are selected.
func TestProviderCatalogMarksOnlyTheCurrentConnected(t *testing.T) {
	cat := providerCatalogFromOption(providerOption())
	if got := ids(cat); len(got) == 0 || got[0] != "google" {
		t.Fatalf("providers = %v, want the current one first", got)
	}
	connected := 0
	for _, o := range cat.Options {
		if o.Meta[picker.MetaConnected] == "true" {
			connected++
			if o.ID != "google" {
				t.Errorf("%q marked connected but is not the current provider", o.ID)
			}
		}
	}
	if connected != 1 {
		t.Errorf("connected = %d, want exactly the current provider", connected)
	}
	if cat.AllowCustom {
		t.Error("a free-typed provider id is meaningless; allow_custom must be off")
	}
}

func TestSplitQualifiedModel(t *testing.T) {
	for _, tc := range []struct{ in, mp, id string }{
		{"openai/gpt-5.6-sol", "openai", "gpt-5.6-sol"},
		{"gpt-4o", "", "gpt-4o"},
		{"", "", ""},
		// A model id containing a slash keeps everything after the first one,
		// which is how opencode ids look too.
		{"vertex/publishers/google/x", "vertex", "publishers/google/x"},
	} {
		mp, id := splitQualifiedModel(tc.in)
		if mp != tc.mp || id != tc.id {
			t.Errorf("split(%q) = (%q, %q), want (%q, %q)", tc.in, mp, id, tc.mp, tc.id)
		}
	}
}

// stubFramer records set_config_option calls in order.
type stubFramer struct {
	calls   []map[string]any
	failOn  string
	failErr error
}

func (f *stubFramer) sendRequest(_ context.Context, method string, params any) (json.RawMessage, error) {
	if method != "session/set_config_option" {
		return json.RawMessage(`{}`), nil
	}
	m, _ := params.(map[string]any)
	f.calls = append(f.calls, m)
	if f.failOn != "" && m["configId"] == f.failOn {
		return nil, f.failErr
	}
	return json.RawMessage(`{}`), nil
}

func (f *stubFramer) sendResponse(context.Context, json.RawMessage, any, *rpcErrorBody) error {
	return nil
}

func (f *stubFramer) sendNotification(context.Context, string, any) error { return nil }

func sessionWithFramer(f *stubFramer) *session {
	p := &Provider{
		spec: Spec{ID: "goose", DefaultBin: "goose"},
		cfg:  Config{Bin: "goose"},
		log:  slog.Default(),
		fr:   f,
	}
	return &session{p: p, agentID: "s1", log: slog.Default()}
}

// TestApplyModelSetsProviderFirst: goose re-scopes its model list when the
// provider changes, so setting `model` before `provider` applies an id the
// engine is about to stop offering.
func TestApplyModelSetsProviderFirst(t *testing.T) {
	f := &stubFramer{}
	s := sessionWithFramer(f)
	if err := s.applyModel(context.Background(), "openai/gpt-5.6-sol"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls = %d, want provider then model", len(f.calls))
	}
	if f.calls[0]["configId"] != "provider" || f.calls[0]["value"] != "openai" {
		t.Errorf("first call = %v, want provider=openai", f.calls[0])
	}
	if f.calls[1]["configId"] != "model" || f.calls[1]["value"] != "gpt-5.6-sol" {
		t.Errorf("second call = %v, want model=gpt-5.6-sol", f.calls[1])
	}
}

// TestApplyModelUnqualifiedTouchesOnlyModel keeps the pre-0043 meaning for a
// bare id, which is what a persisted session or a hand-typed value carries.
func TestApplyModelUnqualifiedTouchesOnlyModel(t *testing.T) {
	f := &stubFramer{}
	s := sessionWithFramer(f)
	if err := s.applyModel(context.Background(), "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0]["configId"] != "model" {
		t.Fatalf("calls = %v, want only a model set", f.calls)
	}
}

// TestApplyModelTranslatesCredentialFailure: goose answers an unconfigured
// provider with a bare `-32603 Internal error` and no detail, which the user
// cannot act on. The daemon says what to do about it instead.
func TestApplyModelTranslatesCredentialFailure(t *testing.T) {
	f := &stubFramer{failOn: "provider", failErr: fmt.Errorf("JSON-RPC error -32603: Internal error")}
	s := sessionWithFramer(f)
	err := s.applyModel(context.Background(), "anthropic/claude")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"anthropic", "credentials", "configure"} {
		if !contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
	// And the model must not have been set against the wrong provider.
	for _, c := range f.calls {
		if c["configId"] == "model" {
			t.Error("model was set even though the provider switch failed")
		}
	}
}

func TestSetModelRejectsEmpty(t *testing.T) {
	s := sessionWithFramer(&stubFramer{})
	if err := s.SetModel(context.Background(), "  "); err == nil {
		t.Fatal("expected an error for an empty model")
	}
}

// TestRetainConfigOptionsMerges: goose re-emits only what changed, so replacing
// the snapshot wholesale would drop the `provider` option the moment a `model`
// change arrived on its own — and with it the provider step.
func TestRetainConfigOptionsMerges(t *testing.T) {
	p := &Provider{log: slog.Default()}
	s := &session{p: p, log: slog.Default()}
	s.retainConfigOptions([]event.ConfigOption{providerOption(), googleModelOption()})
	s.retainConfigOptions([]event.ConfigOption{{
		ID: "model", Kind: "select", CurrentValue: "gemini-2.0-flash",
		Values: googleModelOption().Values,
	}})

	if _, ok := s.configOption("provider"); !ok {
		t.Fatal("provider option was dropped by a model-only update")
	}
	m, ok := s.configOption("model")
	if !ok || m.CurrentValue != "gemini-2.0-flash" {
		t.Fatalf("model option = %+v, want the updated current value", m)
	}
	// The provider-level cache is refreshed for free by any live session.
	if len(p.cachedConfigOptions()) != 2 {
		t.Fatalf("provider cache = %v, want both options", p.cachedConfigOptions())
	}
}

// TestSessionModelCatalogIsProviderScoped is what makes an in-session /model
// picker show the right list.
func TestSessionModelCatalogIsProviderScoped(t *testing.T) {
	p := &Provider{log: slog.Default()}
	s := &session{p: p, log: slog.Default()}
	s.retainConfigOptions([]event.ConfigOption{providerOption(), googleModelOption()})

	cat, err := s.ModelCatalog(context.Background(), "models")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(cat); len(got) == 0 || got[0] != "google/gemini-3.6-flash" {
		t.Fatalf("models = %v, want google's, current first", got)
	}
	provCat, err := s.ModelCatalog(context.Background(), "providers")
	if err != nil {
		t.Fatal(err)
	}
	if len(provCat.Options) != 3 {
		t.Fatalf("providers = %v", ids(provCat))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
