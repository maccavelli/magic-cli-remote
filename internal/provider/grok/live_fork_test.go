//go:build live_grok

package grok_test

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// T-F1: walk pinned _x.ai/session/fork shapes (MADR 0092 Phase A).
//
// 0081's four shapes fail with missing field `newCwd`. 1.0.4
// (d846eb93d94d) accepts {sourceSessionId, sourceCwd, newCwd} and
// returns newSessionId. The fifth shape is required; the four losers
// stay in front so a schema revert still logs the old error.
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
