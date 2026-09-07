package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// noticeProvider emits a scripted sequence of notices on Start.
type noticeProvider struct {
	notices []scriptedNotice
}

type scriptedNotice struct {
	text   string
	replay bool
}

func (p *noticeProvider) ID() provider.ID { return "noticer" }
func (p *noticeProvider) Ready() bool     { return true }

func (p *noticeProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "n1"
	}
	s := &noticeSession{id: id, events: make(chan event.Event, 256)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle",
		Timestamp: time.Now().UTC()}
	for _, n := range p.notices {
		s.events <- event.Event{
			Type: event.TypeNotice, SessionID: id, Text: n.text,
			Timestamp: time.Now().UTC(), Replay: n.replay,
		}
	}
	return s, nil
}

type noticeSession struct {
	id     string
	events chan event.Event
}

func (s *noticeSession) ID() string                                       { return s.id }
func (s *noticeSession) ProviderID() provider.ID                          { return "noticer" }
func (s *noticeSession) AgentSessionID() string                           { return s.id }
func (s *noticeSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *noticeSession) Cancel(context.Context) error                     { return nil }
func (s *noticeSession) Events() <-chan event.Event                       { return s.events }
func (s *noticeSession) Close(context.Context) error                      { return nil }

// runNoticeSession returns the notices that reached session history.
//
// History rather than the broadcast sink: replayed events are deliberately not
// broadcast (manager.go: `mine && !ev.Replay && m.onEvent != nil`), so a sink
// would report zero for the replay case and the assertion would pass for the
// wrong reason. History is also the thing that matters — it is what a client
// reconstructs the conversation from, and what the suppression edits.
func runNoticeSession(t *testing.T, p *noticeProvider) []event.Event {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := session.NewManager(reg, nil, nil, nil)
	meta, err := mgr.Create(context.Background(), "noticer", provider.StartOptions{
		Name: "s", LocalSessionID: "n1",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(context.Background(), meta.ID, "dev-a") })
	// The pump is asynchronous; give every scripted event time to land.
	time.Sleep(300 * time.Millisecond)

	var notices []event.Event
	for _, ev := range mgr.History(meta.ID) {
		if ev.Type == event.TypeNotice {
			notices = append(notices, ev)
		}
	}
	return notices
}

// TestRepeatedNoticesAreSuppressed is MADR 0137 F6a.
//
// One codex session recorded 77 copies of a single upstream deprecation
// warning. Each one crossed the websocket, was appended to session history and
// rendered on the phone as a fresh message, so a once-per-engine notice read as
// a session going wrong 77 times.
//
// The guard is in the session event pump rather than at the 42 TypeNotice emit
// sites across six provider packages: that is where every provider's events
// converge, so one guard covers all of them and every future site — which a
// per-site guard would silently let opt out.
func TestRepeatedNoticesAreSuppressed(t *testing.T) {
	const dep = "codex_hooks is deprecated; use hooks"
	notices := runNoticeSession(t, &noticeProvider{notices: []scriptedNotice{
		{text: dep}, {text: dep}, {text: dep}, {text: dep}, {text: dep},
		{text: "MCP server \"x\" failed to start"},
		{text: dep},
	}})

	if len(notices) != 3 {
		texts := make([]string, len(notices))
		for i, n := range notices {
			texts[i] = n.Text
		}
		t.Fatalf("got %d notices %q, want 3: the repeated deprecation once, the "+
			"different notice, then the deprecation again", len(notices), texts)
	}
	if notices[0].Text != dep || notices[1].Text == dep || notices[2].Text != dep {
		t.Fatalf("wrong sequence: %q, %q, %q", notices[0].Text, notices[1].Text, notices[2].Text)
	}
}

// TestAReplayDoesNotPoisonTheDeduper is the boundary the guard must not cross,
// and it is a narrower claim than it first appears.
//
// Identical REPLAYED notices were already collapsed before this change:
// appendHistoryLocked de-duplicates any replayed event against existing
// history (manager.go:202-208), so asserting "three replays produce three
// entries" would fail for a reason that has nothing to do with this guard.
//
// What `!ev.Replay` actually protects is the deduper's STATE. session/load
// re-emits the prior conversation; if those events updated the deduper, a
// notice replayed from history would silence the next genuine live one — the
// engine would report something real and the phone would never see it, because
// the same words appeared in a transcript being reconstructed.
func TestAReplayDoesNotPoisonTheDeduper(t *testing.T) {
	const dep = "codex_hooks is deprecated; use hooks"
	notices := runNoticeSession(t, &noticeProvider{notices: []scriptedNotice{
		{text: dep, replay: true},
		{text: dep},
	}})
	if len(notices) != 2 {
		t.Fatalf("got %d notices, want 2: a live notice was suppressed because "+
			"a replayed one had the same text", len(notices))
	}
	if notices[0].Replay == notices[1].Replay {
		t.Fatalf("expected one replayed and one live notice, got replay=%v and %v",
			notices[0].Replay, notices[1].Replay)
	}
}
