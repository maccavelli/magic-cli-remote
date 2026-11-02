package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// newExtSession is a session with no agent process behind it: the extension
// handlers only touch the event stream and the waiter maps.
func newExtSession(timeout time.Duration) *session {
	return &session{
		providerID: "vendor",
		localID:    "l1",
		agentID:    "a1",
		events:     make(chan event.Event, 32),
		done:       make(chan struct{}),
		log:        slog.Default(),
		pending:    make(map[string]*permWaiter),
		questions:  make(map[string]*questionWaiter),
		attached:   true,
		cfg:        Config{PermissionTimeout: timeout},
	}
}

// waitEvent returns the next event of type want, skipping others.
func waitEvent(t *testing.T, s *session, want event.Type) event.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

// Anything no Spec registered gets the SDK's own answer, so an agent that adds
// an extension we have not built stays diagnosable instead of looking like a
// silent hang.
func TestUnknownExtensionMethodIsMethodNotFound(t *testing.T) {
	s := newExtSession(0)
	_, err := s.HandleExtensionMethod(context.Background(), "_vendor/not_implemented", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error for an unknown extension method")
	}
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("error type %T (%v), want *acp.RequestError", err, err)
	}
	if reqErr.Code != -32601 {
		t.Fatalf("code = %d, want -32601 (method not found)", reqErr.Code)
	}
}

// question.respond clears the answers once RespondQuestion returns, but the
// waiting handler reads them later on the agent's goroutine. The result must
// not share the caller's slices, or the agent can receive blank answers (MADR
// 0191, PLAN L6).
func TestRespondQuestionOwnsTheAnswers(t *testing.T) {
	s := newExtSession(0)
	w := &questionWaiter{ch: make(chan questionResult, 1)}
	s.mu.Lock()
	s.questions = map[string]*questionWaiter{"q1": w}
	s.mu.Unlock()

	answers := provider.QuestionAnswers{"0": {"Hello", "Hi"}}
	if err := s.RespondQuestion(context.Background(), "q1", answers, false); err != nil {
		t.Fatal(err)
	}
	answers.Clear()

	res := <-w.ch
	if len(res.answers) != 1 || !slices.Equal(res.answers[0], []string{"Hello", "Hi"}) {
		t.Fatalf("answers after the caller cleared its copy = %q, want [[Hello Hi]]", res.answers)
	}
}

// Answering a form twice (or after it expired) must fail rather than block on a
// waiter nobody owns.
func TestRespondQuestionUnknownID(t *testing.T) {
	s := newExtSession(0)
	if err := s.RespondQuestion(context.Background(), "nope", nil, false); err == nil {
		t.Fatal("want an error for an unknown question id")
	}
}
