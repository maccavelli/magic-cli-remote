package opencode_test

// The OpenCode ACP Spec on G1's recording (0214 PLAN P4 step 6): the handshake
// and session/new answers opencode 2.2.3 sent, its child updates, and its
// retry markers (transcribed from source, as acp_frames_test.go explains).

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// g1Result returns the result of G1's line n (1-based).
func g1Result(t *testing.T, n int) string {
	t.Helper()
	i := 0
	var out string
	g1Lines(t, func(m g1) bool {
		i++
		if i == n {
			out = string(m.Result)
		}
		return false
	})
	if out == "" {
		t.Fatalf("G1 line %d has no result", n)
	}
	return out
}

// G1's answers: initialize (line 1), session/new (line 2), and
// session/set_config_option selecting the free model (line 4).
const (
	g1Initialize   = 1
	g1SessionNew   = 2
	g1SetConfigOpt = 4
)

// g1SessionID is the session id G1's session/new answered with. The fixture
// agent's frames name "agent-1", so startACP answers with that instead.
const g1SessionID = "ses_e66d650b4ffeqRlyXifkffmAqI"

// through sends a mode update after what the test sent and returns every event
// before it: the agent's lines before it have then been handled.
func through(t *testing.T, s provider.Session, a *acptest.Agent) []event.Event {
	t.Helper()
	a.Send(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":{"sessionUpdate":"current_mode_update","currentModeId":"plan"}}}`)
	var out []event.Event
	deadline := time.After(acptest.Budget)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeMode && ev.CurrentModeID == "plan" {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatal("the barrier's mode update did not arrive")
			return nil
		}
	}
}

// startACP starts opencode's ACP Spec on a fixture agent answering as G1's
// opencode did.
func startACP(t *testing.T, opts provider.StartOptions) (provider.Session, *acptest.Agent) {
	t.Helper()
	return acptest.Start(t, opencode.NewACP(acpagent.Config{}), acptest.Options{
		SessionID: "agent-1",
		Start:     opts,
		Replies: map[string]string{
			"initialize":                g1Result(t, g1Initialize),
			"session/new":               strings.Replace(g1Result(t, g1SessionNew), g1SessionID, "agent-1", 1),
			"session/set_config_option": g1Result(t, g1SetConfigOpt),
		},
	})
}

// TestACPSpecAdvertisesD1sCapabilities: initialize advertises 0214 MADR D1's
// client capabilities, the child-session flag among them, and G1's engine,
// 2.2.3, is accepted.
func TestACPSpecAdvertisesD1sCapabilities(t *testing.T) {
	_, a := startACP(t, provider.StartOptions{})
	caps, _ := a.ParamsFor("initialize")["clientCapabilities"].(map[string]any)
	b, _ := json.Marshal(caps)
	for _, want := range []string{`"readTextFile":true`, `"writeTextFile":true`, `"terminal":true`,
		`"elicitation":{"form":{}}`, `"compaction":{}`, `"_meta":{"opencode/child-session-updates":true}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("clientCapabilities %s lack %s", b, want)
		}
	}
	if problems := acpagent.CheckClientCapabilities(opencode.NewACP(acpagent.Config{}).ClientCapabilities()); len(problems) != 0 {
		t.Errorf("advertised without behaviour: %v", problems)
	}
}

// startOnInitialize runs the Spec's Start against an agent whose initialize
// answer is result, and returns Start's error.
func startOnInitialize(t *testing.T, result string) error {
	t.Helper()
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	t.Cleanup(func() {
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		_ = agentReads.Close()
	})
	go func() {
		sc := bufio.NewScanner(agentReads)
		for sc.Scan() {
			var m struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil || len(m.ID) == 0 {
				continue
			}
			answer := result
			if m.Method == "session/new" {
				answer = `{"sessionId":"agent-1"}`
			}
			_, _ = io.WriteString(agentWrites, `{"jsonrpc":"2.0","id":`+string(m.ID)+`,"result":`+answer+"}\n")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := opencode.NewACP(acpagent.Config{}).StartOn(ctx, provider.StartOptions{CWD: t.TempDir()}, clientReads, clientWrites)
	if err == nil {
		_ = s.Close(context.Background())
	}
	return err
}

// TestACPSpecRefusesA1xEngine pins 0214 MADR D3 on G1's initialize answer: the
// same answer from a 1.x engine, or with no version, is refused with the
// version found and the install link; 2.2.3 is accepted.
func TestACPSpecRefusesA1xEngine(t *testing.T) {
	init := g1Result(t, g1Initialize)
	if !strings.Contains(init, `"version":"2.2.3"`) {
		t.Fatalf("G1's initialize names no 2.2.3: %s", init)
	}
	if err := startOnInitialize(t, init); err != nil {
		t.Fatalf("G1's own engine was refused: %v", err)
	}
	for _, c := range []struct{ name, result, version string }{
		{"1.x", strings.Replace(init, `"version":"2.2.3"`, `"version":"1.18.26"`, 1), "1.18.26"},
		{"no version", strings.Replace(init, `,"agentInfo":{"name":"OpenCode","version":"2.2.3"}`, ``, 1), "no version"},
	} {
		err := startOnInitialize(t, c.result)
		if err == nil {
			t.Errorf("%s: Start accepted the engine", c.name)
			continue
		}
		for _, want := range []string{c.version, opencode.ACPMinVersion, "https://opencode.ai/v2/install"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not name %q", c.name, err, want)
			}
		}
	}
}

// TestACPChildUpdatesFillTheSubagentPanel pins 0214 MADR D5 on G1's seven
// child_update frames: the child appears in the subagent panel under its title
// and ends completed, and none of its own output reaches the parent's
// transcript.
func TestACPChildUpdatesFillTheSubagentPanel(t *testing.T) {
	s, a := startACP(t, provider.StartOptions{})
	frames := g1Lines(t, func(m g1) bool { return m.Method == childUpdateMethod })
	for _, f := range frames {
		a.Send(f)
	}
	var last []event.SubagentInfo
	for _, ev := range through(t, s, a) {
		switch ev.Type {
		case event.TypeSubagents:
			last = ev.Subagents
		case event.TypeAssistantChunk, event.TypeToolCall, event.TypeToolUpdate:
			t.Errorf("the child's output reached the parent's transcript: %+v", ev)
		}
	}
	if len(last) != 1 {
		t.Fatalf("subagent panel = %+v, want one child", last)
	}
	if c := last[0]; c.ID != "ses_e66d632ceffe6uZEzR7BHXFDuM" || c.Task != "Read README first line" ||
		c.Status != event.SubagentStatusCompleted {
		t.Errorf("child = %+v, want G1's child, its title, completed", c)
	}
}

// TestACPChildFailedOrInterruptedIsFailed: the two other terminal statuses in
// opencode's source, on G1's last frame.
func TestACPChildFailedOrInterruptedIsFailed(t *testing.T) {
	frames := g1Lines(t, func(m g1) bool { return m.Method == childUpdateMethod })
	last := frames[len(frames)-1]
	if !strings.Contains(last, `"status":"completed"`) {
		t.Fatalf("G1's last child frame is not its completion: %s", last)
	}
	for _, status := range []string{"failed", "interrupted"} {
		s, a := startACP(t, provider.StartOptions{})
		a.Send(strings.Replace(last, `"status":"completed"`, `"status":"`+status+`"`, 1))
		var got []event.SubagentInfo
		for _, ev := range through(t, s, a) {
			if ev.Type == event.TypeSubagents {
				got = ev.Subagents
			}
		}
		if len(got) != 1 || got[0].Status != event.SubagentStatusFailed {
			t.Errorf("%s: subagent panel = %+v, want the child failed", status, got)
		}
	}
}

// retryAt is a retry marker for a retry due in wait, with message.
func retryAt(wait time.Duration, message string) string {
	return `{"attempt":3,"nextRetryAt":"` + time.Now().Add(wait).UTC().Format(time.RFC3339Nano) +
		`","error":{"type":"api","message":"` + message + `","status":429}}`
}

// infoUpdate is a session_info_update carrying value under the retry key.
func infoUpdate(value string) string {
	return `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":` +
		`{"sessionUpdate":"session_info_update","_meta":{"` + retryKey + `":` + value + `}}}}`
}

// TestACPRetryShortIsANotice pins 0214 MADR D6: a short retry is a notice, and
// a cleared marker says nothing.
func TestACPRetryShortIsANotice(t *testing.T) {
	s, a := startACP(t, provider.StartOptions{})
	a.Send(infoUpdate(retryAt(5*time.Second, "Rate limited")))
	a.Send(infoUpdate(`null`))
	var notices []string
	for _, ev := range through(t, s, a) {
		if ev.Type == event.TypeNotice {
			notices = append(notices, ev.Text)
		}
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "Rate limited") || !strings.Contains(notices[0], "attempt 3") {
		t.Errorf("notices = %q, want one naming the retry", notices)
	}
}

// TestACPRetryLongEndsTheTurnWithTheLimitCard pins 0214 MADR D6: a long
// back-off ends the running turn with the limit card and opencode's retry time.
func TestACPRetryLongEndsTheTurnWithTheLimitCard(t *testing.T) {
	s, a := startACP(t, provider.StartOptions{})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	prompt := a.Await("session/prompt")
	due := time.Now().Add(10 * time.Minute)
	a.Send(infoUpdate(retryAt(10*time.Minute, "Rate limited")))
	// The limit cancels the turn; the agent then answers its prompt.
	for deadline := time.Now().Add(acptest.Budget); !a.Sent("session/cancel"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the long back-off did not cancel the turn")
		}
	}
	a.Respond(prompt, map[string]any{"stopReason": "cancelled"})
	limit := acptest.Next(t, s, event.TypeError)
	if limit.RetryAt.IsZero() || limit.RetryAt.Sub(due).Abs() > 5*time.Second {
		t.Errorf("limit card retries at %v, want about %v", limit.RetryAt, due)
	}
}

// TestACPRetryOnAPromptResultSaysNothingShort: a short retry pending when the
// turn was interrupted is not news once the turn is over.
func TestACPRetryOnAPromptResultSaysNothingShort(t *testing.T) {
	s, a := startACP(t, provider.StartOptions{})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(`{"stopReason":"cancelled","_meta":{"`+retryKey+`":`+retryAt(5*time.Second, "Rate limited")+`}}`), &result); err != nil {
		t.Fatal(err)
	}
	a.Respond(a.Await("session/prompt"), result)
	// Every event from the prompt on: to the turn's end, then anything after.
	var evs []event.Event
	for deadline := time.After(acptest.Budget); len(evs) == 0 || evs[len(evs)-1].Type != event.TypeTurnComplete; {
		select {
		case ev := <-s.Events():
			evs = append(evs, ev)
		case <-deadline:
			t.Fatal("the turn did not end")
		}
	}
	evs = append(evs, through(t, s, a)...)
	for _, ev := range evs {
		if ev.Type == event.TypeNotice && strings.Contains(ev.Text, "Retrying") {
			t.Errorf("a retry notice after the turn ended: %q", ev.Text)
		}
	}
}

// TestACPAutoAllowsOnceByKind pins 0214 MADR D9 on opencode's own permission
// options (packages/cli/src/acp/permission.ts:39-43 at b44b7164ba): in auto,
// the choice is by kind, allow_once first.
func TestACPAutoAllowsOnceByKind(t *testing.T) {
	s, a := startACP(t, provider.StartOptions{})
	ms, ok := s.(provider.ModeSession)
	if !ok {
		t.Fatalf("%T is not a ModeSession", s)
	}
	if err := ms.SetMode(context.Background(), "auto"); err != nil {
		t.Fatalf("arm auto: %v", err)
	}
	id := a.Call("session/request_permission", map[string]any{
		"sessionId": "agent-1",
		"toolCall":  map[string]any{"toolCallId": "call-1", "title": "bash", "kind": "execute"},
		"options": []map[string]any{
			{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
			{"optionId": "always", "kind": "allow_always", "name": "Always allow"},
			{"optionId": "reject", "kind": "reject_once", "name": "Reject"},
		},
	})
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(a.AnswerTo(id).Result, &res); err != nil {
		t.Fatal(err)
	}
	if res.Outcome.Outcome != "selected" || res.Outcome.OptionID != "once" {
		t.Errorf("auto answered %+v, want once selected", res.Outcome)
	}
}

// TestACPPlanSetsTheModeOption: G1's session/new carries a mode option and no
// ACP modes, so /plan selects the option, and the answer confirms it (P4
// Deviation 2).
func TestACPPlanSetsTheModeOption(t *testing.T) {
	s, a := startACP(t, provider.StartOptions{})
	a.SetReply("session/set_config_option", strings.Replace(g1Result(t, g1SetConfigOpt),
		`"currentValue":"build"`, `"currentValue":"plan"`, 1))
	if err := s.(provider.ModeSession).SetMode(context.Background(), "plan"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if p := a.ParamsFor("session/set_config_option"); p["configId"] != "mode" || p["value"] != "plan" {
		t.Errorf("set_config_option params = %v, want mode=plan", p)
	}
	if a.Sent("session/set_mode") {
		t.Error("session/set_mode was sent; opencode confirms nothing after it")
	}
	if ev := acptest.Next(t, s, event.TypeMode); ev.CurrentModeID != "plan" {
		t.Errorf("reported mode %q, want plan", ev.CurrentModeID)
	}
}

// TestACPStartAppliesTheRequestedModel: G1's session/new answered with
// another current model, and G1 then selected the free model through the
// option. Magic now does that from the session's requested model (P4
// Deviation 1).
func TestACPStartAppliesTheRequestedModel(t *testing.T) {
	want := "opencode/deepseek-v4.1-flash-free"
	if strings.Contains(g1Result(t, g1SessionNew), `"currentValue":"`+want+`"`) {
		t.Fatal("G1's session/new already has the free model current; the test proves nothing")
	}
	s, a := startACP(t, provider.StartOptions{Model: want})
	if p := a.ParamsFor("session/set_config_option"); p["configId"] != "model" || p["value"] != want {
		t.Errorf("set_config_option params = %v, want model=%s", p, want)
	}
	cm, ok := s.(interface{ CurrentModel() string })
	if !ok || cm.CurrentModel() != want {
		t.Errorf("session model is not %s", want)
	}
}

// TestACPCommandTableOnG1sCommands pins step 5 on what G1's opencode
// advertised: its own compact, goal and review forward, and the commands
// that need the control plane say so.
func TestACPCommandTableOnG1sCommands(t *testing.T) {
	var advertised []string
	for _, f := range g1Lines(t, func(m g1) bool { return strings.Contains(string(m.Params), "available_commands_update") }) {
		var m struct {
			Params struct {
				Update struct {
					AvailableCommands []struct {
						Name string `json:"name"`
					} `json:"availableCommands"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(f), &m); err != nil {
			t.Fatal(err)
		}
		for _, c := range m.Params.Update.AvailableCommands {
			advertised = append(advertised, c.Name)
		}
	}
	tbl := opencode.NewACP(acpagent.Config{}).CommandTable()
	state := command.SessionState{AgentCommands: advertised}
	for _, name := range []string{"compact", "goal", "review"} {
		res, _ := command.Resolve(name, tbl, state)
		if !res.Available || res.Mapping.Kind != command.KindNative || res.Mapping.Native != name {
			t.Errorf("/%s resolves to %+v, want opencode's own command", name, res)
		}
	}
	// Served by the control plane since P6d and P6e (0214 PLAN P6e Deviation
	// 1, "Fold into P6e"): each maps to its operation, which the session
	// declares.
	state.Ops = map[command.Op]bool{command.OpDiff: true, command.OpUndo: true, command.OpRedo: true}
	for name, op := range map[string]command.Op{"diff": command.OpDiff, "undo": command.OpUndo, "redo": command.OpRedo} {
		res, _ := command.Resolve(name, tbl, state)
		if !res.Available || res.Mapping.Kind != command.KindOp || res.Mapping.Op != op {
			t.Errorf("/%s resolves to %+v, want the %s operation", name, res, op)
		}
	}
}
