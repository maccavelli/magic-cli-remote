package ws_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

// PLAN 0191 L1: question.respond carries a decline, and a provider's
// validation refusal reaches the phone as question_invalid, so it can keep the
// form open (MADR 0191 D3, D4).

type formSession struct {
	provider.Session
	mu       sync.Mutex
	declined []string
	refuse   bool
}

func (s *formSession) RespondQuestion(context.Context, string, provider.QuestionAnswers, bool) error {
	if s.refuse {
		return fmt.Errorf("%w: age: must be a whole number", provider.ErrQuestionInvalid)
	}
	return nil
}

func (s *formSession) DeclineQuestion(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.declined = append(s.declined, id)
	return nil
}

type formProvider struct {
	provider.Provider
	refuse bool
	sess   *formSession
}

func (p *formProvider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	s, err := p.Provider.Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	p.sess = &formSession{Session: s, refuse: p.refuse}
	return p.sess, nil
}

func TestWSQuestionDeclineReachesTheSession(t *testing.T) {
	fp := &formProvider{Provider: fake.New()}
	ws := setupWSSessionWithProvider(t, "test", fp)
	defer ws.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeQuestionRespond, "decline", protocol.QuestionRespondPayload{
		SessionID: ws.meta.ID, QuestionID: "q1", Declined: true,
	})
	ws.send(t, env)
	if got := ws.recvSkipEvents(t); got.Type != protocol.TypeOK {
		t.Fatalf("want ok, got %s payload=%s", got.Type, string(got.Payload))
	}
	fp.sess.mu.Lock()
	defer fp.sess.mu.Unlock()
	if len(fp.sess.declined) != 1 || fp.sess.declined[0] != "q1" {
		t.Fatalf("declined %v, want [q1]", fp.sess.declined)
	}
}

func TestWSQuestionRefusalIsQuestionInvalid(t *testing.T) {
	fp := &formProvider{Provider: fake.New(), refuse: true}
	ws := setupWSSessionWithProvider(t, "test", fp)
	defer ws.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeQuestionRespond, "bad", protocol.QuestionRespondPayload{
		SessionID: ws.meta.ID, QuestionID: "q1", Answers: provider.QuestionAnswers{"age": {"x"}},
	})
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type != protocol.TypeError {
		t.Fatalf("want error, got %s", got.Type)
	}
	var e protocol.ErrorPayload
	if err := json.Unmarshal(got.Payload, &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != protocol.ErrQuestionInvalid {
		t.Fatalf("code = %q (%s), want %q", e.Code, e.Message, protocol.ErrQuestionInvalid)
	}
}
