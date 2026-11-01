package session_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// runtimeProvider is a recordingProvider whose sessions answer /status and
// /usage with fixed results (0208-PLAN P5).
type runtimeProvider struct {
	*recordingProvider
	status, usage provider.Result
}

func (p *runtimeProvider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	s, err := p.recordingProvider.Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &runtimeSession{recordingSession: s.(*recordingSession), p: p}, nil
}

func (p *runtimeProvider) CommandTable() command.Table {
	return command.Table{
		"status": {Kind: command.KindOp, Op: command.OpStatus},
		"usage":  {Kind: command.KindOp, Op: command.OpUsage},
	}
}

type runtimeSession struct {
	*recordingSession
	p *runtimeProvider
}

func (s *runtimeSession) RuntimeStatus(context.Context) (provider.Result, error) {
	return s.p.status, nil
}

func (s *runtimeSession) RuntimeUsage(context.Context) (provider.Result, error) {
	return s.p.usage, nil
}

func newRuntimeManager(t *testing.T, p *runtimeProvider) (*session.Manager, *eventSink, session.Meta) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "rec", provider.StartOptions{
		Name:           "s",
		LocalSessionID: "sess-1",
	}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sink.waitForAdvertised(t, "usage", true)
	return mgr, sink, meta
}

// grokUsageAnswer is grok 1.0.70's captured `_x.ai/session/usage` result, as
// the engine sent it.
func grokUsageAnswer(t *testing.T) string {
	t.Helper()
	f, err := os.Open("../provider/grok/testdata/wire/1.0.70/frames.jsonl")
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var frame struct {
			Result json.RawMessage `json:"result"`
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &frame) == nil && json.Unmarshal(frame.Result, &fields) == nil {
			if _, ok := fields["usage"]; ok {
				return string(frame.Result)
			}
		}
	}
	t.Fatal("the 1.0.70 capture has no _x.ai/session/usage answer")
	return ""
}

// TestUsageSendsTheEngineAnswerUnchanged is 0208-PLAN P5 step 3 at the
// daemon: /usage reaches the phone as an engine_result whose text is the
// captured engine body, byte for byte, with no notice written beside it.
func TestUsageSendsTheEngineAnswerUnchanged(t *testing.T) {
	body := grokUsageAnswer(t)
	p := &runtimeProvider{
		recordingProvider: &recordingProvider{},
		usage:             provider.Result{Method: "_x.ai/session/usage", Body: json.RawMessage(body)},
	}
	mgr, sink, meta := newRuntimeManager(t, p)

	if err := mgr.Prompt(context.Background(), meta.ID, "/usage", nil, "dev-a"); err != nil {
		t.Fatalf("/usage: %v", err)
	}
	sink.waitForEvent(t, event.TypeEngineResult)
	i := slices.IndexFunc(sink.snapshot(), func(ev event.Event) bool {
		return ev.Type == event.TypeEngineResult
	})
	got := sink.snapshot()[i]
	if got.Text != body {
		t.Errorf("engine_result text = %s\nwant the engine body unchanged: %s", got.Text, body)
	}
	if got.ResultMethod != "_x.ai/session/usage" || got.SessionID != meta.ID {
		t.Errorf("method = %q, session = %q", got.ResultMethod, got.SessionID)
	}
	if n := sink.notices(); len(n) != 0 {
		t.Errorf("notices %q were written beside the engine's answer", n)
	}
}

// TestStatusWithoutAnEngineAnswerIsANotice: a result with no body is Magic's
// own line, and stays the notice it was.
func TestStatusWithoutAnEngineAnswerIsANotice(t *testing.T) {
	p := &runtimeProvider{
		recordingProvider: &recordingProvider{},
		status:            provider.Result{Text: "Grok billing is unavailable: this grok build does not have the method."},
	}
	mgr, sink, meta := newRuntimeManager(t, p)

	if err := mgr.Prompt(context.Background(), meta.ID, "/status", nil, "dev-a"); err != nil {
		t.Fatalf("/status: %v", err)
	}
	waitFor(t, "the unavailable notice", func() bool {
		return sink.hasNoticeContaining("Grok billing is unavailable")
	})
	if slices.ContainsFunc(sink.snapshot(), func(ev event.Event) bool {
		return ev.Type == event.TypeEngineResult
	}) {
		t.Error("an answer with no engine body was sent as an engine_result")
	}
}
