package opencode_test

// opencode's ACP frames through acpagent's hooks (MADR 0214 D14; 0208 PLAN
// P6c). The child updates and prompt results are G1's recording
// (testdata/wire/2.2.3); the retry frames are transcribed from opencode's
// source, because G1 saw no retry. P4's Spec builds its handlers on these.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// childUpdateMethod is opencode's notification for a child session's output.
// Its name has no leading `_`.
const childUpdateMethod = "opencode/session/child_update"

// retryKey is opencode's retry marker (translate.ts:19 at b44b7164ba).
const retryKey = "opencode/retry"

// Frames transcribed from opencode's source at b44b7164ba, not recorded: G1
// saw no retry (0214-PLAN P2 Deviation 2). The shape is translate.ts:268-272
// with SessionStructuredError (packages/client/src/promise/generated/types.ts:
// 114-120); the values are illustrative. A recording replaces them when one
// occurs.
const (
	// translate.ts:275: a retry is scheduled.
	retryValue     = `{"attempt":2,"nextRetryAt":"2026-11-01T12:00:05.000Z","error":{"type":"api","message":"Rate limited","status":429,"retryAfterMs":5000}}`
	retryScheduled = `{"sessionUpdate":"session_info_update","_meta":{"` + retryKey + `":` + retryValue + `}}`
	// translate.ts:265: the next step started, so the retry is cleared.
	retryCleared = `{"sessionUpdate":"session_info_update","_meta":{"` + retryKey + `":null}}`
	// translate.ts:166: a turn interrupted while a retry was pending.
	retryAtInterrupt = `{"stopReason":"cancelled","_meta":{"` + retryKey + `":` + retryValue + `}}`
)

// g1 is one message of G1's recording.
type g1 struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

// g1Lines returns the lines of G1's recording that keep says to, in order.
func g1Lines(t *testing.T, keep func(g1) bool) []string {
	t.Helper()
	f, err := os.Open("testdata/wire/2.2.3/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var m g1
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if keep(m) {
			out = append(out, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// recorder keeps a copy of each value a hook is given.
type recorder struct {
	mu   sync.Mutex
	got  []json.RawMessage
	from []acpagent.MetaSource
}

func (r *recorder) note(_ context.Context, _ acpagent.Host, params json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, bytes.Clone(params))
}

func (r *recorder) meta(_ context.Context, _ acpagent.Host, from acpagent.MetaSource, value json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, bytes.Clone(value))
	r.from = append(r.from, from)
}

func (r *recorder) calls() ([]json.RawMessage, []acpagent.MetaSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]json.RawMessage(nil), r.got...), append([]acpagent.MetaSource(nil), r.from...)
}

func start(t *testing.T, spec acpagent.Spec) (provider.Session, *acptest.Agent) {
	t.Helper()
	spec.ID = provider.IDOpencode
	return acptest.Start(t, acpagent.New(spec, acpagent.Config{}), acptest.Options{})
}

// barrier sends a mode update and waits for it: the agent's lines before it
// have then been handled.
func barrier(t *testing.T, s provider.Session, a *acptest.Agent) {
	t.Helper()
	a.Send(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":{"sessionUpdate":"current_mode_update","currentModeId":"plan"}}}`)
	acptest.Next(t, s, event.TypeMode)
}

// promptOnce runs one turn whose answer is result, and waits for it to end.
func promptOnce(t *testing.T, s provider.Session, a *acptest.Agent, result string) {
	t.Helper()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		t.Fatal(err)
	}
	a.Respond(a.Await("session/prompt"), v)
	acptest.Next(t, s, event.TypeTurnComplete)
}

// jsonEqual reports whether a and b are the same JSON value.
func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

// TestG1ChildUpdatesReachTheHandler: the seven child_update notifications G1
// recorded reach the handler registered for opencode's name, each with its
// params as sent.
func TestG1ChildUpdatesReachTheHandler(t *testing.T) {
	frames := g1Lines(t, func(m g1) bool { return m.Method == childUpdateMethod })
	if len(frames) != 7 {
		t.Fatalf("G1 recorded %d child_update frames, want 7", len(frames))
	}
	rec := &recorder{}
	s, a := start(t, acpagent.Spec{ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
		childUpdateMethod: rec.note,
	}})
	for _, f := range frames {
		a.Send(f)
	}
	barrier(t, s, a)
	got, _ := rec.calls()
	if len(got) != len(frames) {
		t.Fatalf("the handler was called %d time(s), want %d", len(got), len(frames))
	}
	for i, f := range frames {
		var m g1
		if err := json.Unmarshal([]byte(f), &m); err != nil {
			t.Fatal(err)
		}
		if !jsonEqual(t, got[i], m.Params) {
			t.Errorf("call %d params = %s, want %s", i, got[i], m.Params)
		}
	}
}

// TestG1PromptResultsCallNoMetaHandler: G1's four recorded prompt results,
// each with `_meta: {}`, call no handler for opencode's retry marker.
func TestG1PromptResultsCallNoMetaHandler(t *testing.T) {
	var results []string
	for _, f := range g1Lines(t, func(m g1) bool { return m.Method == "" && len(m.Result) > 0 }) {
		var m g1
		_ = json.Unmarshal([]byte(f), &m)
		var r struct {
			StopReason string `json:"stopReason"`
		}
		if json.Unmarshal(m.Result, &r) == nil && r.StopReason != "" {
			results = append(results, string(m.Result))
		}
	}
	if len(results) != 4 {
		t.Fatalf("G1 recorded %d prompt results, want 4", len(results))
	}
	rec := &recorder{}
	s, a := start(t, acpagent.Spec{MetaHandlers: map[string]acpagent.MetaHandler{retryKey: rec.meta}})
	for _, r := range results {
		promptOnce(t, s, a, r)
	}
	if got, _ := rec.calls(); len(got) != 0 {
		t.Fatalf("the handler was called %d time(s); every G1 _meta is empty", len(got))
	}
}

// TestRetryMarkerReachesItsHandler: opencode's retry marker, scheduled and
// cleared on session_info_update and pending on an interrupted prompt result,
// reaches the handler registered for it (transcribed frames).
func TestRetryMarkerReachesItsHandler(t *testing.T) {
	rec := &recorder{}
	s, a := start(t, acpagent.Spec{MetaHandlers: map[string]acpagent.MetaHandler{retryKey: rec.meta}})
	for _, u := range []string{retryScheduled, retryCleared} {
		a.Send(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":` + u + `}}`)
	}
	barrier(t, s, a)
	promptOnce(t, s, a, retryAtInterrupt)
	got, from := rec.calls()
	want := []struct {
		from  acpagent.MetaSource
		value string
	}{
		{acpagent.MetaFromSessionInfo, retryValue},
		{acpagent.MetaFromSessionInfo, `null`},
		{acpagent.MetaFromPromptResult, retryValue},
	}
	if len(got) != len(want) {
		t.Fatalf("the handler was called %d time(s), want %d", len(got), len(want))
	}
	for i, w := range want {
		if from[i] != w.from || !jsonEqual(t, got[i], json.RawMessage(w.value)) {
			t.Errorf("call %d = %s %s, want %s %s", i, from[i], got[i], w.from, w.value)
		}
	}
}
