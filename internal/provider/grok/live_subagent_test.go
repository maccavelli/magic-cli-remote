//go:build live_grok

package grok_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// TestLiveGrokSubagentSuppressedAndPromoted is the live half of MADR 0051
// Part II for grok, and the regression guard for the leak it fixed.
//
// Before this, acpagent never compared a notification's session id against the
// live one, so a sub-agent's frames — which grok sends down the same
// connection with the child's id — were emitted straight into the parent's
// transcript. Measured on grok 0.2.114 against a two-file read: 63 child
// assistant chunks vs the parent's 56, 46 child thought chunks, plus the
// sub-agent's own prompt echoed back as a user message.
//
// Two things are asserted here that the unit tests cannot: that grok really
// does emit `_x.ai/session_notification` sub-agent frames on a live run, and
// that the assistant text reaching the transcript is the parent's alone.
//
// Run with:
//
//	go test -tags live_grok ./internal/provider/grok/ -run Subagent -count=1 -timeout 600s
func TestLiveGrokSubagentSuppressedAndPromoted(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 540*time.Second)
	defer cancel()

	cwd := t.TempDir()
	// Two files whose contents are distinctive enough that finding them in the
	// transcript proves the parent relayed the conclusion itself.
	if err := os.WriteFile(filepath.Join(cwd, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "b.txt"), []byte("world\n"), 0o600); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	s, err := p.Start(ctx, provider.StartOptions{Name: "live-subagent", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Use your subagent/task tool to launch a background agent " +
		"that reads a.txt and b.txt in the current directory and reports the two " +
		"words. Then tell me the two words."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var (
		reply       strings.Builder
		sets        [][]event.SubagentInfo
		sawRunning  bool
		sawTerminal bool
		cleared     bool
	)
	deadline := time.After(480 * time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			switch ev.Type {
			case event.TypeAssistantChunk, event.TypeThoughtChunk:
				reply.WriteString(ev.Text)
			case event.TypeSubagents:
				sets = append(sets, ev.Subagents)
				if len(ev.Subagents) == 0 {
					if len(sets) > 1 {
						cleared = true
					}
					break
				}
				for _, sa := range ev.Subagents {
					t.Logf("subagent id=%s name=%s status=%s task=%q",
						sa.ID, sa.Name, sa.Status, sa.Task)
					switch sa.Status {
					case event.SubagentStatusRunning:
						sawRunning = true
					case event.SubagentStatusCompleted, event.SubagentStatusFailed:
						sawTerminal = true
					}
					if sa.ID == "" {
						t.Error("sub-agent entry with no id")
					}
				}
			case event.TypeTurnComplete:
				done = true
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn_complete")
		}
	}

	if len(sets) == 0 {
		t.Skip("model did not spawn a sub-agent; the unit fixtures cover the set")
	}
	if !sawRunning {
		t.Error("no running sub-agent was ever reported")
	}
	if !sawTerminal {
		t.Error("the sub-agent never reached a terminal status; the panel would spin forever")
	}
	if !cleared {
		t.Error("the set was never cleared; the panel would stay up after the turn")
	}

	// The parent still relays the conclusion — suppression removes the
	// sub-agent's stream, not the answer.
	text := strings.ToLower(reply.String())
	if !strings.Contains(text, "hello") || !strings.Contains(text, "world") {
		t.Errorf("parent reply lost the sub-agent's conclusion: %q", reply.String())
	}
	// The sub-agent's own report is verbose and markdown-tabled; grok's
	// finished frame carries it as `output`, which must never be forwarded.
	if strings.Contains(text, "<subagent_meta>") {
		t.Error("sub-agent metadata leaked into the transcript")
	}
}
