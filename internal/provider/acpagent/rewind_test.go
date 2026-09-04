package acpagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// capturedRequest is one JSON-RPC request the client put on the wire.
type capturedRequest struct {
	Method string
	Params json.RawMessage
}

// scriptedAgent stands in for grok at the other end of the ACP connection: it
// records every request and answers from a canned table.
//
// Recording the request is the point. The failure Phase 9 guards against is a
// rewind that rolls back the operator's working tree when it was asked only to
// undo a turn — which is not something to verify by observing it happen. The
// assertions below are therefore on the bytes that would have gone out.
type scriptedAgent struct {
	mu       sync.Mutex
	requests []capturedRequest
	// replies maps a method name to the raw JSON result to answer with. A
	// method that is absent is answered -32601, as an older grok would.
	replies map[string]string
}

func (a *scriptedAgent) captured() []capturedRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]capturedRequest(nil), a.requests...)
}

func (a *scriptedAgent) methods() []string {
	out := []string{}
	for _, r := range a.captured() {
		out = append(out, r.Method)
	}
	return out
}

func (a *scriptedAgent) sent(method string) bool {
	for _, m := range a.methods() {
		if m == method {
			return true
		}
	}
	return false
}

// paramsFor returns the decoded params of the first request for method.
func (a *scriptedAgent) paramsFor(t *testing.T, method string) map[string]any {
	t.Helper()
	for _, r := range a.captured() {
		if r.Method != method {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(r.Params, &m); err != nil {
			t.Fatalf("decode params of %s: %v", method, err)
		}
		return m
	}
	t.Fatalf("nothing was sent for %s; the wire carried %v", method, a.methods())
	return nil
}

// newScriptedSession wires a session to a scriptedAgent over a pipe pair.
func newScriptedSession(t *testing.T, replies map[string]string) (*session, *scriptedAgent) {
	t.Helper()

	agent := &scriptedAgent{replies: replies}
	agentReads, clientWrites := io.Pipe()
	clientReads, agentWrites := io.Pipe()

	s := &session{
		localID:  "local-1",
		agentID:  "agent-1",
		log:      slog.New(slog.DiscardHandler),
		events:   make(chan event.Event, 64),
		done:     make(chan struct{}),
		attached: true,
	}
	s.conn = acp.NewClientSideConnection(s, clientWrites, clientReads)

	go func() {
		defer func() { _ = agentWrites.Close() }()
		sc := bufio.NewScanner(agentReads)
		sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal([]byte(line), &msg) != nil || msg.Method == "" {
				continue
			}
			agent.mu.Lock()
			agent.requests = append(agent.requests, capturedRequest{Method: msg.Method, Params: msg.Params})
			result, known := agent.replies[msg.Method]
			agent.mu.Unlock()
			if len(msg.ID) == 0 {
				continue // a notification: nothing to answer
			}
			reply := fmt.Sprintf(
				`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"Method not found"}}`, msg.ID)
			if known {
				reply = fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, msg.ID, result)
			}
			// The transport is newline-delimited, and the reply table above is
			// written across several lines for legibility. Compacting is what
			// keeps one message on one line.
			var framed bytes.Buffer
			if err := json.Compact(&framed, []byte(reply)); err != nil {
				panic(fmt.Sprintf("scripted reply for %s is not JSON: %v", msg.Method, err))
			}
			if _, err := io.WriteString(agentWrites, framed.String()+"\n"); err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		_ = clientWrites.Close()
		_ = agentReads.Close()
		s.markClosedAndKill()
	})
	return s, agent
}

// pointsReply is one rewind point at the given index.
func pointsReply(indices ...int) string {
	parts := make([]string, 0, len(indices))
	for _, i := range indices {
		parts = append(parts, fmt.Sprintf(
			`{"prompt_index":%d,"created_at":"t%d","num_file_snapshots":1,`+
				`"has_file_changes":true,"prompt_preview":"prompt %d"}`, i, i, i))
	}
	return `{"rewind_points":[` + strings.Join(parts, ",") + `]}`
}

// TestUndoLastSendsModeAllAndNeverForces is M1 and M2 together.
//
// Both are asserted on the request body rather than on behaviour, because both
// failures destroy the operator's files: `mode` omitted takes grok's
// backwards-compatibility default, and `force` discards edits made since the
// turn ran. Neither is a thing to reproduce in order to check for it.
func TestUndoLastSendsModeAllAndNeverForces(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": pointsReply(0, 3),
		"_x.ai/rewind/execute": `{"success":true,"target_prompt_index":3,"mode":"all",
			"reverted_files":["a.go","b.go"],"clean_files":[],"conflicts":[],"prompt_text":"add a flag"}`,
	})

	summary, err := s.UndoLast(context.Background())
	if err != nil {
		t.Fatalf("UndoLast: %v", err)
	}

	// Exactly two calls, in order: resolve the target, then act on it.
	if got := agent.methods(); len(got) != 2 ||
		got[0] != "_x.ai/rewind/points" || got[1] != "_x.ai/rewind/execute" {
		t.Fatalf("the wire carried %v, want exactly [_x.ai/rewind/points _x.ai/rewind/execute]", got)
	}

	body := agent.paramsFor(t, "_x.ai/rewind/execute")

	mode, ok := body["mode"]
	if !ok {
		t.Error("the rewind body omits `mode`. grok defaults it to All — the mode that rolls back " +
			"the operator's files — and the field is documented as one clients must set explicitly " +
			"(acp_types.rs RewindRequest.mode)")
	} else if mode != rewindModeAll {
		t.Errorf("mode = %v, want %q", mode, rewindModeAll)
	}

	if force, ok := body["force"]; !ok {
		t.Error("the rewind body omits `force`; it must be present and false")
	} else if force != false {
		t.Errorf("force = %v, want false: forcing discards edits made since that turn", force)
	}

	if idx := body["targetPromptIndex"]; idx != float64(3) {
		t.Errorf("targetPromptIndex = %v, want 3 — the most recent rewind point", idx)
	}
	if body["sessionId"] != "agent-1" {
		t.Errorf("sessionId = %v, want agent-1", body["sessionId"])
	}

	if !strings.Contains(summary, "2 files") {
		t.Errorf("summary = %q, want the number of files it reverted", summary)
	}
	if !strings.Contains(summary, "prompt 3") {
		t.Errorf("summary = %q, want the prompt it undid", summary)
	}
}

// TestUndoLastDecodesSnakeCaseResponses is M3.
//
// grok's rewind types carry no `serde(rename_all)`, so they arrive snake_case —
// while `_x.ai/session/fork`, on the same transport in the same binary, answers
// camelCase. Guessing wrong does not error: every field decodes to its zero
// value and the undo reports that it reverted nothing.
func TestUndoLastDecodesSnakeCaseResponses(t *testing.T) {
	s, _ := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": pointsReply(7),
		"_x.ai/rewind/execute": `{"success":true,"target_prompt_index":7,"mode":"all",
			"reverted_files":["x.go","y.go","z.go"],"clean_files":[],"conflicts":[]}`,
	})

	summary, err := s.UndoLast(context.Background())
	if err != nil {
		t.Fatalf("UndoLast: %v", err)
	}
	if !strings.Contains(summary, "3 files") {
		t.Errorf("summary = %q — reverted_files did not decode. That is what a casing mismatch "+
			"looks like from here: no error, just zeros", summary)
	}
}

// TestUndoLastReportsConflictsAndRevertsNothing is M4.
func TestUndoLastReportsConflictsAndRevertsNothing(t *testing.T) {
	s, _ := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": pointsReply(2),
		"_x.ai/rewind/execute": `{"success":false,"target_prompt_index":2,"mode":"all",
			"reverted_files":[],"clean_files":["ok.go"],
			"conflicts":[{"path":"edited.go","conflict_type":"content_mismatch"}]}`,
	})

	summary, err := s.UndoLast(context.Background())
	if err == nil {
		t.Fatalf("a rewind that reported success=false returned the summary %q", summary)
	}
	for _, want := range []string{"edited.go", "content_mismatch", "nothing was reverted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

// TestUndoLastCarriesAnEngineErrorMessage covers the failure with no conflict
// list, where grok's own text is the only thing worth showing.
func TestUndoLastCarriesAnEngineErrorMessage(t *testing.T) {
	s, _ := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": pointsReply(1),
		"_x.ai/rewind/execute": `{"success":false,"target_prompt_index":1,"mode":"all",
			"reverted_files":[],"clean_files":[],"conflicts":[],"error":"snapshot store is unavailable"}`,
	})

	_, err := s.UndoLast(context.Background())
	if err == nil || !strings.Contains(err.Error(), "snapshot store is unavailable") {
		t.Fatalf("err = %v, want grok's own message", err)
	}
}

// TestUndoLastOnASessionWithNothingToUndo must not send an execute at all: with
// no rewind point there is no target, and a guessed index rewinds to somewhere
// the operator did not ask for.
func TestUndoLastOnASessionWithNothingToUndo(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": `{"rewind_points":[]}`,
	})

	_, err := s.UndoLast(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nothing to undo") {
		t.Fatalf("err = %v, want \"nothing to undo in this session\"", err)
	}
	if agent.sent("_x.ai/rewind/execute") {
		t.Fatal("execute was sent with no rewind point to target")
	}
}

// TestUndoLastOnAGrokWithoutRewind: an older build answers -32601, which must
// read as unsupported rather than as a failed undo — the phone hides the
// control instead of showing an error.
func TestUndoLastOnAGrokWithoutRewind(t *testing.T) {
	s, _ := newScriptedSession(t, map[string]string{})

	_, err := s.UndoLast(context.Background())
	if !errors.Is(err, provider.ErrNotImplemented) {
		t.Fatalf("err = %v, want provider.ErrNotImplemented", err)
	}
}

// TestLastRewindPointTakesTheHighestIndex pins that the array's order is not
// relied on. prompt_index is documented as increasing per session, which is a
// property worth using explicitly rather than assuming the slice is sorted.
func TestLastRewindPointTakesTheHighestIndex(t *testing.T) {
	if _, ok := lastRewindPoint(nil); ok {
		t.Error("an empty list reported a last point")
	}
	got, ok := lastRewindPoint([]rewindPoint{{PromptIndex: 5}, {PromptIndex: 9}, {PromptIndex: 2}})
	if !ok || got.PromptIndex != 9 {
		t.Errorf("got index %d (ok=%v), want 9", got.PromptIndex, ok)
	}
}

// TestGrokImplementsUndoButNotRevert pins the interface choice.
//
// Revert takes a provider-native message id and grok emits none — its rewind is
// indexed by prompt position — so UndoSession is the honest surface. And there
// is no Unrevert anywhere in grok, so `/redo` must stay unavailable rather than
// being wired to something that merely resembles one.
func TestGrokImplementsUndoButNotRevert(t *testing.T) {
	var s any = &session{}
	if _, ok := s.(provider.UndoSession); !ok {
		t.Error("the grok session does not implement provider.UndoSession")
	}
	if _, ok := s.(provider.RevertSession); ok {
		t.Error("the grok session implements RevertSession, but grok has no message ids and no un-rewind")
	}
}

// TestExtensionMethodsDoNotTakeTheUnsafeTransport is M5.
//
// The two transports emit byte-identical JSON-RPC — CallExtension is a name
// check followed by the same SendRequest — so the route cannot be observed on
// the wire. It is observed here by counting calls to rawConnOf, which exists
// only on the unsafe path.
//
// The unsafe path is sound only while `conn` is the first field of
// acp.ClientSideConnection. An upstream field reorder would make it read some
// other pointer, so every method that has a supported API must stay off it.
func TestExtensionMethodsDoNotTakeTheUnsafeTransport(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points":  pointsReply(4),
		"_x.ai/rewind/execute": `{"success":true,"target_prompt_index":4,"reverted_files":[]}`,
		"_x.ai/session/fork":   `{"sessionId":"forked-1"}`,
	})

	var mu sync.Mutex
	var unsafeCalls []string
	current := "unknown"

	restore := rawConnOf
	rawConnOf = func(c *acp.ClientSideConnection) *acp.Connection {
		mu.Lock()
		unsafeCalls = append(unsafeCalls, current)
		mu.Unlock()
		return restore(c)
	}
	t.Cleanup(func() { rawConnOf = restore })

	ctx := context.Background()

	current = "_x.ai/rewind/points+execute"
	if _, err := s.UndoLast(ctx); err != nil {
		t.Fatalf("UndoLast: %v", err)
	}
	current = "_x.ai/session/fork"
	_ = s.rawRequest(ctx, "_x.ai/session/fork", map[string]any{"sessionId": s.agentID}, nil)

	mu.Lock()
	got := append([]string(nil), unsafeCalls...)
	mu.Unlock()
	if len(got) != 0 {
		t.Errorf("extension methods went through the unsafe raw-connection cast: %v. "+
			"They have a public API (ClientSideConnection.CallExtension) and must use it", got)
	}
	if !agent.sent("_x.ai/session/fork") {
		t.Error("_x.ai/session/fork never reached the agent")
	}

	// The other half of the claim: standard methods have no public raw path,
	// so they must still take it. Without this, deleting the unsafe branch
	// outright would pass the assertion above.
	current = "session/set_model"
	_ = s.rawRequest(ctx, "session/set_model", map[string]any{"sessionId": s.agentID}, nil)
	mu.Lock()
	got = append([]string(nil), unsafeCalls...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "session/set_model" {
		t.Errorf("unsafe-path calls = %v, want exactly [session/set_model]", got)
	}
}

// TestCallAgentExtensionPrefixesTheMethod pins the other half of M5: the
// underscore is what makes a method an extension, both for our own routing and
// for the SDK's validation. Callers pass the bare vendor name.
func TestCallAgentExtensionPrefixesTheMethod(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": pointsReply(0),
	})

	var points rewindPointsResponse
	if err := callAgentExtension(context.Background(), s, "x.ai/rewind/points",
		rewindPointsRequest{SessionID: s.agentID}, &points); err != nil {
		t.Fatalf("callAgentExtension: %v", err)
	}
	if !agent.sent("_x.ai/rewind/points") {
		t.Fatalf("the wire carried %v, want the `_`-prefixed extension name", agent.methods())
	}
	if len(points.RewindPoints) != 1 {
		t.Fatalf("decoded %d rewind points, want 1", len(points.RewindPoints))
	}
	if !isExtensionMethod("_x.ai/rewind/points") || isExtensionMethod("session/resume") {
		t.Error("isExtensionMethod does not key on the leading underscore")
	}
}

// TestUndoLastRespectsItsContext: a grok that accepts the request and never
// answers must not pin the caller.
func TestUndoLastRespectsItsContext(t *testing.T) {
	s, _ := newScriptedSession(t, map[string]string{
		"_x.ai/rewind/points": pointsReply(1),
		// execute is deliberately unanswered by the script, so the reply is
		// -32601 and this returns promptly; the point is that it returns.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.UndoLast(ctx) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("UndoLast never returned")
	}
}

// TestUndoLastWithoutAnAgentSession fails before touching the wire: an empty
// session id would ask grok to rewind whatever it decides that names.
func TestUndoLastWithoutAnAgentSession(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{"_x.ai/rewind/points": pointsReply(0)})
	s.agentID = ""

	if _, err := s.UndoLast(context.Background()); err == nil {
		t.Fatal("a session with no agent id was allowed to rewind")
	}
	if len(agent.methods()) != 0 {
		t.Errorf("the wire carried %v, want nothing", agent.methods())
	}
}

// TestNoExtensionMethodIsWrittenWithoutItsUnderscore is acceptance 4 over the
// call sites rather than over one request.
//
// The mistake this catches is not the routing — M5 pins that — but a *new*
// extension call added later and written as `x.ai/…` instead of `_x.ai/…`.
// rawRequest would classify it as a standard method and put it on the unsafe
// cast, which is precisely the path Phase 9 took extension methods off.
func TestNoExtensionMethodIsWrittenWithoutItsUnderscore(t *testing.T) {
	// The standard methods acp-go-sdk@v0.13.5 does not model. These have no
	// public raw path, so the unsafe cast is the only way to reach them, and
	// this set is the complete list of what may use it.
	standard := map[string]bool{
		"initialize":        true,
		"session/set_model": true,
		"session/resume":    true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	// literalArg returns the string literal at index i, or "" if it is not one.
	literalArg := func(call *ast.CallExpr, i int) string {
		if len(call.Args) <= i {
			return ""
		}
		lit, ok := call.Args[i].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return ""
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return ""
		}
		return v
	}

	sites := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				where := fset.Position(call.Pos())
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					if fn.Sel.Name != "rawRequest" {
						return true
					}
					// rawRequest(ctx, method, params, out)
					// The one legitimately computed name is callAgentExtension's
					// own `"_"+method`, which is prefixed by construction.
					if bin, ok := call.Args[1].(*ast.BinaryExpr); ok {
						if lit, ok := bin.X.(*ast.BasicLit); ok && bin.Op == token.ADD &&
							lit.Kind == token.STRING && lit.Value == `"_"` {
							sites++
							return true
						}
					}
					method := literalArg(call, 1)
					if method == "" {
						t.Errorf("%s: rawRequest is called with a computed method name that is not "+
							"provably `\"_\"+…`; this check can only reason about literals", where)
						return true
					}
					sites++
					if !isExtensionMethod(method) && !standard[method] {
						t.Errorf("%s: rawRequest(%q) takes the unsafe raw-connection cast, but %q is "+
							"not one of the three standard methods that need it. If it is a vendor "+
							"extension it must be written with its leading underscore", where, method, method)
					}
				case *ast.Ident:
					if fn.Name != "callAgentExtension" {
						return true
					}
					// callAgentExtension(ctx, s, method, params, out)
					method := literalArg(call, 2)
					if method == "" {
						return true
					}
					sites++
					if isExtensionMethod(method) {
						t.Errorf("%s: callAgentExtension(%q) — the helper adds the underscore itself, "+
							"so this would send a double-prefixed method", where, method)
					}
				}
				return true
			})
		}
	}
	if sites < 6 {
		t.Fatalf("found only %d call sites; the scan is not seeing the package", sites)
	}
}
