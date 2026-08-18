package httpagent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// replySpy records permission/question replies made by the expiry fail-safes.
type replySpy struct {
	fakeDialectSession
	mu    sync.Mutex
	perms []string
	q     []string
}

func (r *replySpy) RespondPermission(_ context.Context, id, _ string, cancelled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancelled {
		r.perms = append(r.perms, id)
	}
	return nil
}

func (r *replySpy) RespondQuestion(_ context.Context, id string, _ [][]string, cancelled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancelled {
		r.q = append(r.q, id)
	}
	return nil
}

func (r *replySpy) rejected() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.perms...), append([]string(nil), r.q...)
}

func newExpirySession(t *testing.T, timeout time.Duration) (*session, *replySpy) {
	t.Helper()
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{PermissionTimeout: timeout}, nil)
	s, _ := newTestSession(p)
	s.questionPending = map[string]struct{}{}
	spy := &replySpy{}
	spy.h = s
	s.ds = spy
	return s, spy
}

// drainFor collects events until the predicate is satisfied or time runs out.
func drainFor(t *testing.T, s *session, want func(event.Event) bool) event.Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if want(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for event")
		}
	}
}

// A permission the phone never answers must be rejected server-side, or the
// agent waits on it forever and the turn never ends.
func TestPermissionExpiryRejectsAndNotifies(t *testing.T) {
	s, spy := newExpirySession(t, 20*time.Millisecond)
	s.TrackPermissionOrigin("perm1", "child-9")
	s.TrackPermission("perm1")

	if got := s.PermissionOrigin("perm1"); got != "child-9" {
		t.Fatalf("origin=%q want child-9 (replies must route to the asking child)", got)
	}

	resolved := drainFor(t, s, func(ev event.Event) bool {
		return ev.Type == event.TypePermissionResolved
	})
	if resolved.PermissionID != "perm1" || resolved.Status != event.PermissionStatusCancelled {
		t.Fatalf("resolved=%+v want perm1 cancelled", resolved)
	}
	// The wire contract reserves timed_out for exactly this expiry
	// (protocol-v1.md; MADR 0101 B) — without it the phone cannot leave a
	// "timed out" tombstone instead of silently deleting the notification.
	if !resolved.TimedOut {
		t.Fatal("expiry resolution must carry TimedOut")
	}
	perms, _ := spy.rejected()
	if len(perms) != 1 || perms[0] != "perm1" {
		t.Fatalf("engine rejects=%v want [perm1]", perms)
	}
	// The id is consumed, so a late real answer is refused rather than double-replied.
	if s.TakePending("perm1") {
		t.Fatal("expired permission must no longer be pending")
	}
	// Origin is cleared once the reply lands.
	if got := s.PermissionOrigin("perm1"); got != s.agentID {
		t.Fatalf("origin=%q want the parent after expiry cleanup", got)
	}
}

func TestQuestionExpiryRejectsAndNotifies(t *testing.T) {
	s, spy := newExpirySession(t, 20*time.Millisecond)
	s.TrackQuestion("q1")

	resolved := drainFor(t, s, func(ev event.Event) bool {
		return ev.Type == event.TypeQuestionResolved
	})
	if resolved.QuestionID != "q1" || resolved.Status != event.PermissionStatusCancelled {
		t.Fatalf("resolved=%+v want q1 cancelled", resolved)
	}
	if !resolved.TimedOut {
		t.Fatal("question expiry resolution must carry TimedOut (MADR 0101 B)")
	}
	_, quests := spy.rejected()
	if len(quests) != 1 || quests[0] != "q1" {
		t.Fatalf("engine rejects=%v want [q1]", quests)
	}
	if s.TakeQuestionPending("q1") {
		t.Fatal("expired question must no longer be pending")
	}
}

// Answering before the timeout must disarm the fail-safe: a second, spurious
// reject would cancel work the user actually approved.
func TestPermissionAnsweredBeforeExpiryIsNotRejected(t *testing.T) {
	s, spy := newExpirySession(t, 60*time.Millisecond)
	s.TrackPermission("perm1")

	if err := s.RespondPermission(context.Background(), "perm1", "once", false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	// The answered resolution must NOT be marked TimedOut: the contract
	// reserves the flag for the timer expiry alone (MADR 0101 B).
	resolved := drainFor(t, s, func(ev event.Event) bool {
		return ev.Type == event.TypePermissionResolved
	})
	if resolved.TimedOut {
		t.Fatal("a human-answered resolution must not carry TimedOut")
	}
	time.Sleep(150 * time.Millisecond)

	perms, _ := spy.rejected()
	if len(perms) != 0 {
		t.Fatalf("expiry rejected an already-answered permission: %v", perms)
	}
}

// Closing the session must not leave expiry goroutines parked on their timers.
func TestExpiryStopsOnClose(t *testing.T) {
	s, spy := newExpirySession(t, time.Hour)
	s.TrackPermission("perm1")
	s.TrackQuestion("q1")

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	perms, quests := spy.rejected()
	if len(perms) != 0 || len(quests) != 0 {
		t.Fatalf("close must not trigger engine rejects: perms=%v quests=%v", perms, quests)
	}
	// Close still cancels the sheets locally so no card is stranded on the phone.
	var sawPerm, sawQuestion bool
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypePermissionResolved && ev.Status == event.PermissionStatusCancelled {
				sawPerm = true
			}
			if ev.Type == event.TypeQuestionResolved && ev.Status == event.PermissionStatusCancelled {
				sawQuestion = true
			}
			continue
		default:
		}
		break
	}
	if !sawPerm || !sawQuestion {
		t.Fatalf("close must cancel open sheets: perm=%v question=%v", sawPerm, sawQuestion)
	}
}

// A failed engine reply leaves the permission outstanding so the user can retry
// instead of the sheet vanishing with the agent still blocked.
func TestRespondPermissionRestoresPendingOnError(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.ds = &failingReplyDialect{}
	s.TrackPermission("perm1")

	if err := s.RespondPermission(context.Background(), "perm1", "once", false, "dev-1"); err == nil {
		t.Fatal("expected reply error")
	}
	s.mu.Lock()
	_, still := s.pending["perm1"]
	s.mu.Unlock()
	if !still {
		t.Fatal("failed reply must leave the permission answerable")
	}
}

func TestRespondQuestionRestoresPendingOnError(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.questionPending = map[string]struct{}{}
	s.ds = &failingReplyDialect{}
	s.TrackQuestion("q1")

	if err := s.RespondQuestion(context.Background(), "q1", nil, false); err == nil {
		t.Fatal("expected reply error")
	}
	s.mu.Lock()
	_, still := s.questionPending["q1"]
	s.mu.Unlock()
	if !still {
		t.Fatal("failed reply must leave the question answerable")
	}
}

// Answering an unknown id is a client bug, not something to forward to the engine.
func TestRespondUnknownIDsAreRefused(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.questionPending = map[string]struct{}{}
	if err := s.RespondPermission(context.Background(), "nope", "once", false, "dev-1"); err == nil {
		t.Fatal("expected error for unknown permission")
	}
	if err := s.RespondQuestion(context.Background(), "nope", nil, false); err == nil {
		t.Fatal("expected error for unknown question")
	}
}

type failingReplyDialect struct{ fakeDialectSession }

func (*failingReplyDialect) RespondPermission(context.Context, string, string, bool) error {
	return errReplyFailed
}
func (*failingReplyDialect) RespondQuestion(context.Context, string, [][]string, bool) error {
	return errReplyFailed
}

var errReplyFailed = errStr("engine refused reply")

type errStr string

func (e errStr) Error() string { return string(e) }

// EmitReplay feeds the history ring pre-attach: it must never block, dropping
// the OLDEST events so the most recent conversation survives a full buffer.
func TestEmitReplayDropsOldestNeverBlocks(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	capacity := cap(s.events)

	done := make(chan struct{})
	go func() {
		for i := range capacity * 2 {
			s.EmitReplay(event.Event{Type: event.TypeAssistantChunk, Text: string(rune('a' + i%26))})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("EmitReplay blocked on a full buffer")
	}

	var got []event.Event
	for {
		select {
		case ev := <-s.events:
			got = append(got, ev)
			continue
		default:
		}
		break
	}
	if len(got) == 0 {
		t.Fatal("no replay events buffered")
	}
	for _, ev := range got {
		if !ev.Replay {
			t.Fatal("EmitReplay must stamp Replay=true")
		}
	}
	// The tail survived: the last event written is the last one readable.
	last := got[len(got)-1]
	want := string(rune('a' + (capacity*2-1)%26))
	if last.Text != want {
		t.Fatalf("last buffered=%q want %q (oldest, not newest, must be dropped)", last.Text, want)
	}
}

// A dead engine must surface as an error plus a disconnected status, which is
// what flips the session out of "live" for the manager.
func TestServerDiedEmitsDisconnected(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "testbin"}, nil)
	s, _ := newTestSession(p)
	s.serverDied()

	var sawErr, sawStatus bool
	for range 2 {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeError && strings.Contains(ev.Error, "testbin") {
				sawErr = true
			}
			if ev.Type == event.TypeSessionStatus && ev.Status == "disconnected" {
				sawStatus = true
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
	if !sawErr || !sawStatus {
		t.Fatalf("err=%v status=%v", sawErr, sawStatus)
	}
}

// A closed session accepts no more prompts.
func TestPromptAfterCloseFails(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	_ = s.Close(context.Background())
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "x"}}); err == nil {
		t.Fatal("expected error prompting a closed session")
	}
}
