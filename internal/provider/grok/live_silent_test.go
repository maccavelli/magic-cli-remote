//go:build live_grok

package grok_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run SilentCommands -count=1 -v
//
// MADR 0208 P4b's acceptance, through the daemon against a real grok:
//
//   - /compact is grok's own command again, and its compaction reaches the
//     phone, so it is not marked silent (P4b deviation 1);
//   - /always-approve ends its turn with nothing over ACP, so P4a marks it;
//   - a bare /feedback answers with its usage line, so it is not marked.
//
// It spends tokens: one short turn, so /compact has a conversation to compact.
func TestLiveGrokSilentCommandsThroughTheDaemon(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	reg := provider.NewRegistry()
	reg.Register(p)
	dataDir := t.TempDir()
	store, err := session.OpenStore(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	var sink silentSink
	mgr := session.NewManager(reg, store, nil, sink.add)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	meta, err := mgr.Create(ctx, provider.IDGrok,
		provider.StartOptions{Name: "silent-live", CWD: t.TempDir()}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer mgr.Close(context.Background(), meta.ID, "dev-a")

	sink.waitFor(t, "grok to advertise compact, always-approve and feedback", func(evs []event.Event) bool {
		names := advertisedNames(evs)
		return slices.Contains(names, "compact") && slices.Contains(names, "always-approve") &&
			slices.Contains(names, "feedback")
	})
	sink.waitFor(t, "/compact offered as available", func(evs []event.Event) bool {
		r, ok := lastRemote(evs, "compact")
		return ok && r.Available
	})

	turn := func(text string) []event.Event {
		t.Helper()
		start := len(sink.snapshot())
		if err := mgr.Prompt(ctx, meta.ID, text, nil, "dev-a"); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		sink.waitFor(t, "the "+text+" turn to end", func(evs []event.Event) bool {
			return countType(evs[start:], event.TypeTurnComplete) > 0
		})
		// The mark is written after the turn's pump releases the session.
		time.Sleep(2 * time.Second)
		evs := sink.snapshot()[start:]
		for _, ev := range evs {
			switch ev.Type {
			case event.TypeCompaction:
				t.Logf("%s: compaction id=%q status=%q summary=%q error=%q",
					text, ev.CompactionID, ev.CompactionStatus, ev.CompactionSummary, ev.CompactionError)
			case event.TypeNotice, event.TypeError:
				t.Logf("%s: %s %q", text, ev.Type, ev.Text)
			case event.TypeAssistantChunk:
				t.Logf("%s: assistant %q", text, ev.Text)
			}
		}
		return evs
	}

	turn("Reply with the single word: ready")

	evs := turn("/compact")
	if !slices.ContainsFunc(evs, func(ev event.Event) bool {
		return ev.Type == event.TypeCompaction && ev.CompactionStatus == "completed"
	}) {
		t.Error("/compact: no completed compaction reached the phone")
	}
	turn("/always-approve")
	turn("/feedback")

	marks, err := session.LoadSilentMarks(dataDir)
	if err != nil {
		t.Fatalf("load marks: %v", err)
	}
	var marked []string
	for _, m := range marks {
		t.Logf("mark: provider=%s version=%s command=%s", m.Provider, m.Version, m.Command)
		marked = append(marked, m.Command)
	}
	if slices.Contains(marked, "compact") {
		t.Error("/compact was marked silent, but grok compacted")
	}
	if !slices.Contains(marked, "always-approve") {
		t.Error("/always-approve was not marked silent")
	}
	if slices.Contains(marked, "feedback") {
		t.Error("/feedback was marked silent, but grok answered with its usage line")
	}
	if !slices.ContainsFunc(sink.snapshot(), func(ev event.Event) bool {
		return ev.Type == event.TypeNotice && strings.Contains(ev.Text, "/always-approve” produced nothing")
	}) {
		t.Error("no notice told the phone /always-approve produced nothing")
	}
}

// silentSink records every event the manager emits.
type silentSink struct {
	mu  sync.Mutex
	evs []event.Event
}

func (s *silentSink) add(ev event.Event) {
	s.mu.Lock()
	s.evs = append(s.evs, ev)
	s.mu.Unlock()
}

func (s *silentSink) snapshot() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.evs)
}

func (s *silentSink) waitFor(t *testing.T, what string, ok func([]event.Event) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if ok(s.snapshot()) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func advertisedNames(evs []event.Event) []string {
	var names []string
	for _, ev := range evs {
		if ev.Type == event.TypeAvailableCommands {
			for _, c := range ev.Commands {
				names = append(names, strings.TrimPrefix(c.Name, "/"))
			}
		}
	}
	return names
}

func lastRemote(evs []event.Event, name string) (event.RemoteCommand, bool) {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != event.TypeRemoteCommands {
			continue
		}
		for _, c := range evs[i].RemoteCommands {
			if c.Name == name {
				return c, true
			}
		}
	}
	return event.RemoteCommand{}, false
}

func countType(evs []event.Event, typ event.Type) int {
	n := 0
	for _, ev := range evs {
		if ev.Type == typ {
			n++
		}
	}
	return n
}
