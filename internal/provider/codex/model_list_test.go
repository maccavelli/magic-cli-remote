package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// codexLike answers model/list the way codex-cli 0.145.0 actually does, probed
// 2026-07-28 (MADR 0043 §2.2):
//
//   - a request whose params are absent or null is refused with -32600
//     "Invalid request: missing field `params`";
//   - the reply array is `data`, with `nextCursor` for paging.
//
// Both of those were wrong in the daemon at once, and because every failure
// path returned the same empty static catalog, the result was a picker that
// looked like a codex limitation rather than a bug.
func codexLike(pages ...string) rpcSender {
	page := 0
	return func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method != "model/list" {
			return nil, fmt.Errorf("unexpected method %q", method)
		}
		if params == nil {
			return nil, fmt.Errorf("-32600 Invalid request: missing field `params`")
		}
		if page >= len(pages) {
			return nil, fmt.Errorf("no page %d", page)
		}
		out := pages[page]
		page++
		return json.RawMessage(out), nil
	}
}

const onePage = `{"data":[
 {"id":"gpt-5.6-sol","displayName":"GPT-5.6-Sol","description":"Latest frontier agentic coding model.","hidden":false,"isDefault":true,"defaultReasoningEffort":"low","inputModalities":["text","image"]},
 {"id":"gpt-5.6-terra","displayName":"GPT-5.6-Terra","description":"Balanced.","hidden":false,"isDefault":false,"defaultReasoningEffort":"medium","inputModalities":["text","image"]},
 {"id":"internal-only","displayName":"Internal","hidden":true,"isDefault":false}
]}`

func optionIDs(cat picker.Catalog) []string {
	out := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestListModelsDecodesDataField is the regression guard for the field name.
// A reply shaped `{"models":…}` — what the daemon used to decode — must not
// produce a catalog, so this test fails the moment the decode drifts back.
func TestListModelsDecodesDataField(t *testing.T) {
	cat, err := listModelsVia(context.Background(), codexLike(onePage), "", silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	if len(ids) != 2 {
		t.Fatalf("options = %v, want the two visible models", ids)
	}
	if cat.Source != picker.SourceLive {
		t.Errorf("source = %q, want live — a decoded engine answer is not a static fallback", cat.Source)
	}
	if len(cat.DefaultIDs) != 1 || cat.DefaultIDs[0] != "gpt-5.6-sol" {
		t.Errorf("default = %v, want the isDefault model", cat.DefaultIDs)
	}
	if !cat.AllowCustom {
		t.Error("allow_custom must stay on so a known id is never blocked by the catalog")
	}
}

// TestListModelsSendsParamsObject pins the other half: codex refuses a request
// with no params, and the daemon must never send one.
func TestListModelsSendsParamsObject(t *testing.T) {
	var seen any
	send := func(_ context.Context, _ string, params any) (json.RawMessage, error) {
		seen = params
		return json.RawMessage(onePage), nil
	}
	if _, err := listModelsVia(context.Background(), send, "", silentLogger()); err != nil {
		t.Fatal(err)
	}
	if seen == nil {
		t.Fatal("model/list was sent with nil params; codex answers -32600")
	}
	if _, ok := seen.(map[string]any); !ok {
		t.Fatalf("params = %T, want a JSON object", seen)
	}
}

// TestListModelsSkipsHidden: codex marks internal entries hidden, and they must
// not reach a user's picker.
func TestListModelsSkipsHidden(t *testing.T) {
	cat, _ := listModelsVia(context.Background(), codexLike(onePage), "", silentLogger())
	for _, id := range optionIDs(cat) {
		if id == "internal-only" {
			t.Fatal("hidden model reached the catalog")
		}
	}
}

// TestListModelsFollowsCursor covers paging.
func TestListModelsFollowsCursor(t *testing.T) {
	first := `{"data":[{"id":"a","displayName":"A","isDefault":true}],"nextCursor":"c1"}`
	second := `{"data":[{"id":"b","displayName":"B"}]}`
	cat, err := listModelsVia(context.Background(), codexLike(first, second), "", silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("options = %v, want both pages in order", ids)
	}
}

// TestListModelsStopsOnRepeatedCursor bounds a looping cursor: an engine that
// keeps returning the same cursor must not spin the WS handler.
func TestListModelsStopsOnRepeatedCursor(t *testing.T) {
	calls := 0
	send := func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"data":[{"id":"a"}],"nextCursor":"same"}`), nil
	}
	if _, err := listModelsVia(context.Background(), send, "", silentLogger()); err != nil {
		t.Fatal(err)
	}
	if calls > maxModelListPages {
		t.Fatalf("followed %d pages, cap is %d", calls, maxModelListPages)
	}
}

// TestListModelsConfigModelWins: a daemon config model is the operator's
// pre-session policy, so the picker must not claim the engine's default when
// Start will use the configured one.
func TestListModelsConfigModelWins(t *testing.T) {
	cat, _ := listModelsVia(context.Background(), codexLike(onePage), "gpt-5.6-terra", silentLogger())
	if len(cat.DefaultIDs) != 1 || cat.DefaultIDs[0] != "gpt-5.6-terra" {
		t.Fatalf("default = %v, want the configured model", cat.DefaultIDs)
	}
	if optionIDs(cat)[0] != "gpt-5.6-terra" {
		t.Errorf("current model not ranked first: %v", optionIDs(cat))
	}
}

// TestListModelsFallsBackOnError keeps free-text working when the engine says
// no, and keeps the fallback distinguishable from a live answer.
func TestListModelsFallsBackOnError(t *testing.T) {
	send := func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, fmt.Errorf("engine down")
	}
	cat, err := listModelsVia(context.Background(), send, "pinned", silentLogger())
	if err != nil {
		t.Fatalf("a catalog outage must not fail the request: %v", err)
	}
	if cat.Source != picker.SourceStatic {
		t.Errorf("source = %q, want static so the client badges it offline", cat.Source)
	}
	if !cat.AllowCustom {
		t.Error("allow_custom must survive a catalog outage")
	}
}

// TestListModelsEmptyAnswerIsStatic: an engine that answers with zero models is
// reported as a fallback, because an empty live catalog and a broken one look
// identical to a user.
func TestListModelsEmptyAnswerIsStatic(t *testing.T) {
	cat, _ := listModelsVia(context.Background(), codexLike(`{"data":[]}`), "", silentLogger())
	if cat.Source != picker.SourceStatic {
		t.Errorf("source = %q, want static", cat.Source)
	}
}
