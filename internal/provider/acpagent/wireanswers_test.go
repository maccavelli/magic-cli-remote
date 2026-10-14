package acpagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// The SDK, built with production's options, validates what an agent sends and
// answers a bad message itself (MADR 0184 D5). These pin its answers at Magic's
// boundary, so a bump that changes one fails here, and so Magic's own copies of
// those checks can go (PLAN 0184 A2 step 6).

const wireBudget = 5 * time.Second

// errorCode is the code of the client's error answer to id, or 0 when the
// answer is a result.
func errorCode(t *testing.T, a *wireAgent, id string) int {
	t.Helper()
	m := a.answerTo(id, wireBudget)
	if m.Error == nil {
		return 0
	}
	t.Logf("answer to %s: %d %s %s", id, m.Error.Code, m.Error.Message, m.Error.Data)
	return m.Error.Code
}

func TestWireMalformedJSONIsAParseError(t *testing.T) {
	_, a := newWireSession(t, `{}`)
	a.sendRaw(`{"jsonrpc":"2.0","id":1,"method":`)
	if got := errorCode(t, a, "null"); got != -32700 {
		t.Fatalf("a malformed line was answered %d, want -32700", got)
	}
}

func TestWireInvalidRequestIsRefused(t *testing.T) {
	_, a := newWireSession(t, `{}`)
	a.sendRaw(`{"jsonrpc":"2.0","id":2,"method":7}`)
	if got := errorCode(t, a, "2"); got != -32600 {
		t.Fatalf("a request whose method is not a string was answered %d, want -32600", got)
	}
}

func TestWireUnknownMethodIsMethodNotFound(t *testing.T) {
	_, a := newWireSession(t, `{}`)
	a.sendRaw(`{"jsonrpc":"2.0","id":3,"method":"fs/delete_everything","params":{}}`)
	if got := errorCode(t, a, "3"); got != -32601 {
		t.Fatalf("an unknown method was answered %d, want -32601", got)
	}
}

// TestWireUnadvertisedElicitationIsRefused: Magic advertises elicitation
// form mode only (MADR 0191 D1, D6; until then, none, MADR 0184 D9). The SDK
// gates elicitation by its mode variant, not by method, so an unadvertised URL
// elicitation is invalid params, -32602, naming the capability. The params are
// the SDK's own valid URL request, so the refusal is the gate's and not a
// decode failure's.
func TestWireUnadvertisedElicitationIsRefused(t *testing.T) {
	_, a := newWireSession(t, `{}`)
	a.send(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "elicitation/create",
		"params": acp.CreateElicitationRequest{Url: &acp.CreateElicitationUrl{Message: "sign in", Mode: "url",
			ElicitationId: "e1", Url: "https://example.com/login",
			Session: &acp.ElicitationSessionScope{SessionId: "agent-1"}}}})
	m := a.answerTo("4", wireBudget)
	if m.Error == nil || m.Error.Code != -32602 || !strings.Contains(string(m.Error.Data), "elicitation.url not advertised") {
		t.Fatalf("an unadvertised elicitation was answered %+v; want -32602 naming elicitation.url", m.Error)
	}
}

// TestWireUnadvertisedMethodIsMethodNotFound: a Spec that advertises no
// terminal, as gobble's will (MADR 0184, amendment, owner answer Q3), gets
// terminal/create refused by the SDK with -32601 before Magic's terminal host
// runs.
func TestWireUnadvertisedMethodIsMethodNotFound(t *testing.T) {
	noTerminal := acp.ClientCapabilities{Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}}
	_, a := newWireSessionFor(t, `{}`, &Provider{spec: Spec{ClientCapabilities: &noTerminal}})
	a.send(map[string]any{"jsonrpc": "2.0", "id": 5, "method": "terminal/create",
		"params": map[string]any{"sessionId": "agent-1", "command": "true"}})
	if got := errorCode(t, a, "5"); got != -32601 {
		t.Fatalf("terminal/create to a client that did not advertise terminal was answered %d, want -32601", got)
	}
}

// TestWireFSPathsAreValidatedByTheSDK is why ReadTextFile and WriteTextFile no
// longer check the path themselves: the SDK answers -32602 before either
// handler runs. A refusal from the handler would be -32603.
func TestWireFSPathsAreValidatedByTheSDK(t *testing.T) {
	_, a := newWireSession(t, `{}`)
	cases := []struct{ id, line string }{
		{"10", `{"jsonrpc":"2.0","id":10,"method":"fs/read_text_file","params":{"sessionId":"agent-1"}}`},
		{"11", `{"jsonrpc":"2.0","id":11,"method":"fs/read_text_file","params":{"sessionId":"agent-1","path":""}}`},
		{"12", `{"jsonrpc":"2.0","id":12,"method":"fs/read_text_file","params":{"sessionId":"agent-1","path":"relative/file.txt"}}`},
		{"13", `{"jsonrpc":"2.0","id":13,"method":"fs/write_text_file","params":{"sessionId":"agent-1","path":"relative/out.txt","content":"x"}}`},
	}
	for _, tc := range cases {
		a.sendRaw(tc.line)
		if got := errorCode(t, a, tc.id); got != -32602 {
			t.Errorf("request %s was answered %d, want -32602 from the SDK: %s", tc.id, got, tc.line)
		}
	}
	if _, err := os.Stat("relative/out.txt"); err == nil {
		t.Error("a write to a relative path reached the file system")
	}
}

// TestWireOverlongLineIsSkippedNotFatal: a line past the bound is answered
// -32600 and skipped, and the connection keeps serving.
func TestWireOverlongLineIsSkippedNotFatal(t *testing.T) {
	_, a := newWireSession(t, `{}`)
	a.sendRaw(`{"jsonrpc":"2.0","id":20,"method":"fs/read_text_file","params":{"sessionId":"agent-1","path":"/` +
		strings.Repeat("x", maxACPMessageSize) + `"}}`)
	if got := errorCode(t, a, "null"); got != -32600 {
		t.Fatalf("an over-long line was answered %d, want -32600", got)
	}

	file := filepath.Join(t.TempDir(), "after.txt")
	if err := os.WriteFile(file, []byte("still serving"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.send(map[string]any{"jsonrpc": "2.0", "id": 21, "method": "fs/read_text_file",
		"params": map[string]any{"sessionId": "agent-1", "path": file}})
	m := a.answerTo("21", wireBudget)
	if m.Error != nil || !strings.Contains(string(m.Result), "still serving") {
		t.Fatalf("after an over-long line the connection answered %+v, %s; want the file's content", m.Error, m.Result)
	}
}
