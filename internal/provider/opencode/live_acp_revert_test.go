//go:build live_opencode

package opencode_test

// 0214 PLAN P6e: undo, redo and the shell end to end, through Magic's own
// provider (acpagent with the opencode Spec and its control plane) on an
// isolated home. The undo test runs three turns on the free model, on the
// owner's word (P6e Deviation 1, "Yes, free model"); the shell test sends no
// prompt.
//
// Run with: go test -tags live_opencode ./internal/provider/opencode/ -run 'LiveV2(UndoAndRedo|Shell)' -count=1 -v

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// liveEvents is what a session emitted.
type liveEvents struct {
	mu  sync.Mutex
	all []event.Event
}

// since returns what was emitted from index from on.
func (l *liveEvents) since(from int) []event.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]event.Event(nil), l.all[from:]...)
}

func (l *liveEvents) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.all)
}

// startLiveV2Session starts a session of Magic's own v2 provider in an
// isolated git project. The provider's engine and control plane inherit this
// process's environment, so the isolated home is made this process's own,
// with credential-shaped variables and BASH_ENV blanked, as newV2Home leaves
// them out.
func startLiveV2Session(t *testing.T) (*v2Home, provider.Session, *liveEvents) {
	t.Helper()
	h := newV2Home(t)
	for _, kv := range h.env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if v2Credential.MatchString(k) || strings.EqualFold(k, "BASH_ENV") {
			t.Setenv(k, "")
		}
	}
	// OpenCode records a turn's files only in a git repository
	// (packages/core/src/snapshot.ts:114), as G5's project was.
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = h.project
		cmd.Env = append(append([]string(nil), h.env...), "GIT_AUTHOR_NAME=pin", "GIT_AUTHOR_EMAIL=pin@example.invalid",
			"GIT_COMMITTER_NAME=pin", "GIT_COMMITTER_EMAIL=pin@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	// h.project is a temporary directory, which on macOS is reached through
	// the /var symlink: the Spec's RealCWD is what makes undo work there.
	p := opencode.NewACP(acpagent.Config{Bin: "opencode", DefaultCWD: h.project, AlwaysApprove: true, AllowRemoteShell: true})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{CWD: h.project, Model: v2FreeModel})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	evs := &liveEvents{}
	go func() {
		for ev := range s.Events() {
			evs.mu.Lock()
			evs.all = append(evs.all, ev)
			evs.mu.Unlock()
		}
	}()
	return h, s, evs
}

// TestLiveV2UndoAndRedo: two turns write note.txt; /undo puts back the first
// turn's file, /redo the second's, /undo the first's again; the third prompt
// makes the undo permanent: the engine keeps two prompts, and Magic removes
// one turn.
func TestLiveV2UndoAndRedo(t *testing.T) {
	h, s, evs := startLiveV2Session(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	busy := s.(interface{ Busy() bool })
	turn := func(text string) {
		t.Helper()
		if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: text}}); err != nil {
			t.Fatalf("prompt %q: %v", text, err)
		}
		for deadline := time.Now().Add(5 * time.Minute); busy.Busy(); time.Sleep(200 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("turn %q did not end", text)
			}
		}
	}
	note := func() string {
		b, _ := os.ReadFile(filepath.Join(h.project, "note.txt"))
		return strings.TrimSpace(string(b))
	}

	turn("Use your write tool to create the file note.txt containing exactly the word apple, then reply done.")
	if note() != "apple" {
		t.Fatalf("after the first turn note.txt = %q; the model did not write it, so nothing below can be checked", note())
	}
	turn("Use your write tool to replace the content of note.txt with exactly the word banana, then reply done.")
	if note() != "banana" {
		t.Fatalf("after the second turn note.txt = %q; the model did not write it", note())
	}

	undo := s.(provider.UndoSession)
	redo := s.(provider.RevertSession)
	if text, err := undo.UndoLast(ctx); err != nil || note() != "apple" || !strings.Contains(text, "restored 1 file") {
		t.Fatalf("/undo: %q, %v; note.txt = %q, want apple, and 1 file restored", text, err, note())
	}
	if err := redo.Unrevert(ctx); err != nil || note() != "banana" {
		t.Fatalf("/redo: %v; note.txt = %q, want banana", err, note())
	}
	if _, err := undo.UndoLast(ctx); err != nil || note() != "apple" {
		t.Fatalf("/undo again: %v; note.txt = %q, want apple", err, note())
	}

	mark := evs.len()
	turn("Reply with exactly one word: cherry")
	var removed []event.Event
	for _, ev := range evs.since(mark) {
		if ev.Type == event.TypeTranscriptRemove {
			removed = append(removed, ev)
		}
	}
	if len(removed) != 1 || removed[0].RemoveTurns != 1 {
		t.Errorf("the third prompt's removals %+v, want one of 1 turn", removed)
	}
	if note() != "apple" {
		t.Errorf("after the third turn note.txt = %q, want apple: the undo is permanent", note())
	}

	c := startV2Control(t, h)
	users := 0
	for _, m := range c.messages(s.(interface{ AgentSessionID() string }).AgentSessionID()) {
		if m[0] == "user" {
			users++
		}
	}
	if users != 2 {
		t.Errorf("the engine keeps %d prompts, want 2: the first and the third", users)
	}
}

// TestLiveV2Shell: the `!` shell runs a command in the project through the
// control plane and shows it as a bash tool call, completed with its output.
// No prompt is sent.
func TestLiveV2Shell(t *testing.T) {
	h, s, evs := startLiveV2Session(t)
	if err := os.WriteFile(filepath.Join(h.project, "note.txt"), []byte("apple\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mark := evs.len()
	if err := s.(provider.ShellSession).Shell(context.Background(), "cat note.txt"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	time.Sleep(time.Second)
	var call, done *event.Event
	for _, ev := range evs.since(mark) {
		if ev.ToolName == "bash" && ev.Type == event.TypeToolCall {
			call = &ev
		}
		if ev.ToolName == "bash" && ev.Type == event.TypeToolUpdate {
			done = &ev
		}
	}
	if call == nil || done == nil || call.Text != "cat note.txt" || done.Status != event.ToolStatusCompleted ||
		strings.TrimSpace(done.Text) != "apple" || call.ToolID != done.ToolID {
		t.Errorf("shell events: call %+v, done %+v; want a bash call with the command, completed with apple", call, done)
	}
}
