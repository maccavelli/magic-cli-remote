package session_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// canonicalFixture is a live fake session: it declares the full canonical table
// and implements every capability the table claims, so these tests exercise the
// real dispatch paths (MADR 0023).
func canonicalFixture(t *testing.T) (*session.Manager, *sync.Mutex, *[]event.Event, session.Meta) {
	t.Helper()
	mgr, mu, events := newFakeManager(t)
	meta, err := mgr.Create(context.Background(), provider.IDFake,
		provider.StartOptions{Name: "canon", Model: "fake-echo"}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close(context.Background(), meta.ID, "dev-a") })
	// The fake announces modes, then usage, then idle. Resolution is re-run as
	// each lands, so wait for the settled list — /context is the last to flip —
	// before asserting on what the session offers.
	waitEventMatching(t, mu, events, event.TypeRemoteCommands, func(ev event.Event) bool {
		for _, c := range ev.RemoteCommands {
			if c.Name == "context" && c.Available {
				return true
			}
		}
		return false
	})
	return mgr, mu, events, meta
}

// noticeContaining waits for a notice holding sub, returning its text.
func noticeContaining(t *testing.T, mu *sync.Mutex, events *[]event.Event, sub string) string {
	t.Helper()
	ev := waitEventMatching(t, mu, events, event.TypeNotice, func(ev event.Event) bool {
		return strings.Contains(ev.Text, sub)
	})
	return ev.Text
}

func notices(mu *sync.Mutex, events *[]event.Event) []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for _, ev := range *events {
		if ev.Type == event.TypeNotice {
			out = append(out, ev.Text)
		}
	}
	return out
}

func TestCanonicalOpCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{name: "compact", command: "/compact", want: "Compacting"},
		// The fake reports usage at start, which is what makes /context work.
		{name: "context", command: "/context", want: "Context: 1,200 tokens of 128,000 (0%)"},
		{name: "diff", command: "/diff", want: "main.go +12"},
		{name: "undo", command: "/undo", want: "Undid the last turn"},
		{name: "redo", command: "/redo", want: "Restored the undone turn"},
		{name: "sessions", command: "/sessions", want: "canon ← this one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, mu, events, meta := canonicalFixture(t)
			if err := mgr.Prompt(context.Background(), meta.ID, tt.command, nil, "dev-a"); err != nil {
				t.Fatalf("%s: %v", tt.command, err)
			}
			noticeContaining(t, mu, events, tt.want)
			// Every daemon-handled command is echoed, so the transcript shows
			// what the user asked for.
			waitEventMatching(t, mu, events, event.TypeUserMessage, func(ev event.Event) bool {
				return ev.Text == tt.command
			})
		})
	}
}

// An in-place model switch must not restart the agent: the conversation is the
// whole point of having the capability.
func TestModelSwitchInPlaceKeepsTheSession(t *testing.T) {
	mgr, mu, events, meta := canonicalFixture(t)
	before := mgr.History(meta.ID)

	if err := mgr.Prompt(context.Background(), meta.ID, "/model fake-slow", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	noticeContaining(t, mu, events, "the conversation is kept")

	// Metadata follows the engine, or /compact and a later resume disagree
	// with it.
	for _, m := range mgr.ListFor("dev-a") {
		if m.ID == meta.ID && m.Model != "fake-slow" {
			t.Fatalf("session model = %q, want fake-slow", m.Model)
		}
	}
	if len(mgr.History(meta.ID)) < len(before) {
		t.Fatal("history shrank: the session was restarted, not switched")
	}
}

// /reset is a spelling of /clear, which restarts the agent.
func TestClearAndResetAreTheSameCommand(t *testing.T) {
	for _, spelling := range []string{"/clear", "/reset"} {
		t.Run(spelling, func(t *testing.T) {
			mgr, mu, events, meta := canonicalFixture(t)
			if err := mgr.Prompt(context.Background(), meta.ID, spelling, nil, "dev-a"); err != nil {
				t.Fatal(err)
			}
			noticeContaining(t, mu, events, "Agent restarted")
		})
	}
}

// The fake declares /goal as the agent's own command, and advertises nothing, so
// it resolves to unavailable — with a reason rather than silence.
func TestUnavailableCanonicalCommandExplainsItself(t *testing.T) {
	mgr, mu, events, meta := canonicalFixture(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/goal ship it", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	text := noticeContaining(t, mu, events, "isn't available here")
	if !strings.Contains(text, "/goal") {
		t.Fatalf("notice does not name the command: %q", text)
	}
	if !strings.Contains(text, "/help") {
		t.Fatalf("notice does not point anywhere useful: %q", text)
	}
}

// A command outside the canonical vocabulary that the agent never advertised is
// still reported rather than sent to the model as literal text.
func TestUnknownCommandStillReportsUnavailable(t *testing.T) {
	mgr, mu, events, meta := canonicalFixture(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/frobnicate", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	noticeContaining(t, mu, events, "isn't available over the remote")
}

// The advertised list is what clients render, so it must mark exactly what this
// session can do — including the reasons for what it cannot.
func TestAdvertisedListMatchesTheSession(t *testing.T) {
	_, mu, events, _ := canonicalFixture(t)
	ev := waitEventMatching(t, mu, events, event.TypeRemoteCommands, func(ev event.Event) bool {
		for _, c := range ev.RemoteCommands {
			// /context flips to available once the fake's first usage report
			// lands; wait for that settled list.
			if c.Name == "context" && c.Available {
				return true
			}
		}
		return false
	})
	byName := map[string]event.RemoteCommand{}
	for _, c := range ev.RemoteCommands {
		byName[c.Name] = c
	}
	for _, want := range []string{"help", "plan", "mode", "model", "context",
		"compact", "clear", "new", "sessions", "goal", "diff", "undo", "redo"} {
		c, ok := byName[want]
		if !ok {
			t.Errorf("/%s missing from the advertised list", want)
			continue
		}
		if c.Description == "" {
			t.Errorf("/%s advertised without a description", want)
		}
		if !c.Available && c.Reason == "" {
			t.Errorf("/%s unavailable with no reason", want)
		}
	}
	if byName["goal"].Available {
		t.Error("/goal must be unavailable on an agent that does not advertise it")
	}
	if !byName["compact"].Available {
		t.Errorf("/compact must be available on a provider that implements it: %q",
			byName["compact"].Reason)
	}
}

// Re-resolution only emits when the answer changed, so a client is not woken for
// every provider event.
func TestAdvertisedListDoesNotRepeatItself(t *testing.T) {
	mgr, mu, events, meta := canonicalFixture(t)
	// Drive some traffic that does not change resolution.
	if err := mgr.Prompt(context.Background(), meta.ID, "hello", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	waitEventMatching(t, mu, events, event.TypeTurnComplete, func(event.Event) bool { return true })

	mu.Lock()
	defer mu.Unlock()
	var lists [][]event.RemoteCommand
	for _, ev := range *events {
		if ev.Type == event.TypeRemoteCommands {
			lists = append(lists, ev.RemoteCommands)
		}
	}
	if len(lists) == 0 {
		t.Fatal("no command list advertised")
	}
	for i := 1; i < len(lists); i++ {
		if equalCommands(lists[i-1], lists[i]) {
			t.Fatalf("advertised the same list twice (%d emissions)", len(lists))
		}
	}
}

func equalCommands(a, b []event.RemoteCommand) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// /help must not offer anything the router would refuse, and must explain the
// rest — the two lists come from the same resolution.
func TestHelpAgreesWithTheRouter(t *testing.T) {
	mgr, mu, events, meta := canonicalFixture(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/help", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	text := noticeContaining(t, mu, events, "You can run:")
	offered, unavailable, _ := strings.Cut(text, ". Not here:")
	if !strings.Contains(offered, "/compact") || !strings.Contains(offered, "/context") {
		t.Fatalf("help omits commands that work here: %q", offered)
	}
	if strings.Contains(offered, "/goal") || !strings.Contains(unavailable, "/goal") {
		t.Fatalf("help misplaces /goal: offered=%q unavailable=%q", offered, unavailable)
	}
	if got := notices(mu, events); len(got) == 0 {
		t.Fatal("no notices recorded")
	}
}
