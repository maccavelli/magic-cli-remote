package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestParseReviewArg(t *testing.T) {
	cases := []struct {
		in      string
		kind    string
		wantErr bool
	}{
		{"", provider.ReviewUncommitted, false},
		{"uncommitted", provider.ReviewUncommitted, false},
		{"base main", provider.ReviewBaseBranch, false},
		{"commit abcdef", provider.ReviewCommit, false},
		{"custom look at tests", provider.ReviewCustom, false},
		{"base", "", true},
		{"commit", "", true},
		{"custom", "", true},
		{"base   ", "", true},
		{"nope", "", true},
	}
	for _, tc := range cases {
		got, err := provider.ParseReviewArg(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: want error", tc.in)
			}
			continue
		}
		if err != nil || got.Kind != tc.kind {
			t.Fatalf("%q: %+v err=%v", tc.in, got, err)
		}
	}
}

func TestReviewTargetJSONShapes(t *testing.T) {
	uncommitted, err := reviewTargetJSON(provider.ReviewTarget{Kind: provider.ReviewUncommitted})
	if err != nil || uncommitted["type"] != provider.ReviewUncommitted {
		t.Fatalf("%v %v", uncommitted, err)
	}
	base, err := reviewTargetJSON(provider.ReviewTarget{Kind: provider.ReviewBaseBranch, Branch: "main"})
	if err != nil || base["branch"] != "main" {
		t.Fatalf("%v %v", base, err)
	}
	commit, err := reviewTargetJSON(provider.ReviewTarget{Kind: provider.ReviewCommit, SHA: "deadbeef"})
	if err != nil {
		t.Fatal(err)
	}
	if commit["sha"] != "deadbeef" {
		t.Fatalf("%v", commit)
	}
	if _, ok := commit["title"]; !ok || commit["title"] != nil {
		t.Fatalf("commit title must be JSON null: %#v", commit["title"])
	}
	custom, err := reviewTargetJSON(provider.ReviewTarget{Kind: provider.ReviewCustom, Instructions: "be brief"})
	if err != nil || custom["instructions"] != "be brief" {
		t.Fatalf("%v %v", custom, err)
	}
	if _, err := reviewTargetJSON(provider.ReviewTarget{Kind: provider.ReviewBaseBranch}); err == nil {
		t.Fatal("empty branch")
	}
}

func seedReviewSession(t *testing.T) (*session, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t), sessions: map[string]*session{}}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.agentID = "thread-1"
	s.engineGeneration = 1
	s.thinkingLevel = "high"
	s.serviceTier = "priority"
	s.personality = "friendly"
	p.sessions[s.agentID] = s
	return s, engineR, engineW
}

func TestReviewStartSendsInlineAndPreservesSettings(t *testing.T) {
	s, engineR, engineW := seedReviewSession(t)
	before := s.snapshotSettings()
	got := make(chan map[string]any, 1)
	go func() {
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(engineR).Decode(&req); err != nil {
			return
		}
		got <- map[string]any{"method": req.Method, "params": req.Params}
		b, _ := json.Marshal(map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"turn":           map[string]any{"id": "turn-r", "status": "inProgress"},
				"reviewThreadId": "review-thread",
			},
		})
		_, _ = engineW.Write(append(b, '\n'))
	}()
	if err := s.StartReview(context.Background(), provider.ReviewTarget{Kind: provider.ReviewCustom, Instructions: "nits only"}); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-got:
		if req["method"] != "review/start" {
			t.Fatalf("method = %v", req["method"])
		}
		params := req["params"].(map[string]any)
		if params["delivery"] != "inline" {
			t.Fatalf("delivery = %#v", params["delivery"])
		}
		if _, ok := params["collaborationMode"]; ok {
			t.Fatal("review must not rewrite collaboration")
		}
		if _, ok := params["serviceTier"]; ok {
			t.Fatal("review must not send Fast")
		}
		target := params["target"].(map[string]any)
		if target["type"] != provider.ReviewCustom || target["instructions"] != "nits only" {
			t.Fatalf("target = %#v", target)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	waitFor(t, "alias", func() bool {
		s.p.mu.Lock()
		defer s.p.mu.Unlock()
		return s.p.sessions["review-thread"] == s
	})
	after := s.snapshotSettings()
	if after.Model != before.Model || after.Thinking != before.Thinking ||
		after.Collab != before.Collab || after.Approval != before.Approval ||
		after.Tier != before.Tier || after.Persona != before.Persona {
		t.Fatalf("settings mutated: %+v -> %+v", before, after)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestReviewBusyRejectsPromptAndSecondReview(t *testing.T) {
	s, engineR, engineW := seedReviewSession(t)
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		b, _ := json.Marshal(map[string]any{"id": req.ID, "result": map[string]any{
			"turn": map[string]any{"id": "t1", "status": "inProgress"},
		}})
		_, _ = engineW.Write(append(b, '\n'))
	}()
	if err := s.StartReview(context.Background(), provider.ReviewTarget{Kind: provider.ReviewUncommitted}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reviewing", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.reviewing && s.turnBusy
	})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("prompt during review: %v", err)
	}
	if err := s.StartReview(context.Background(), provider.ReviewTarget{Kind: provider.ReviewUncommitted}); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("second review: %v", err)
	}
}

func TestReviewIgnoresUnrelatedThread(t *testing.T) {
	s, _, _ := seedReviewSession(t)
	s.p.routeNotification("item/agentMessage/delta", []byte(`{"threadId":"other","itemId":"i","delta":"nope"}`))
	select {
	case ev := <-s.Events():
		if ev.Type == event.TypeAssistantChunk {
			t.Fatalf("unrelated thread leaked: %+v", ev)
		}
	default:
	}
}

func TestReviewFallbackOnceWithoutAssistantText(t *testing.T) {
	s := seededCollabSession(t)
	s.reviewing = true
	s.handleReviewItem("enteredReviewMode", []byte(`{"type":"enteredReviewMode","id":"e1"}`), true)
	s.handleReviewItem("exitedReviewMode", []byte(`{"type":"exitedReviewMode","id":"x1","review":"looks fine"}`), true)
	s.handleReviewItem("exitedReviewMode", []byte(`{"type":"exitedReviewMode","id":"x1","review":"looks fine"}`), false)
	var notices, assistants []string
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeNotice:
				notices = append(notices, ev.Text)
			case event.TypeAssistantChunk:
				assistants = append(assistants, ev.Text)
			}
		default:
			goto done
		}
	}
done:
	if len(notices) != 2 {
		t.Fatalf("notices = %v", notices)
	}
	if len(assistants) != 1 || assistants[0] != "looks fine" {
		t.Fatalf("assistants = %v", assistants)
	}
}

func TestReviewFallbackSkippedWhenAssistantTextArrived(t *testing.T) {
	s := seededCollabSession(t)
	s.reviewing = true
	s.noteReviewAssistant()
	s.handleReviewItem("exitedReviewMode", []byte(`{"type":"exitedReviewMode","review":"duplicate"}`), true)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeAssistantChunk {
				t.Fatalf("fallback must not append: %q", ev.Text)
			}
		default:
			return
		}
	}
}

func TestReviewEmptyTargetRejectedBeforeRPC(t *testing.T) {
	s, engineR, _ := seedReviewSession(t)
	if err := s.StartReview(context.Background(), provider.ReviewTarget{Kind: provider.ReviewCustom}); !errors.Is(err, provider.ErrReviewInvalid) {
		t.Fatalf("err = %v", err)
	}
	_ = engineR
}
