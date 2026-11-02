package acpagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
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
	return slices.Contains(a.methods(), method)
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

// TestExtensionMethodsDoNotTakeTheStandardTransport is M5.
//
// The two transports emit byte-identical JSON-RPC — CallExtension is a name
// check followed by the same SendRequest — so the route cannot be observed on
// the wire. It is observed here by counting calls to standardConnOf, which
// exists only on the standard-method path.
//
// That path sends untyped standard methods, which the SDK does not check
// against what initialize agreed (MADR 0184 D6), so every method that has an
// extension API must stay off it. Under the Coder fork it was also an
// unsafe.Pointer cast; acp-go-sdk2's Connection() replaced that (0184 D2).
func TestExtensionMethodsDoNotTakeTheStandardTransport(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_vendor/points": `{"points":[4]}`,
		"_vendor/fork":   `{"sessionId":"forked-1"}`,
	})

	var mu sync.Mutex
	var standardCalls []string
	current := "unknown"

	restore := standardConnOf
	standardConnOf = func(c *acp.ClientSideConnection) *acp.Connection {
		mu.Lock()
		standardCalls = append(standardCalls, current)
		mu.Unlock()
		return restore(c)
	}
	t.Cleanup(func() { standardConnOf = restore })

	ctx := context.Background()

	current = "_vendor/points"
	if err := s.CallExtension(ctx, "_vendor/points", map[string]any{"sessionId": s.agentID}, nil); err != nil {
		t.Fatalf("CallExtension: %v", err)
	}
	current = "_vendor/fork"
	_ = s.rawRequest(ctx, "_vendor/fork", map[string]any{"sessionId": s.agentID}, nil)

	mu.Lock()
	got := append([]string(nil), standardCalls...)
	mu.Unlock()
	if len(got) != 0 {
		t.Errorf("extension methods went through the standard-method raw path: %v. "+
			"They have a public API (ClientSideConnection.CallExtension) and must use it", got)
	}
	if !agent.sent("_vendor/fork") {
		t.Error("_vendor/fork never reached the agent")
	}

	// The other half of the claim: standard methods cannot go through
	// CallExtension, so they must still take this path. Without this, deleting
	// the standard branch outright would pass the assertion above.
	current = "session/set_model"
	_ = s.rawRequest(ctx, "session/set_model", map[string]any{"sessionId": s.agentID}, nil)
	mu.Lock()
	got = append([]string(nil), standardCalls...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "session/set_model" {
		t.Errorf("standard-path calls = %v, want exactly [session/set_model]", got)
	}
}

// TestCallExtensionSendsTheNameAsGiven pins the other half of M5: the
// underscore is what makes a method an extension, both for our own routing and
// for the SDK's validation. Host.CallExtension takes the name in full, as the
// wire carries it (MADR 0208, amendment of 2026-11-01).
func TestCallExtensionSendsTheNameAsGiven(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_vendor/points": `{"points":[0]}`,
	})

	var out struct{ Points []int }
	if err := s.CallExtension(context.Background(), "_vendor/points",
		map[string]any{"sessionId": s.agentID}, &out); err != nil {
		t.Fatalf("CallExtension: %v", err)
	}
	if !agent.sent("_vendor/points") {
		t.Fatalf("the wire carried %v, want the `_`-prefixed extension name", agent.methods())
	}
	if len(out.Points) != 1 {
		t.Fatalf("decoded %d points, want 1", len(out.Points))
	}
	if !isExtensionMethod("_vendor/points") || isExtensionMethod("session/resume") {
		t.Error("isExtensionMethod does not key on the leading underscore")
	}
}
