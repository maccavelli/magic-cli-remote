package acpagent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// testInitializeResult is a minimal valid initialize result: the protocol
// version and an agent that advertises nothing optional.
const testInitializeResult = `{"protocolVersion":1,"agentCapabilities":{}}`

// initializeForTest completes initialize on conn, as spawnAgent does before
// anything else. acp-go-sdk2 refuses every typed request, and drops every typed
// notification, until initialize has completed on the connection. A fixture
// that skips it tests a connection production never has, and a test of
// inbound notifications passes without a single one reaching the handler
// (PLAN 0184 A1, execution record). The fixture's agent must answer it.
func initializeForTest(t *testing.T, conn *acp.ClientSideConnection) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := conn.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, ok := conn.Negotiated(); !ok {
		t.Fatal("initialize returned, but the connection has no negotiated record")
	}
}

// answerInitialize plays the agent for a fixture that has no agent of its own:
// it reads what the client writes from r, answers initialize on w, and discards
// everything else, until r ends.
func answerInitialize(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.Method != "initialize" {
			continue
		}
		if _, err := io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(msg.ID)+`,"result":`+testInitializeResult+"}\n"); err != nil {
			return
		}
	}
}
