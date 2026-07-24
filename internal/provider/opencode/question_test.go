package opencode

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestQuestionAskedEmitsRequest(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("question.asked", json.RawMessage(`{
		"id":"q1","sessionID":"ses_test",
		"questions":[{
			"question":"Which packages?","header":"Scope",
			"multiple":true,"custom":false,
			"options":[{"label":"core","description":"Core pkg"},{"label":"cli","description":"CLI"}]
		}]
	}`))
	var found bool
	for _, ev := range h.events {
		if ev.Type == event.TypeQuestion && ev.QuestionID == "q1" {
			found = true
			if len(ev.Questions) != 1 || ev.Questions[0].Header != "Scope" {
				t.Fatalf("questions=%+v", ev.Questions)
			}
			if len(ev.Questions[0].Options) != 2 || ev.Questions[0].Options[0].OptionID != "core" {
				t.Fatalf("options=%+v", ev.Questions[0].Options)
			}
			if !ev.Questions[0].Multiple {
				t.Fatal("want multiple")
			}
		}
	}
	if !found {
		t.Fatal("expected question_request")
	}
}

func TestQuestionRejectedEmitsResolved(t *testing.T) {
	h := &qPendingHost{
		captureHost: captureHost{},
		pending:     map[string]struct{}{"q1": {}},
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("question.rejected", json.RawMessage(`{"requestID":"q1","sessionID":"ses_test"}`))
	var found bool
	for _, ev := range h.events {
		if ev.Type == event.TypeQuestionResolved && ev.QuestionID == "q1" &&
			ev.Status == event.PermissionStatusCancelled {
			found = true
		}
	}
	if !found {
		t.Fatal("expected question_resolved cancelled")
	}
}

func TestRespondQuestionReplyAndReject(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(200)
		_, _ = w.Write([]byte("true"))
	}))
	defer ts.Close()

	h := &captureHost{}
	h.api = func(ctx context.Context, method, path string, body, out any) error {
		var rd io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = strings.NewReader(string(raw))
		}
		req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, rd)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return errHTTP(res.StatusCode, "fail")
		}
		return nil
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	if err := s.RespondQuestion(context.Background(), "q9", [][]string{{"core", "cli"}}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RespondQuestion(context.Background(), "q9", nil, true); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) < 2 {
		t.Fatalf("paths=%v", paths)
	}
	if !strings.Contains(paths[0], "/question/q9/reply") {
		t.Fatalf("reply path=%q", paths[0])
	}
	if !strings.Contains(bodies[0], `"core"`) {
		t.Fatalf("reply body=%q", bodies[0])
	}
	if !strings.Contains(paths[1], "/question/q9/reject") {
		t.Fatalf("reject path=%q", paths[1])
	}
}

// qPendingHost tracks question pending for resolved-event tests.
type qPendingHost struct {
	captureHost
	pending map[string]struct{}
}

func (h *qPendingHost) TrackQuestion(id string) {
	if h.pending == nil {
		h.pending = make(map[string]struct{})
	}
	h.pending[id] = struct{}{}
}

func (h *qPendingHost) TakeQuestionPending(id string) bool {
	_, ok := h.pending[id]
	delete(h.pending, id)
	return ok
}
