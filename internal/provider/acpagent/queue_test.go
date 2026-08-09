package acpagent

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func newQueueTestSession() *session {
	return &session{
		localID: "local-q",
		agentID: "agent-q",
		events:  make(chan event.Event, 64),
		done:    make(chan struct{}),
		log:     slog.Default(),
		pending: make(map[string]*permWaiter),
	}
}

func TestPromptQueuesWhileTurnActive(t *testing.T) {
	s := newQueueTestSession()
	hold := make(chan struct{})
	var promptN atomic.Int32
	s.testSubmit = func(ctx context.Context, blocks []acp.ContentBlock) (acp.PromptResponse, error) {
		promptN.Add(1)
		select {
		case <-hold:
		case <-ctx.Done():
			return acp.PromptResponse{}, ctx.Err()
		}
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	// First prompt starts a turn (blocks on hold).
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "one"}}); err != nil {
		t.Fatalf("first: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for promptN.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("first prompt never reached submit")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second prompt should queue (not ErrTurnBusy).
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "two"}}); err != nil {
		t.Fatalf("second: %v", err)
	}
	s.mu.Lock()
	qn := len(s.promptQueue)
	s.mu.Unlock()
	if qn != 1 {
		t.Fatalf("queue len=%d want 1", qn)
	}
	if promptN.Load() != 1 {
		t.Fatalf("submits=%d want 1 (queued not submitted)", promptN.Load())
	}

	// Release first turn; drain should submit the second.
	close(hold)
	deadline = time.Now().Add(2 * time.Second)
	for promptN.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("queued prompt never drained, submits=%d", promptN.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Wait for second turn to finish (hold already closed).
	deadline = time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		active := s.prompting
		qn = len(s.promptQueue)
		s.mu.Unlock()
		if !active && qn == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still active=%v queue=%d", active, qn)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPromptQueueOverflowReturnsTurnBusy(t *testing.T) {
	s := newQueueTestSession()
	s.mu.Lock()
	s.prompting = true
	s.mu.Unlock()

	for i := 0; i < maxPromptQueue; i++ {
		if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "q"}}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "overflow"}})
	if err != provider.ErrTurnBusy {
		t.Fatalf("overflow err=%v want ErrTurnBusy", err)
	}
}

func TestPromptQueueBlockedByPendingPermission(t *testing.T) {
	s := newQueueTestSession()
	var promptN atomic.Int32
	s.testSubmit = func(ctx context.Context, blocks []acp.ContentBlock) (acp.PromptResponse, error) {
		promptN.Add(1)
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	s.mu.Lock()
	s.prompting = true
	s.promptQueue = [][]provider.Content{{{Type: "text", Text: "queued"}}}
	s.pending = map[string]*permWaiter{"p1": {ch: make(chan permResult, 1)}}
	s.mu.Unlock()

	// Simulate turn end while permission still pending.
	s.mu.Lock()
	s.prompting = false
	s.mu.Unlock()
	s.tryDrainQueue()
	if promptN.Load() != 0 {
		t.Fatal("must not drain while permission pending")
	}

	// Resolve permission → drain.
	s.mu.Lock()
	delete(s.pending, "p1")
	s.mu.Unlock()
	s.tryDrainQueue()
	deadline := time.Now().Add(2 * time.Second)
	for promptN.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("after permission clear submits=%d", promptN.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCancelClearsPromptQueue(t *testing.T) {
	s := newQueueTestSession()
	s.testCancel = func(context.Context) error { return nil }
	s.mu.Lock()
	s.prompting = true
	s.promptQueue = [][]provider.Content{{{Type: "text", Text: "x"}}}
	s.mu.Unlock()
	if err := s.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	n := len(s.promptQueue)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("queue after cancel=%d", n)
	}
}

func TestQueuedPromptEmitsUserMessage(t *testing.T) {
	s := newQueueTestSession()
	s.mu.Lock()
	s.prompting = true
	s.mu.Unlock()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hello-q"}}); err != nil {
		t.Fatal(err)
	}
	var sawUser, sawNotice bool
	deadline := time.After(time.Second)
	for !sawUser || !sawNotice {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeUserMessage && ev.Text == "hello-q" {
				sawUser = true
			}
			if ev.Type == event.TypeNotice && len(ev.Text) > 0 {
				sawNotice = true
			}
		case <-deadline:
			t.Fatalf("sawUser=%v sawNotice=%v", sawUser, sawNotice)
		}
	}
}

func TestRespondPermissionDrainsQueueWhenIdle(t *testing.T) {
	s := newQueueTestSession()
	var promptN atomic.Int32
	s.testSubmit = func(ctx context.Context, blocks []acp.ContentBlock) (acp.PromptResponse, error) {
		promptN.Add(1)
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	w := &permWaiter{ch: make(chan permResult, 1)}
	s.mu.Lock()
	s.promptQueue = [][]provider.Content{{{Type: "text", Text: "after-perm"}}}
	s.pending["p1"] = w
	s.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-w.ch // RespondPermission sends here
	}()

	if err := s.RespondPermission(context.Background(), "p1", "once", false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for promptN.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("RespondPermission did not drain queue, submits=%d", promptN.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
