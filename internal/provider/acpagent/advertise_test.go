package acpagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// initializeFrame is the initialize request Magic sends, as JSON. Pinned byte
// for byte, so any change to what Magic advertises is a reviewed change to
// this test (PLAN 0184 A2 step 2).
const initializeFrame = `{"clientCapabilities":{"auth":{},"elicitation":{"form":{}},"fs":{"readTextFile":true,"writeTextFile":true},` +
	`"plan":{},"session":{"compaction":{},"configOptions":{"boolean":{}},"notices":{}},"terminal":true},` +
	`"clientInfo":{"name":"mcremote","version":"v0.21.0"},"protocolVersion":1}`

// TestInitializeFrameIsPinned pins the serialized initialize request.
func TestInitializeFrameIsPinned(t *testing.T) {
	p := &Provider{cfg: Config{ClientVersion: "v0.21.0"}}
	b, err := json.Marshal(p.initializeRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != initializeFrame {
		t.Fatalf("initialize frame changed:\n got: %s\nwant: %s", b, initializeFrame)
	}
}

// TestInitializeSendsClientInfo: an empty version still sends one, since the
// schema requires clientInfo.version.
func TestInitializeSendsClientInfo(t *testing.T) {
	for _, tc := range []struct{ cfg, want string }{{"v0.21.0", "v0.21.0"}, {"", "dev"}} {
		req := (&Provider{cfg: Config{ClientVersion: tc.cfg}}).initializeRequest()
		if req.ClientInfo == nil || req.ClientInfo.Name != "mcremote" || req.ClientInfo.Version != tc.want {
			t.Errorf("ClientVersion %q: clientInfo = %+v, want mcremote %s", tc.cfg, req.ClientInfo, tc.want)
		}
	}
}

// TestCapabilityHonesty is MADR 0184 D4's invariant: initialize advertises a
// capability exactly when this package has its behavior. It covers the default
// set every Spec without its own uses (grok's package checks grok's Spec), and
// shows the check can fail.
func TestCapabilityHonesty(t *testing.T) {
	if problems := CheckClientCapabilities(defaultClientCapabilities()); len(problems) != 0 {
		t.Fatalf("the default advertisement is dishonest: %v", problems)
	}
	if problems := CheckClientCapabilities((&Provider{}).ClientCapabilities()); len(problems) != 0 {
		t.Fatalf("a Spec without its own set advertises dishonestly: %v", problems)
	}
	less := acp.ClientCapabilities{}
	if problems := CheckClientCapabilities((&Provider{spec: Spec{ClientCapabilities: &less}}).ClientCapabilities()); len(problems) != 0 {
		t.Fatalf("advertising nothing is honest, got %v", problems)
	}

	// Each of these advertises something with no behavior here; each must be
	// caught.
	dishonest := map[string]acp.ClientCapabilities{
		// session.notices and session.compaction were examples here until
		// MADR 0188 and MADR 0190 implemented them.
		// elicitation.form is implemented (MADR 0191); URL mode is not.
		// plan was an example until MADR 0192 implemented it.
		"elicitation.url": {Elicitation: &acp.ElicitationCapabilities{Url: &acp.ElicitationUrlCapabilities{}}},
		"auth.terminal":   {Auth: acp.AuthCapabilities{Terminal: true}},
	}
	for name, c := range dishonest {
		if problems := CheckClientCapabilities(c); len(problems) == 0 {
			t.Errorf("%s: advertised without behavior, and the check accepted it", name)
		}
	}
}

// TestSessionNewAcceptsABooleanConfigOption is PLAN 0184 A2 step 1. Magic maps
// boolean config options end to end, and acp-go-sdk2 refuses a session/new
// answer carrying one unless the client advertised session.configOptions.boolean
// at initialize. Without the advertisement, any agent offering a boolean option
// fails session/new (MADR 0184 D4).
func TestSessionNewAcceptsABooleanConfigOption(t *testing.T) {
	s, agent := newWireSession(t, `{}`)

	type result struct {
		resp acp.NewSessionResponse
		err  error
	}
	got := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := s.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		got <- result{resp, err}
	}()

	req := agent.request("session/new", 5*time.Second)
	agent.respond(req, acp.NewSessionResponse{
		SessionId: "agent-1",
		ConfigOptions: []acp.SessionConfigOption{
			{Boolean: &acp.SessionConfigOptionBoolean{Id: "web", Name: "Web search", CurrentValue: true}},
		},
	})

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("session/new with a boolean config option failed: %v", r.err)
		}
		if len(r.resp.ConfigOptions) != 1 || r.resp.ConfigOptions[0].Boolean == nil {
			t.Fatalf("config options = %+v, want the boolean option", r.resp.ConfigOptions)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session/new did not return")
	}
}
