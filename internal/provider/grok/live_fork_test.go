//go:build live_grok

package grok_test

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// T-I1: walk pinned _x.ai/session/fork shapes. Implement ForkSession only
// when a shape returns a new session id (MADR 0081 Phase I).
//
// Measured 2026-08-12 grok 1.0.3: all four listed shapes fail with
// missing field `newCwd`. Plan forbids guessing a fifth field; /fork
// stays KindNone.
func TestLiveGrokSessionForkShapes(t *testing.T) {
	p := startACP(t, nil)
	sid := p.sessionID()
	if sid == "" {
		t.Fatal("no sessionId")
	}
	cwd := t.TempDir()
	// startACP already used a temp cwd; fork sourceCwd must be the session cwd.
	// Recover cwd from the session/new we sent — the helper's TempDir is gone.
	// Use a fresh absolute path grok already accepted: re-read from result _meta.
	if res, _ := p.result(2)["result"].(map[string]any); res != nil {
		if meta, _ := res["_meta"].(map[string]any); meta != nil {
			if c, _ := meta["currentWorkingDirectory"].(string); c != "" {
				cwd = c
			}
		}
	}

	shapes := []struct {
		name   string
		params map[string]any
	}{
		{"sourceSessionId+sourceCwd", map[string]any{"sourceSessionId": sid, "sourceCwd": cwd}},
		{"source+cwd", map[string]any{"sourceSessionId": sid, "sourceCwd": cwd, "cwd": cwd}},
		{"both ids + sourceCwd", map[string]any{"sessionId": sid, "sourceSessionId": sid, "sourceCwd": cwd}},
		{"source+cwd+mcp", map[string]any{"sourceSessionId": sid, "sourceCwd": cwd, "mcpServers": []any{}}},
	}

	var winner string
	for i, sh := range shapes {
		id := 20 + i
		p.send(t, id, "_x.ai/session/fork", sh.params)
		msg, err := p.waitRaw(t, id, 8*time.Second)
		if err != nil {
			t.Logf("%s: %v", sh.name, err)
			continue
		}
		if errObj, has := msg["error"]; has {
			t.Logf("%s: error %v", sh.name, errObj)
			continue
		}
		newID := forkSessionID(msg)
		t.Logf("%s: result session=%q", sh.name, newID)
		if newID != "" && newID != sid {
			winner = sh.name
			break
		}
	}

	tbl := grok.New(grok.Config{}).CommandTable()
	if tbl["fork"].Kind == command.KindOp && winner == "" {
		t.Fatal("fork is KindOp but no live shape returned a new session id")
	}
	if winner == "" {
		t.Log("no winning fork shape; leave /fork KindNone")
	}
}

func forkSessionID(msg map[string]any) string {
	res, _ := msg["result"].(map[string]any)
	if res == nil {
		return ""
	}
	if id, _ := res["sessionId"].(string); id != "" {
		return id
	}
	if inner, _ := res["result"].(map[string]any); inner != nil {
		id, _ := inner["sessionId"].(string)
		return id
	}
	return ""
}
