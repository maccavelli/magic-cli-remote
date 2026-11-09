package gobble

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maccavelli/gobble-cli/acpext"
)

// The release fixture, captured from gobble 0.4.0 (MADR 0179 D12, PLAN P3). It
// holds what gobble sent: wirecap taps the agent's output, so a frame here is
// gobble's answer, notification or request, never one of this repository's.
const fixture = "testdata/wire/" + KnownGoodVersion + "/frames.jsonl"

// frame is one line of the fixture.
type frame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	} `json:"error"`
}

func readFixture(t *testing.T, path string) []frame {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []frame
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var fr frame
		if err := json.Unmarshal(sc.Bytes(), &fr); err != nil {
			t.Fatalf("%s: a line is not JSON: %v", path, err)
		}
		out = append(out, fr)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// sentMethods checks what gobble sent: a notification or request only of
// D6, D7, D9 and D10's methods (item 7). gobble sends no fs/* or terminal/*
// request to a client that offered neither, no _x.ai/* method, and, from
// 0.4.0, no _gobble/compaction to a client that offered session.compaction
// (A2).
func sentMethods(frames []frame) []string {
	allowed := map[string]bool{
		"session/update":             true,
		"session/request_permission": true,
		acpext.MethodPersonality:     true,
		acpext.MethodRetry:           true,
	}
	var bad []string
	for _, f := range frames {
		if f.Method != "" && !allowed[f.Method] {
			bad = append(bad, f.Method)
		}
	}
	return bad
}

// Item 7: the recorded session sends nothing outside D6, D7, D9 and D10.
func TestFixtureSendsOnlyKnownMethods(t *testing.T) {
	if bad := sentMethods(readFixture(t, fixture)); len(bad) != 0 {
		t.Fatalf("gobble sent %v; none is allowed (MADR 0179 D12 item 7)", bad)
	}
}

// strict decodes raw into v with unknown members refused, and reports
// whether v encodes back to the same JSON.
func strict(raw json.RawMessage, v any) (bool, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return false, err
	}
	back, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	var a, b any
	if err := json.Unmarshal(raw, &a); err != nil {
		return false, err
	}
	if err := json.Unmarshal(back, &b); err != nil {
		return false, err
	}
	return reflect.DeepEqual(a, b), nil
}

// gobbleShapes checks every gobble shape in frames against acpext's type
// (item 9): initialize's _meta.gobble, each _gobble/* notification, each clone
// result, each error's data, and each compaction_update's _meta.gobble (A2).
// It returns how many of each it checked.
func gobbleShapes(t *testing.T, frames []frame) map[string]int {
	t.Helper()
	seen := map[string]int{}
	check := func(kind string, raw json.RawMessage, v any) {
		seen[kind]++
		same, err := strict(raw, v)
		if err != nil {
			t.Errorf("%s %s does not decode as %T: %v", kind, raw, v, err)
			return
		}
		if !same {
			t.Errorf("%s %s does not encode back to the same JSON as %T", kind, raw, v)
		}
	}
	for _, f := range frames {
		switch f.Method {
		case acpext.MethodPersonality:
			check(f.Method, f.Params, &acpext.PersonalityNotice{})
		case acpext.MethodRetry:
			check(f.Method, f.Params, &acpext.RetryNotice{})
		case "session/update":
			var p struct {
				Update struct {
					Kind string                     `json:"sessionUpdate"`
					Meta map[string]json.RawMessage `json:"_meta"`
				} `json:"update"`
			}
			if json.Unmarshal(f.Params, &p) == nil && p.Update.Kind == "compaction_update" {
				if raw, ok := p.Update.Meta[acpext.MetaKey]; ok {
					check("compaction_update._meta.gobble", raw, &acpext.CompactionMeta{})
				}
			}
		}
		if f.Error != nil && len(f.Error.Data) > 0 && f.Error.Code != -32601 {
			check("error data", f.Error.Data, &acpext.ErrorData{})
		}
		if len(f.Result) == 0 {
			continue
		}
		var init struct {
			AgentCapabilities struct {
				Meta map[string]json.RawMessage `json:"_meta"`
			} `json:"agentCapabilities"`
		}
		if json.Unmarshal(f.Result, &init) == nil {
			if raw, ok := init.AgentCapabilities.Meta[acpext.MetaKey]; ok {
				check("initialize _meta.gobble", raw, &acpext.CapabilitiesMeta{})
			}
		}
		var keys map[string]json.RawMessage
		if json.Unmarshal(f.Result, &keys) == nil && len(keys) == 1 {
			if _, ok := keys["sessionId"]; ok {
				check("clone result", f.Result, &acpext.CloneResult{})
			}
		}
	}
	return seen
}

// Item 9, with A2: every gobble shape the release sent decodes into its
// acpext type, with unknown members refused, and encodes back unchanged.
func TestFixtureShapesAreAcpexts(t *testing.T) {
	seen := gobbleShapes(t, readFixture(t, fixture))
	for _, kind := range []string{"initialize _meta.gobble", acpext.MethodPersonality, "clone result",
		"error data", "compaction_update._meta.gobble"} {
		if seen[kind] == 0 {
			t.Errorf("the fixture holds no %s; the check would pass without checking it", kind)
		}
	}
}

// The fixture carries the compaction as A2 describes: a live in_progress and
// completed pair with one id, and the replay after the load with the same id,
// tokensBefore and no reason.
func TestFixtureCompactionIsTheStandardUpdate(t *testing.T) {
	type update struct {
		Kind   string `json:"sessionUpdate"`
		ID     string `json:"compactionId"`
		Status string `json:"status"`
		Meta   struct {
			Gobble acpext.CompactionMeta `json:"gobble"`
		} `json:"_meta"`
	}
	var got []update
	for _, f := range readFixture(t, fixture) {
		var p struct {
			Update update `json:"update"`
		}
		if f.Method == "session/update" && json.Unmarshal(f.Params, &p) == nil && p.Update.Kind == "compaction_update" {
			got = append(got, p.Update)
		}
	}
	if len(got) != 3 {
		t.Fatalf("%d compaction updates, want the live pair and the replay: %+v", len(got), got)
	}
	live, done, replay := got[0], got[1], got[2]
	if live.Status != "in_progress" || done.Status != "completed" || replay.Status != "completed" ||
		live.ID != done.ID || replay.ID != done.ID {
		t.Errorf("updates = %+v; want in_progress, completed, completed, one id", got)
	}
	if done.Meta.Gobble.Reason == "" || done.Meta.Gobble.TokensBefore <= 0 {
		t.Errorf("the live completed meta is %+v; want a reason and tokensBefore", done.Meta.Gobble)
	}
	if replay.Meta.Gobble.Reason != "" || replay.Meta.Gobble.TokensBefore != done.Meta.Gobble.TokensBefore {
		t.Errorf("the replay's meta is %+v; want tokensBefore %d and no reason", replay.Meta.Gobble, done.Meta.Gobble.TokensBefore)
	}
}

// The fixture is named for KnownGoodVersion, and its meta.json agrees.
func TestFixtureIsTheKnownGoodRelease(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(fixture), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Provider string `json:"provider"`
		Version  string `json:"version"`
		Frames   int    `json:"frames"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	lines, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(lines), "\n")
	if meta.Provider != "gobble" || meta.Version != KnownGoodVersion || meta.Frames != n {
		t.Fatalf("meta = %+v with %d frames on disk; want gobble %s and the count", meta, n, KnownGoodVersion)
	}
}
