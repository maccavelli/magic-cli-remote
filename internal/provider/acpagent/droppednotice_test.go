package acpagent

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// turnEvents drives one real turn through Prompt -> beginTurn, with submit
// standing in for the agent, and returns the session's events up to and
// including turn_complete.
func turnEvents(t *testing.T, s *session, submit func(context.Context) (acp.PromptResponse, error)) []event.Event {
	t.Helper()
	s.testSubmit = func(ctx context.Context, _ []acp.ContentBlock) (acp.PromptResponse, error) {
		return submit(ctx)
	}
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	var got []event.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.events:
			got = append(got, ev)
			if ev.Type == event.TypeTurnComplete {
				return got
			}
		case <-deadline:
			t.Fatalf("no turn_complete; events so far: %d", len(got))
		}
	}
}

// TestDroppedUpdatesNoticeIsPerTurnAndPrecedesTurnComplete pins MADR 0167 D18:
// a turn during which the engine connection dropped notifications gets exactly
// one notice, emitted before turn_complete, whichever way the turn ends; a
// turn without drops gets none.
func TestDroppedUpdatesNoticeIsPerTurnAndPrecedesTurnComplete(t *testing.T) {
	endings := []struct {
		name string
		end  func() (acp.PromptResponse, error)
	}{
		{"done", func() (acp.PromptResponse, error) { return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil }},
		{"cancelled", func() (acp.PromptResponse, error) { return acp.PromptResponse{}, context.Canceled }},
		{"errored", func() (acp.PromptResponse, error) { return acp.PromptResponse{}, errors.New("agent exploded") }},
	}
	for _, ending := range endings {
		for _, drops := range []uint64{0, 1, 7} {
			t.Run(ending.name+"/drops="+strconv.FormatUint(drops, 10), func(t *testing.T) {
				s := newQueueTestSession()
				var counter atomic.Uint64
				counter.Store(40) // drops from earlier turns must not count against this one
				s.testDropped = counter.Load

				evs := turnEvents(t, s, func(context.Context) (acp.PromptResponse, error) {
					counter.Add(drops) // the SDK dropped these while the turn was running
					return ending.end()
				})

				var notices []int
				for i, ev := range evs {
					if ev.Type == event.TypeNotice && strings.Contains(ev.Text, "may be incomplete") {
						notices = append(notices, i)
					}
				}
				last := len(evs) - 1 // turn_complete
				switch {
				case drops == 0 && len(notices) != 0:
					t.Fatalf("a turn with no drops got a loss notice: %q", evs[notices[0]].Text)
				case drops > 0 && len(notices) != 1:
					t.Fatalf("got %d loss notices, want exactly 1", len(notices))
				case drops > 0 && notices[0] >= last:
					t.Fatalf("the loss notice came after turn_complete")
				case drops > 0:
					want := map[uint64]string{1: " 1 was dropped", 7: " 7 were dropped"}[drops]
					if text := evs[notices[0]].Text; !strings.Contains(text, want) {
						t.Fatalf("notice %q does not state %q", text, want)
					}
				}
			})
		}
	}
}

// TestDroppedNotificationsWithoutAConnectionIsZero keeps the helper safe on a
// session that has no transport yet — the state before spawn, and in tests.
func TestDroppedNotificationsWithoutAConnectionIsZero(t *testing.T) {
	s := newQueueTestSession()
	if got := s.droppedNotifications(); got != 0 {
		t.Fatalf("droppedNotifications() = %d with no connection, want 0", got)
	}
}
