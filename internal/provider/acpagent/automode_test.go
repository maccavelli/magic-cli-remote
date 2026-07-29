package acpagent

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Grok is the reason this exists: its own auto-approve is a flag on the
// process (`--permission-mode auto`), so the only way to offer it per session
// is for the daemon to advertise the mode itself and answer the permission
// requests (MADR 0049). The load-bearing properties are that the synthetic id
// never reaches the agent, and that arming it actually stops the prompts —
// advertising auto while still asking would be worse than not offering it.

func autoModeSession(t *testing.T) *session {
	t.Helper()
	s := modeSession(t)
	s.synthesizeAuto = true
	return s
}

// fakeAgent answers ACP requests on a pipe pair so SetMode can reach a
// transport. It records every session/set_mode it is asked for.
type fakeAgent struct {
	mu       sync.Mutex
	setModes []string
	failSet  bool

	toAgent   *io.PipeWriter
	fromAgent *io.PipeReader
	done      chan struct{}
}

func (f *fakeAgent) modes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.setModes...)
}

// startFakeAgent wires a session's conn to an in-memory agent.
func startFakeAgent(t *testing.T, s *session, failSet bool) *fakeAgent {
	t.Helper()
	agentIn, clientOut := io.Pipe() // client → agent
	clientIn, agentOut := io.Pipe() // agent → client
	f := &fakeAgent{
		failSet:   failSet,
		toAgent:   clientOut,
		fromAgent: clientIn,
		done:      make(chan struct{}),
	}

	go func() {
		defer close(f.done)
		sc := bufio.NewScanner(agentIn)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		// Scan ends when the test closes the pipe; a read error there is the
		// teardown, not a failure worth surfacing.
		defer func() { _ = sc.Err() }()
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					ModeID string `json:"modeId"`
				} `json:"params"`
			}
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				continue
			}
			if req.Method == "session/set_mode" {
				f.mu.Lock()
				f.setModes = append(f.setModes, req.Params.ModeID)
				fail := f.failSet
				f.mu.Unlock()
				if fail {
					writeLine(agentOut, map[string]any{
						"jsonrpc": "2.0", "id": req.ID,
						"error": map[string]any{"code": -32600, "message": "refused"},
					})
					continue
				}
			}
			if len(req.ID) > 0 && string(req.ID) != "null" {
				writeLine(agentOut, map[string]any{
					"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{},
				})
			}
		}
	}()

	s.conn = acp.NewClientSideConnection(s, clientOut, clientIn)
	t.Cleanup(func() {
		_ = clientOut.Close()
		_ = agentOut.Close()
	})
	return f
}

func writeLine(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(append(b, '\n'))
}

func modeIDs(modes []event.SessionMode) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, m.ID)
	}
	return out
}

func TestAutoModeAdvertisedWhenOptedIn(t *testing.T) {
	s := autoModeSession(t)
	s.emitModesOrStatic(nil)

	ev := recvEvent(t, s.events)
	got := modeIDs(ev.Modes)
	if len(got) != 3 || got[0] != "default" || got[2] != autoModeID {
		t.Fatalf("modes = %v, want [default plan auto]", got)
	}
	// Last, so the normal mode stays the menu head and an older client that
	// still falls back to the first entry lands somewhere safe.
	if !ev.Modes[2].Dangerous {
		t.Error("auto must be flagged dangerous or the phone will not gate arming")
	}
	if ev.CurrentModeID != "default" {
		t.Fatalf("current = %q, want default", ev.CurrentModeID)
	}
}

func TestAutoModeAbsentWhenNotOptedIn(t *testing.T) {
	// Guards goose and every other ACP agent that did not ask for this.
	s := modeSession(t)
	s.emitModesOrStatic(nil)

	ev := recvEvent(t, s.events)
	if got := modeIDs(ev.Modes); len(got) != 2 {
		t.Fatalf("modes = %v, want the static list untouched", got)
	}
}

func TestAutoModeNeverShadowsANativeOne(t *testing.T) {
	s := autoModeSession(t)
	s.emitModesOrStatic(&acp.SessionModeState{
		CurrentModeId: "auto",
		AvailableModes: []acp.SessionMode{
			{Id: "auto", Name: "Auto"},
			{Id: "plan", Name: "Plan"},
		},
	})

	ev := recvEvent(t, s.events)
	got := modeIDs(ev.Modes)
	if len(got) != 2 {
		t.Fatalf("modes = %v, want the agent's own list with one auto", got)
	}
}

// The synthetic id is ours, not the agent's: grok has no `auto` ACP mode and
// would reject it. Arming must put the agent into its normal mode instead.
func TestArmingAutoSendsNormalModeNotAuto(t *testing.T) {
	s := autoModeSession(t)
	s.syntheticModes = true
	f := startFakeAgent(t, s, false)

	if err := s.SetMode(t.Context(), autoModeID); err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}
	got := f.modes()
	if len(got) != 1 || got[0] != "default" {
		t.Fatalf("agent saw set_mode %v, want exactly [default]", got)
	}
	for _, m := range got {
		if m == autoModeID {
			t.Fatal("the synthetic id must never reach the agent")
		}
	}
}

// The whole point: armed means the daemon answers, so nothing is asked of the
// user. Advertising auto while still emitting permission requests would be a
// mode chip that lies.
func TestArmedAutoAnswersPermissionsWithoutPrompting(t *testing.T) {
	s := autoModeSession(t)
	s.mu.Lock()
	s.autoApprove = true
	s.mu.Unlock()

	resp, err := s.RequestPermission(t.Context(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "deny-1", Name: "Deny", Kind: "reject_once"},
			{OptionId: "allow-1", Name: "Allow", Kind: "allow_once"},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if resp.Outcome.Selected == nil ||
		string(resp.Outcome.Selected.OptionId) != "allow-1" {
		t.Fatalf("outcome = %+v, want the allow option selected", resp.Outcome)
	}
	// Armed auto must not ask the user — but it must still leave an audit
	// trail. Before MADR 0051 the ACP path emitted nothing at all, so the user
	// had no way to see what ran on their behalf.
	var audit *event.Event
	for drained := false; !drained; {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypePermission:
				t.Fatalf("armed auto must not ask the user; got %s", ev.Type)
			case event.TypeApprovalSummary:
				e := ev
				audit = &e
			default:
				t.Fatalf("unexpected event on the auto path: %s", ev.Type)
			}
		default:
			drained = true
		}
	}
	if audit == nil {
		t.Fatal("no approval_summary: an auto-approved permission left no audit trail")
	}
	if audit.ApprovalGroupID != approvalGroupID {
		t.Errorf("approval_group_id = %q, want %q", audit.ApprovalGroupID, approvalGroupID)
	}
	if len(audit.Approvals) != 1 {
		t.Fatalf("approvals = %v, want exactly one", audit.Approvals)
	}
}

func TestSelectingARealModeDisarmsAuto(t *testing.T) {
	s := autoModeSession(t)
	s.syntheticModes = true
	startFakeAgent(t, s, false)

	if err := s.SetMode(t.Context(), autoModeID); err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}
	if err := s.SetMode(t.Context(), "plan"); err != nil {
		t.Fatalf("SetMode(plan): %v", err)
	}
	s.mu.Lock()
	armed := s.autoApprove
	s.mu.Unlock()
	if armed {
		t.Fatal("selecting a real mode must disarm auto")
	}
}

// A switch the agent refused must not leave the session claiming auto while
// the agent is still in whatever mode it was in.
func TestFailedSwitchDoesNotArmAuto(t *testing.T) {
	s := autoModeSession(t)
	s.syntheticModes = true
	startFakeAgent(t, s, true)

	if err := s.SetMode(t.Context(), autoModeID); err == nil {
		t.Fatal("want the agent's refusal surfaced")
	}
	s.mu.Lock()
	armed := s.autoApprove
	s.mu.Unlock()
	if armed {
		t.Fatal("a refused switch must leave auto disarmed")
	}
}

func TestUnknownModeStillRejectedWithAutoEnabled(t *testing.T) {
	s := autoModeSession(t)
	s.syntheticModes = true

	if err := s.SetMode(t.Context(), "bogus-xyz"); err == nil {
		t.Fatal("want an error for an id outside our list")
	}
	s.mu.Lock()
	armed := s.autoApprove
	s.mu.Unlock()
	if armed {
		t.Fatal("a rejected id must not arm auto")
	}
}

func TestArmedAutoIsReportedAsTheCurrentMode(t *testing.T) {
	s := autoModeSession(t)
	s.mu.Lock()
	s.autoApprove = true
	s.mu.Unlock()

	s.emitModesOrStatic(nil)
	ev := recvEvent(t, s.events)
	if ev.CurrentModeID != autoModeID {
		t.Fatalf("current = %q, want auto while armed", ev.CurrentModeID)
	}

	s.mu.Lock()
	s.autoApprove = false
	s.mu.Unlock()
	s.emitModesOrStatic(nil)
	ev = recvEvent(t, s.events)
	if ev.CurrentModeID != "default" {
		t.Fatalf("current = %q, want the real mode once disarmed", ev.CurrentModeID)
	}
}

// Arming auto switches the agent to its normal mode, so the agent confirms
// *that* id in a current_mode_update. Publishing it raw overwrote the `auto`
// the user had just selected — a chip naming a mode the daemon was not
// enforcing. Caught live against grok 0.2.114, not by the fakes above, because
// nothing here drove the notification path.
func TestCurrentModeUpdateDoesNotOverwriteArmedAuto(t *testing.T) {
	s := autoModeSession(t)
	s.mu.Lock()
	s.autoApprove = true
	s.mu.Unlock()

	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		SessionId: acp.SessionId(s.agentID),
		Update: acp.SessionUpdate{
			CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
				CurrentModeId: "default",
			},
		},
	})
	if err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeMode {
		t.Fatalf("want a mode event, got %s", ev.Type)
	}
	if ev.CurrentModeID != autoModeID {
		t.Fatalf("current = %q, want auto to survive the agent's confirmation",
			ev.CurrentModeID)
	}
}

// staticModes is shared by every session built from one Spec. Appending into
// its spare capacity would write through to another session's list.
func TestAdvertisedModesDoesNotMutateTheSharedSpecList(t *testing.T) {
	shared := make([]event.SessionMode, 2, 4) // spare capacity: the hazard
	shared[0] = event.SessionMode{ID: "default", Name: "Build"}
	shared[1] = event.SessionMode{ID: "plan", Name: "Plan"}

	a := autoModeSession(t)
	a.staticModes = shared
	b := autoModeSession(t)
	b.staticModes = shared

	first := a.advertisedModes(shared)
	second := b.advertisedModes(shared)

	if len(shared) != 2 {
		t.Fatalf("shared list grew to %d", len(shared))
	}
	if first[2].ID != autoModeID || second[2].ID != autoModeID {
		t.Fatal("each session must get its own auto entry")
	}
	if &first[0] == &second[0] {
		t.Fatal("sessions must not share one backing array")
	}
}
