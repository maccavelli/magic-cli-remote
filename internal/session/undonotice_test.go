package session

import (
	"context"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// stubSession is the minimum provider.Session, so the two types below differ in
// exactly one thing: whether they can redo.
type stubSession struct{}

func (stubSession) ID() string                                       { return "s" }
func (stubSession) ProviderID() provider.ID                          { return provider.IDFake }
func (stubSession) AgentSessionID() string                           { return "a" }
func (stubSession) Prompt(context.Context, []provider.Content) error { return nil }
func (stubSession) Cancel(context.Context) error                     { return nil }
func (stubSession) Events() <-chan event.Event                       { return nil }
func (stubSession) Close(context.Context) error                      { return nil }

// undoOnlySession is grok's shape: a rewind with no inverse.
type undoOnlySession struct{ stubSession }

func (undoOnlySession) UndoLast(context.Context) (string, error) { return "", nil }

// undoAndRedoSession is kilo's and opencode's shape.
type undoAndRedoSession struct{ stubSession }

func (undoAndRedoSession) UndoLast(context.Context) (string, error) { return "", nil }
func (undoAndRedoSession) Revert(context.Context, string, string) error {
	return nil
}
func (undoAndRedoSession) Unrevert(context.Context) error { return nil }

// TestUndoNoticeOffersRedoOnlyWhenThereIsOne.
//
// Until MADR 0138 Phase 9 every UndoSession in the tree was also a
// RevertSession, so the notice could promise /redo unconditionally and always
// be right. grok is the first provider that can undo and not redo, and the
// daemon must not offer a command it answers with "This agent can't redo a
// turn."
func TestUndoNoticeOffersRedoOnlyWhenThereIsOne(t *testing.T) {
	var undoOnly provider.Session = undoOnlySession{}
	var both provider.Session = undoAndRedoSession{}

	// Both must genuinely be the shapes this test claims, or it proves nothing.
	if _, ok := undoOnly.(provider.UndoSession); !ok {
		t.Fatal("undoOnlySession does not implement UndoSession")
	}
	if _, ok := undoOnly.(provider.RevertSession); ok {
		t.Fatal("undoOnlySession implements RevertSession; it is meant to be the grok shape")
	}
	if _, ok := both.(provider.RevertSession); !ok {
		t.Fatal("undoAndRedoSession does not implement RevertSession")
	}

	const summary = "reverted 2 files"

	got := undoNotice(undoOnly, summary)
	if !strings.Contains(got, summary) {
		t.Errorf("notice = %q, want it to carry the summary", got)
	}
	if strings.Contains(got, "/redo") {
		t.Errorf("notice = %q — it offers /redo to a session that cannot redo; "+
			"the same daemon answers that command with \"This agent can't redo a turn.\"", got)
	}

	got = undoNotice(both, summary)
	if !strings.Contains(got, "/redo restores it.") {
		t.Errorf("notice = %q, want the redo hint for a session that can redo", got)
	}
}
