package acpagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// childUpdateMethod is opencode's vendor notification for a child session's
// output (MADR 0214, A7 and D14 as corrected). Its name has no leading `_`.
const childUpdateMethod = "opencode/session/child_update"

// g1Frames returns the lines of 0214-PLAN P3's G1 recording (opencode 2.2.3
// over ACP) whose method is method, in order.
func g1Frames(t *testing.T, method string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "opencode", "testdata", "wire", "2.2.3", "frames.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var m wireMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.Method == method {
			out = append(out, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// paramsRecorder is a notification handler that keeps a copy of each params
// it is given.
type paramsRecorder struct {
	mu  sync.Mutex
	got []json.RawMessage
}

func (r *paramsRecorder) handle(_ context.Context, _ Host, params json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, bytes.Clone(params))
}

func (r *paramsRecorder) calls() []json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]json.RawMessage(nil), r.got...)
}

// barrier sends a mode update and waits until it has reached Magic: the
// agent's lines before it have then been read and handled.
func barrier(t *testing.T, s *session, a *wireAgent) {
	t.Helper()
	a.sendRaw(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":{"sessionUpdate":"current_mode_update","currentModeId":"plan"}}}`)
	deadline := time.After(wireBudget)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeMode {
				return
			}
		case <-deadline:
			t.Fatal("the barrier update never reached Magic")
		}
	}
}

// TestVendorNotificationReachesItsSpecHandler is D14 as corrected again
// (0214-PLAN P2 Deviation 2): the seven child_update notifications G1
// recorded reach the handler a Spec registers for that name, which has no
// leading `_`, each with its params as sent.
func TestVendorNotificationReachesItsSpecHandler(t *testing.T) {
	frames := g1Frames(t, childUpdateMethod)
	if len(frames) != 7 {
		t.Fatalf("G1 recorded %d child_update frames, want 7", len(frames))
	}
	rec := &paramsRecorder{}
	p := &Provider{spec: Spec{ExtensionNotifications: map[string]ExtensionNotificationHandler{
		childUpdateMethod: rec.handle,
	}}}
	s, a := newWireSessionFor(t, "{}", p)
	for _, f := range frames {
		a.sendRaw(f)
	}
	barrier(t, s, a)
	got := rec.calls()
	if len(got) != len(frames) {
		t.Fatalf("the handler was called %d time(s), want %d", len(got), len(frames))
	}
	for i, f := range frames {
		var m wireMsg
		if err := json.Unmarshal([]byte(f), &m); err != nil {
			t.Fatal(err)
		}
		if !jsonEqual(t, got[i], m.Params) {
			t.Errorf("call %d params = %s, want %s", i, got[i], m.Params)
		}
	}
}

// TestVendorRequestIsNotTakenByANotificationHandler: a request under a name
// registered as a notification is answered as before, -32601, and the handler
// is not called; only notifications are delivered.
func TestVendorRequestIsNotTakenByANotificationHandler(t *testing.T) {
	rec := &paramsRecorder{}
	p := &Provider{spec: Spec{ExtensionNotifications: map[string]ExtensionNotificationHandler{
		childUpdateMethod: rec.handle,
	}}}
	_, a := newWireSessionFor(t, "{}", p)
	a.sendRaw(`{"jsonrpc":"2.0","id":77,"method":"` + childUpdateMethod + `","params":{}}`)
	m := a.answerTo("77", wireBudget)
	if m.Error == nil || m.Error.Code != -32601 {
		t.Fatalf("answer = %+v, want -32601", m)
	}
	if n := len(rec.calls()); n != 0 {
		t.Fatalf("the notification handler was called %d time(s) for a request", n)
	}
}

// TestUnregisteredVendorNotificationIsDroppedAsBefore: a vendor notification
// no handler is registered for is dropped, as the SDK drops it today, and the
// connection carries on.
func TestUnregisteredVendorNotificationIsDroppedAsBefore(t *testing.T) {
	rec := &paramsRecorder{}
	p := &Provider{spec: Spec{ExtensionNotifications: map[string]ExtensionNotificationHandler{
		childUpdateMethod: rec.handle,
	}}}
	s, a := newWireSessionFor(t, "{}", p)
	a.sendRaw(`{"jsonrpc":"2.0","method":"opencode/session/other","params":{}}`)
	barrier(t, s, a)
	if n := len(rec.calls()); n != 0 {
		t.Fatalf("the handler was called %d time(s) for another method", n)
	}
}

// TestUnderscoreNotificationIsDeliveredOnce: a `_`-prefixed name keeps its
// route through HandleExtensionMethod, as grok's do, and is not also taken
// by the vendor route.
func TestUnderscoreNotificationIsDeliveredOnce(t *testing.T) {
	rec := &paramsRecorder{}
	p := &Provider{spec: Spec{ExtensionNotifications: map[string]ExtensionNotificationHandler{
		"_x.test/note":    rec.handle,
		childUpdateMethod: func(context.Context, Host, json.RawMessage) {},
	}}}
	s, a := newWireSessionFor(t, "{}", p)
	a.sendRaw(`{"jsonrpc":"2.0","method":"_x.test/note","params":{"n":1}}`)
	barrier(t, s, a)
	if n := len(rec.calls()); n != 1 {
		t.Fatalf("the handler was called %d time(s), want 1", n)
	}
}

// TestNoVendorMethodInstallsNoMiddleware: a Spec that registers no name
// without `_` gets the connection options it had before D14.
func TestNoVendorMethodInstallsNoMiddleware(t *testing.T) {
	base := len(clientConnOptions(nil, nil))
	for name, handlers := range map[string]map[string]ExtensionNotificationHandler{
		"none":       nil,
		"underscore": {"_vendor/models/update": func(context.Context, Host, json.RawMessage) {}},
	} {
		s := &session{extNotificationHandlers: handlers}
		if got := len(clientConnOptions(nil, s)); got != base {
			t.Errorf("%s: %d options, want %d", name, got, base)
		}
	}
	s := &session{extNotificationHandlers: map[string]ExtensionNotificationHandler{
		childUpdateMethod: func(context.Context, Host, json.RawMessage) {},
	}}
	if got := len(clientConnOptions(nil, s)); got != base+1 {
		t.Errorf("vendor: %d options, want %d", got, base+1)
	}
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
