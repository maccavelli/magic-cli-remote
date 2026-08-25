package ws_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

func TestProxyBoundaryRejectsRawCodexJSONRPCInjection(t *testing.T) {
	ws := setupWSSession(t, "proxy-boundary")
	defer ws.close(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := protocol.Envelope{
		V: protocol.V1, Type: "codex.jsonrpc", ID: "inject",
		Payload: []byte(`{"method":"thread/start","id":1,"params":{}}`),
	}
	writeEnv(ctx, t, ws.conn, env)
	var response protocol.Envelope
	for response.Type != protocol.TypeError {
		response = readEnv(ctx, t, ws.conn)
	}
	if response.Type != protocol.TypeError || !strings.Contains(string(response.Payload), "unknown_type") {
		t.Fatalf("raw Codex injection response = %s %s", response.Type, response.Payload)
	}
}
