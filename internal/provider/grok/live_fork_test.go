//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// T-F1: walk pinned _x.ai/session/fork shapes (MADR 0092 Phase A).
//
// 0081's four shapes fail with missing field `newCwd`. 1.0.5
// (5115b46bc909) accepts {sourceSessionId, sourceCwd, newCwd} and
// returns newSessionId. The fifth shape is required; the four losers
// stay in front so a schema revert still logs the old error.
func TestLiveGrokSessionForkShapes(t *testing.T) {
	p := startACP(t, nil)
	sid := p.sessionID()
	if sid == "" {
		t.Fatal("no sessionId")
	}
	cwd := acpSessionCWD(t, p)

	const winning = "source+sourceCwd+newCwd"
	shapes := []struct {
		name   string
		params map[string]any
	}{
		{"sourceSessionId+sourceCwd", map[string]any{"sourceSessionId": sid, "sourceCwd": cwd}},
		{"source+cwd", map[string]any{"sourceSessionId": sid, "sourceCwd": cwd, "cwd": cwd}},
		{"both ids + sourceCwd", map[string]any{"sessionId": sid, "sourceSessionId": sid, "sourceCwd": cwd}},
		{"source+cwd+mcp", map[string]any{"sourceSessionId": sid, "sourceCwd": cwd, "mcpServers": []any{}}},
		{winning, map[string]any{"sourceSessionId": sid, "sourceCwd": cwd, "newCwd": cwd}},
	}

	var winner string
	var winningID string
	for i, sh := range shapes {
		id := 20 + i
		p.send(t, id, "_x.ai/session/fork", sh.params)
		msg, err := p.waitRaw(t, id, 8*time.Second)
		if err != nil {
			if sh.name == winning {
				t.Fatalf("%s: %v", sh.name, err)
			}
			t.Logf("%s: %v", sh.name, err)
			continue
		}
		if errObj, has := msg["error"]; has {
			if sh.name == winning {
				t.Fatalf("%s: error %v", sh.name, errObj)
			}
			t.Logf("%s: error %v", sh.name, errObj)
			continue
		}
		newID := forkSessionID(msg)
		t.Logf("%s: result session=%q", sh.name, newID)
		if newID != "" && newID != sid {
			if winner == "" {
				winner = sh.name
			}
			if sh.name == winning {
				winningID = newID
			}
		}
	}
	if winningID == "" || winningID == sid {
		t.Fatal("winning shape source+sourceCwd+newCwd did not return a newSessionId")
	}

	tbl := grok.New(grok.Config{}).CommandTable()
	if tbl["fork"].Kind == command.KindOp && winner == "" {
		t.Fatal("fork is KindOp but no live shape returned a new session id")
	}
}

// T-F5: a second grok process must session/load the id _x.ai/session/fork
// just created. That is Manager.Fork → Create. Must pass before KindOp
// is remapped (MADR 0092 Phase B).
func TestLiveGrokForkLoadOnNewProcess(t *testing.T) {
	ap := startACP(t, nil)
	sid := ap.sessionID()
	if sid == "" {
		t.Fatal("no sessionId")
	}
	cwd := acpSessionCWD(t, ap)

	ap.send(t, 30, "_x.ai/session/fork", map[string]any{
		"sourceSessionId": sid,
		"sourceCwd":       cwd,
		"newCwd":          cwd,
	})
	msg, err := ap.waitRaw(t, 30, 8*time.Second)
	if err != nil {
		t.Fatalf("session/fork: %v", err)
	}
	if errObj, has := msg["error"]; has {
		t.Fatalf("session/fork: %v", errObj)
	}
	newID := forkSessionID(msg)
	if newID == "" || newID == sid {
		t.Fatalf("fork returned id %q, want a newSessionId", newID)
	}

	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	child, err := p.Start(ctx, provider.StartOptions{
		Name:           "fork-load-dst",
		CWD:            cwd,
		AgentSessionID: newID,
	})
	if err != nil {
		t.Fatalf("session/load of forked id %s: %v", newID, err)
	}
	defer child.Close(context.Background())
	if got := child.AgentSessionID(); got != newID {
		t.Fatalf("loaded AgentSessionID=%q, want %q", got, newID)
	}
}

// T-F3: production ForkSession + session/load (MADR 0092 Phase D).
func TestLiveGrokForkSession(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cwd := t.TempDir()
	parent, err := p.Start(ctx, provider.StartOptions{Name: "fork-live", CWD: cwd})
	if err != nil {
		t.Fatalf("start parent: %v", err)
	}
	defer parent.Close(context.Background())

	fs, ok := parent.(provider.ForkSession)
	if !ok {
		t.Fatal("grok session must implement provider.ForkSession")
	}
	res, err := fs.Fork(ctx, provider.ForkOptions{})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if res.AgentSessionID == "" || res.AgentSessionID == parent.AgentSessionID() {
		t.Fatalf("Fork result %+v, want new AgentSessionID", res)
	}
	if res.ForkedFromID != parent.AgentSessionID() {
		t.Fatalf("ForkedFromID=%q, want %q", res.ForkedFromID, parent.AgentSessionID())
	}

	ignored, err := fs.Fork(ctx, provider.ForkOptions{LastTurnID: "not-a-turn"})
	if err != nil {
		t.Fatalf("Fork with LastTurnID must be ignored, not fail: %v", err)
	}
	if ignored.AgentSessionID == "" || ignored.AgentSessionID == parent.AgentSessionID() {
		t.Fatalf("LastTurnID Fork result %+v", ignored)
	}

	child, err := p.Start(ctx, provider.StartOptions{
		Name:           "fork-live-child",
		CWD:            cwd,
		AgentSessionID: res.AgentSessionID,
	})
	if err != nil {
		t.Fatalf("session/load of forked id %s: %v", res.AgentSessionID, err)
	}
	defer child.Close(context.Background())
	if got := child.AgentSessionID(); got != res.AgentSessionID {
		t.Fatalf("loaded AgentSessionID=%q, want %q", got, res.AgentSessionID)
	}
}

func acpSessionCWD(t *testing.T, p *acpProc) string {
	t.Helper()
	cwd := t.TempDir()
	if res, _ := p.result(2)["result"].(map[string]any); res != nil {
		if meta, _ := res["_meta"].(map[string]any); meta != nil {
			if c, _ := meta["currentWorkingDirectory"].(string); c != "" {
				return c
			}
		}
	}
	return cwd
}

func forkSessionID(msg map[string]any) string {
	res, _ := msg["result"].(map[string]any)
	if res == nil {
		return ""
	}
	if id, _ := res["newSessionId"].(string); id != "" {
		return id
	}
	if id, _ := res["sessionId"].(string); id != "" {
		return id
	}
	if inner, _ := res["result"].(map[string]any); inner != nil {
		if id, _ := inner["newSessionId"].(string); id != "" {
			return id
		}
		id, _ := inner["sessionId"].(string)
		return id
	}
	return ""
}
