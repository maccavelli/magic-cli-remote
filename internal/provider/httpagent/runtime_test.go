package httpagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestHTTPEnginesImplementRuntimeSession is acceptance 2 and 3's precondition.
func TestHTTPEnginesImplementRuntimeSession(t *testing.T) {
	var s any = &session{}
	if _, ok := s.(provider.RuntimeSession); !ok {
		t.Fatal("the httpagent session does not implement provider.RuntimeSession; " +
			"/status and /usage would stay dead on kilo")
	}
}

// TestTitleIDIsSafeOnAnEmptyID guards the display helper against a dialect that
// reports no id.
func TestTitleIDIsSafeOnAnEmptyID(t *testing.T) {
	if got := titleID(""); got != "Engine" {
		t.Errorf("titleID(\"\") = %q", got)
	}
	if got := titleID(provider.IDKilo); got != "Kilo" {
		t.Errorf("titleID(kilo) = %q", got)
	}
}

// stubDialect is the minimum Dialect. It deliberately does NOT implement
// RuntimeDialect, which is the case Q5 exercises.
type stubDialect struct{ id provider.ID }

func (d stubDialect) ID() provider.ID                { return d.id }
func (d stubDialect) DefaultBin() string             { return "stub" }
func (d stubDialect) ServeArgs(int) []string         { return nil }
func (d stubDialect) HealthPath() string             { return "/health" }
func (d stubDialect) EventsPath() string             { return "/event" }
func (d stubDialect) AfterBoot(context.Context, API) {}
func (d stubDialect) DecodeFrame([]byte) (string, json.RawMessage, string, bool) {
	return "", nil, "", false
}
func (d stubDialect) NewSession(Host) DialectSession { return nil }

// runtimeDialect wraps stubDialect with a status the test controls.
type runtimeDialect struct {
	stubDialect
	res provider.Result
	err error
}

func (d runtimeDialect) RuntimeStatus(context.Context, API) (provider.Result, error) {
	return d.res, d.err
}

// TestUsagePassesTheSessionObjectThrough is 0208-PLAN P5 step 3 for kilo
// `/usage` (Deviation 5): kilo 7.8.18's answer to GET /session/{id}, byte for
// byte, named by the request that got it.
//
// One source of truth, as before: kilo publishes the same totals a second time
// under /session/{id}/model-usage, and reading both is how two numbers for one
// session start to disagree. The capture is kilo's, so it lives with kilo's.
func TestUsagePassesTheSessionObjectThrough(t *testing.T) {
	captured, err := os.ReadFile("../kilo/testdata/wire/7.8.18/session.json")
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	captured = bytes.TrimSpace(captured)
	var calls []string
	api := func(_ context.Context, method, path string, _, out any) error {
		calls = append(calls, method+" "+path)
		return json.Unmarshal(captured, out)
	}
	res := runtimeUsage(context.Background(), api, "ses_e58c45d75ffefGFzcSFuzhBlJW")
	if len(calls) != 1 || calls[0] != "GET /session/ses_e58c45d75ffefGFzcSFuzhBlJW" {
		t.Fatalf("engine calls = %v, want exactly [GET /session/ses_e58c45d75ffefGFzcSFuzhBlJW]. The "+
			"session object already carries the totals; model-usage is a second source for the same figure", calls)
	}
	if !bytes.Equal(res.Body, captured) {
		t.Errorf("body = %s\nwant kilo's answer unchanged: %s", res.Body, captured)
	}
	if res.Method != "GET /session/ses_e58c45d75ffefGFzcSFuzhBlJW" || res.Text != "" {
		t.Errorf("method = %q, text = %q; want the request that answered and no text", res.Method, res.Text)
	}
}

// TestUsageWithoutAnEngineAnswerIsText keeps /usage saying something when there
// is no engine answer to pass through: no engine session yet, or a failed read.
func TestUsageWithoutAnEngineAnswerIsText(t *testing.T) {
	api := func(context.Context, string, string, any, any) error {
		t.Fatal("a session with no engine id must not call the engine")
		return nil
	}
	if res := runtimeUsage(context.Background(), api, ""); len(res.Body) != 0 ||
		!strings.Contains(res.Text, "no engine session id") {
		t.Errorf("usage = %+v, want a line saying the session has no engine id, and no body", res)
	}

	failing := func(context.Context, string, string, any, any) error { return errors.New("engine refused: 503") }
	if res := runtimeUsage(context.Background(), failing, "ses_abc"); len(res.Body) != 0 ||
		!strings.Contains(res.Text, "unavailable") || !strings.Contains(res.Text, "503") {
		t.Errorf("usage = %+v, want the failure named, and no body", res)
	}
}

// TestStatusWithoutARuntimeDialectStillAnswers is Q5.
//
// Same claim as Phase 10's P4 for grok: cmdRuntime swallows a returned error,
// so a dialect with nothing to report must still produce a line.
func TestStatusWithoutARuntimeDialectStillAnswers(t *testing.T) {
	api := func(context.Context, string, string, any, any) error {
		t.Fatal("a dialect with no RuntimeDialect must not call the engine")
		return nil
	}
	got := runtimeStatus(context.Background(), stubDialect{id: provider.IDOpencode}, api, "Opencode")
	if !strings.Contains(got.Text, "no plan usage") || len(got.Body) != 0 {
		t.Errorf("status = %+v, want a line saying the engine publishes none, and no body", got)
	}
	if strings.TrimSpace(got.Text) == "" {
		t.Error("status was empty; /status would show nothing at all")
	}
}

// TestStatusReportsADialectFailureAsText keeps a broken probe visible, and
// passes the dialect's answer through unchanged (MADR 0208 D12).
func TestStatusReportsADialectFailureAsText(t *testing.T) {
	d := runtimeDialect{stubDialect: stubDialect{id: provider.IDKilo},
		err: errors.New("engine refused: 503")}
	got := runtimeStatus(context.Background(), d, nil, "Kilo")
	if !strings.Contains(got.Text, "status unavailable") || !strings.Contains(got.Text, "503") {
		t.Errorf("status = %+v, want the failure named", got)
	}

	// An empty answer is treated as "nothing to say", not as an answer.
	blank := runtimeDialect{stubDialect: stubDialect{id: provider.IDKilo}, res: provider.Result{Text: "   "}}
	if got := runtimeStatus(context.Background(), blank, nil, "Kilo"); !strings.Contains(got.Text, "no plan usage") {
		t.Errorf("status = %+v, want the fallback line for an empty dialect answer", got)
	}

	answer := provider.Result{Method: "GET /plan", Body: json.RawMessage(`{"items":[]}`)}
	ok := runtimeDialect{stubDialect: stubDialect{id: provider.IDKilo}, res: answer}
	if got := runtimeStatus(context.Background(), ok, nil, "Kilo"); got.Method != answer.Method ||
		string(got.Body) != string(answer.Body) || got.Text != "" {
		t.Errorf("status = %+v, want the dialect's answer unchanged", got)
	}
}
