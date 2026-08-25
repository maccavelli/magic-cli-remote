package codex

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func advertisedIDs(cfg Config) []string {
	modes := advertisedModes(cfg)
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, m.ID)
	}
	return out
}

// Full access removes the sandbox as well as the prompts, so it is a separate,
// opt-in decision rather than one tap away (MADR 0044 D5). Default is always
// first (MADR 0047 D1).
func TestFullAccessModeIsGated(t *testing.T) {
	t.Run("hidden_by_default", func(t *testing.T) {
		got := advertisedIDs(Config{})
		if strings.Join(got, ",") != "default,read-only,auto" {
			t.Fatalf("modes = %v, want [default read-only auto]", got)
		}
	})

	t.Run("advertised_when_allowed", func(t *testing.T) {
		got := advertisedIDs(Config{AllowFullAccess: true})
		if strings.Join(got, ",") != "default,read-only,auto,full-access" {
			t.Fatalf("modes = %v, want [default read-only auto full-access]", got)
		}
	})

	t.Run("not_selectable_when_gated", func(t *testing.T) {
		if _, ok := findCodexMode(Config{}, modeFullAccess); ok {
			t.Fatal("full-access must not resolve while the gate is off")
		}
		if _, ok := findCodexMode(Config{AllowFullAccess: true}, modeFullAccess); !ok {
			t.Fatal("full-access must resolve once allowed")
		}
	})
}

func TestDefaultModeIsNotDangerous(t *testing.T) {
	m, ok := findCodexMode(Config{}, modeDefault)
	if !ok {
		t.Fatal("default must always be advertised")
	}
	if m.mode.Dangerous {
		t.Error("default must not be flagged dangerous")
	}
	if m.approvalPolicy != "on-request" || m.sandbox != "workspace-write" {
		t.Errorf("default policy = (%q,%q), want (on-request, workspace-write)",
			m.approvalPolicy, m.sandbox)
	}
}

func TestDefaultModeIsDistinctFromAuto(t *testing.T) {
	def, _ := findCodexMode(Config{}, modeDefault)
	auto, _ := findCodexMode(Config{}, modeAuto)
	if def.approvalPolicy == auto.approvalPolicy && def.sandbox == auto.sandbox {
		t.Fatal("default and auto must not share the same policy pair")
	}
	if auto.approvalPolicy != "never" || auto.sandbox != "workspace-write" {
		t.Errorf("auto = (%q,%q), want (never, workspace-write)",
			auto.approvalPolicy, auto.sandbox)
	}
}

func TestSeedPolicy(t *testing.T) {
	t.Run("empty_config_is_default", func(t *testing.T) {
		ap, sb, id := seedPolicy(Config{})
		if id != modeDefault || ap != "on-request" || sb != "workspace-write" {
			t.Fatalf("seedPolicy(empty) = (%q,%q,%q), want (on-request, workspace-write, default)",
				ap, sb, id)
		}
	})

	t.Run("never_alone_repairs_to_auto", func(t *testing.T) {
		// Live codex: never without sandbox leaves untrusted projects in
		// readOnly — the repair must set workspace-write (MADR 0047 D2.2).
		ap, sb, id := seedPolicy(Config{ApprovalPolicy: "never"})
		if id != modeAuto || ap != "never" || sb != "workspace-write" {
			t.Fatalf("seedPolicy(never alone) = (%q,%q,%q), want auto pair",
				ap, sb, id)
		}
	})

	t.Run("matching_auto", func(t *testing.T) {
		ap, sb, id := seedPolicy(Config{
			ApprovalPolicy: "never", SandboxMode: "workspace-write",
		})
		if id != modeAuto || ap != "never" || sb != "workspace-write" {
			t.Fatalf("seedPolicy(auto pair) = (%q,%q,%q)", ap, sb, id)
		}
	})

	t.Run("matching_read_only", func(t *testing.T) {
		ap, sb, id := seedPolicy(Config{
			ApprovalPolicy: "on-request", SandboxMode: "read-only",
		})
		if id != modeReadOnly || ap != "on-request" || sb != "read-only" {
			t.Fatalf("seedPolicy(read-only) = (%q,%q,%q)", ap, sb, id)
		}
	})

	t.Run("gated_full_access_falls_to_default", func(t *testing.T) {
		ap, sb, id := seedPolicy(Config{
			ApprovalPolicy: "never", SandboxMode: "danger-full-access",
		})
		if id != modeDefault {
			t.Fatalf("gated full-access seed id = %q, want default", id)
		}
		if ap != "on-request" || sb != "workspace-write" {
			t.Fatalf("gated full-access seed policy = (%q,%q)", ap, sb)
		}
	})

	t.Run("partial_sandbox_only_falls_to_default", func(t *testing.T) {
		_, _, id := seedPolicy(Config{SandboxMode: "workspace-write"})
		if id != modeDefault {
			t.Fatalf("partial config id = %q, want default", id)
		}
	})
}

func TestNewSessionEmptyConfigEmitsDefaultCurrent(t *testing.T) {
	s := modeTestSession(t, Config{})
	s.emitModes()
	var got string
	for _, ev := range drainModeEvents(s) {
		if ev.Type == event.TypeMode {
			got = ev.CurrentModeID
		}
	}
	if got != modeDefault {
		t.Fatalf("current mode = %q, want %q", got, modeDefault)
	}
	ap, sb := s.policy()
	if ap != "on-request" || sb != "workspace-write" {
		t.Fatalf("live policy = (%q,%q), want default pair", ap, sb)
	}
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()
	if auto {
		t.Error("empty create must not arm auto-approve")
	}
}

func TestNewSessionNeverAloneArmsAutoWithSandbox(t *testing.T) {
	s := modeTestSession(t, Config{ApprovalPolicy: "never"})
	ap, sb := s.policy()
	if ap != "never" || sb != "workspace-write" {
		t.Fatalf("policy = (%q,%q), want auto pair", ap, sb)
	}
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()
	if !auto {
		t.Error("never alone must arm autoApprove after seed repair")
	}
	s.emitModes()
	var got string
	for _, ev := range drainModeEvents(s) {
		if ev.Type == event.TypeMode {
			got = ev.CurrentModeID
		}
	}
	if got != modeAuto {
		t.Fatalf("current = %q, want auto", got)
	}
}

// Auto pairs never with workspace-write, not danger-full-access: removing the
// human from the loop and removing the sandbox are separate decisions.
func TestAutoModeKeepsTheSandbox(t *testing.T) {
	m, ok := findCodexMode(Config{}, modeAuto)
	if !ok {
		t.Fatal("auto must always be advertised")
	}
	if m.approvalPolicy != "never" {
		t.Errorf("auto approvalPolicy = %q, want never (no prompts)", m.approvalPolicy)
	}
	if m.sandbox != "workspace-write" {
		t.Errorf("auto sandbox = %q, want workspace-write — an unattended session "+
			"must still be contained", m.sandbox)
	}
	if !m.mode.Dangerous {
		t.Error("auto must be marked dangerous so the client can confirm before arming")
	}
}

func TestReadOnlyModeIsNotDangerous(t *testing.T) {
	m, _ := findCodexMode(Config{}, modeReadOnly)
	if m.mode.Dangerous {
		t.Error("read-only must not be flagged dangerous")
	}
	if m.approvalPolicy == "never" {
		t.Error("read-only must still ask")
	}
}

func TestModeIDForRoundTrips(t *testing.T) {
	cfg := Config{AllowFullAccess: true}
	for _, m := range availableCodexModes(cfg) {
		if got := modeIDFor(cfg, m.approvalPolicy, m.sandbox); got != m.mode.ID {
			t.Errorf("modeIDFor(%q,%q) = %q, want %q",
				m.approvalPolicy, m.sandbox, got, m.mode.ID)
		}
	}

	// An unrecognised pair reports no current mode rather than naming one that
	// is not really in effect — the chip is the user's only signal about
	// whether the agent can act unattended.
	if got := modeIDFor(cfg, "untrusted", "read-only"); got != "" {
		t.Errorf("modeIDFor(unmatched) = %q, want empty", got)
	}
	// full-access is a real pair, but not while the gate is off.
	if got := modeIDFor(Config{}, "never", "danger-full-access"); got != "" {
		t.Errorf("modeIDFor(full-access, gated) = %q, want empty", got)
	}
}

// turn/start's sandboxPolicy is an internally tagged enum with camelCase
// variants — a different type from thread/start's kebab-case string. Verified
// live against codex 0.145 (MADR 0044 Finding 4).
func TestSandboxPolicyParamShape(t *testing.T) {
	tests := []struct {
		mode     string
		wantType string
	}{
		{"read-only", "readOnly"},
		{"workspace-write", "workspaceWrite"},
		{"danger-full-access", "dangerFullAccess"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := sandboxPolicyParam(tt.mode)
			if got == nil {
				t.Fatalf("sandboxPolicyParam(%q) = nil", tt.mode)
			}
			if got["type"] != tt.wantType {
				t.Fatalf("type = %v, want %q (camelCase, not the kebab-case "+
					"thread/start spelling)", got["type"], tt.wantType)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), tt.mode) {
				t.Errorf("%s leaked the thread/start spelling into turn/start: %s",
					tt.mode, raw)
			}
		})
	}

	if sandboxPolicyParam("") != nil {
		t.Error("an unset sandbox must omit sandboxPolicy entirely")
	}
	if sandboxPolicyParam("bogus") != nil {
		t.Error("an unknown sandbox must omit sandboxPolicy rather than guess")
	}
}

func modeTestSession(t *testing.T, cfg Config) *session {
	t.Helper()
	return newSession(nil, cfg, provider.StartOptions{
		LocalSessionID: "local-1", CWD: t.TempDir(),
	}, testLogger(t))
}

func drainModeEvents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestSetModeRewritesPolicy(t *testing.T) {
	s := modeTestSession(t, Config{AllowFullAccess: true})

	if err := s.SetMode(context.Background(), modeAuto); err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}
	approval, sandbox := s.policy()
	if approval != "never" || sandbox != "workspace-write" {
		t.Fatalf("policy = (%q,%q), want (never, workspace-write)", approval, sandbox)
	}
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()
	if !auto {
		t.Error("auto mode must arm the interception layer too (MADR 0044 D6)")
	}

	var sawMode bool
	for _, ev := range drainModeEvents(s) {
		if ev.Type == event.TypeMode && ev.CurrentModeID == modeAuto {
			sawMode = true
		}
	}
	if !sawMode {
		t.Error("a mode switch must confirm itself with a session_mode event")
	}

	if err := s.SetMode(context.Background(), modeDefault); err != nil {
		t.Fatalf("SetMode(default): %v", err)
	}
	approval, sandbox = s.policy()
	if approval != "on-request" || sandbox != "workspace-write" {
		t.Fatalf("policy = (%q,%q), want default pair (on-request, workspace-write)",
			approval, sandbox)
	}
	s.mu.Lock()
	auto = s.autoApprove
	s.mu.Unlock()
	if auto {
		t.Error("switching away from auto must disarm the interception layer")
	}

	if err := s.SetMode(context.Background(), modeReadOnly); err != nil {
		t.Fatalf("SetMode(read-only): %v", err)
	}
	approval, sandbox = s.policy()
	if approval != "on-request" || sandbox != "read-only" {
		t.Fatalf("policy = (%q,%q), want (on-request, read-only)", approval, sandbox)
	}
}

func TestSetModeRejectsUnknownAndGated(t *testing.T) {
	s := modeTestSession(t, Config{})
	before, beforeSandbox := s.policy()

	if err := s.SetMode(context.Background(), "bogus"); err == nil {
		t.Error("unknown mode must error")
	}
	if err := s.SetMode(context.Background(), modeFullAccess); err == nil {
		t.Error("gated full-access must error rather than silently apply")
	}

	after, afterSandbox := s.policy()
	if after != before || afterSandbox != beforeSandbox {
		t.Errorf("a rejected switch changed policy: (%q,%q) -> (%q,%q)",
			before, beforeSandbox, after, afterSandbox)
	}
}

// TestSeededPolicyReportsMatchingMode: config that matches a mode shows that
// mode; unmatched pairs are repaired to default by seedPolicy (MADR 0047 D2).
func TestSeededPolicyReportsMatchingMode(t *testing.T) {
	t.Run("matching", func(t *testing.T) {
		s := modeTestSession(t, Config{ApprovalPolicy: "never", SandboxMode: "workspace-write"})
		s.emitModes()
		var got string
		for _, ev := range drainModeEvents(s) {
			if ev.Type == event.TypeMode {
				got = ev.CurrentModeID
			}
		}
		if got != modeAuto {
			t.Fatalf("current mode = %q, want %q", got, modeAuto)
		}
	})

	t.Run("unmatched_pair_seeds_default", func(t *testing.T) {
		// untrusted + read-only matches no advertised mode; seed falls to default.
		s := modeTestSession(t, Config{ApprovalPolicy: "untrusted", SandboxMode: "read-only"})
		s.emitModes()
		var got string
		for _, ev := range drainModeEvents(s) {
			if ev.Type == event.TypeMode {
				got = ev.CurrentModeID
			}
		}
		if got != modeDefault {
			t.Fatalf("current mode = %q, want default after seed repair", got)
		}
	})
}

// TestArmingAutoSweepsPendingApprovals covers the main use case: arming auto to
// unblock an agent that is already waiting on an approval (MADR 0044 D4.5).
func TestArmingAutoSweepsPendingApprovals(t *testing.T) {
	s := modeTestSession(t, Config{})

	var mu sync.Mutex
	var decisions []string
	s.respond = func(_ context.Context, _ json.RawMessage, result any, _ *rpcErrorBody) error {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := result.(map[string]any); ok {
			decisions = append(decisions, m["decision"].(string))
		}
		return nil
	}

	// An approval this session surfaced and is blocked on. The descriptor is
	// what lets the sweep name it in the approval audit (MADR 0051 §4.4).
	s.mu.Lock()
	s.trackPendingLocked("per_waiting", pendingCallback{
		rpcID: json.RawMessage(`7`), tool: "shell", detail: "make test",
	})
	s.mu.Unlock()

	if err := s.SetMode(context.Background(), modeAuto); err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}

	mu.Lock()
	got := append([]string(nil), decisions...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "accept" {
		t.Fatalf("sweep decisions = %v, want one accept", got)
	}

	s.mu.Lock()
	left := len(s.pendingPerms)
	s.mu.Unlock()
	if left != 0 {
		t.Errorf("%d approvals left pending after the sweep", left)
	}

	var resolved int
	for _, ev := range drainModeEvents(s) {
		if ev.Type == event.TypePermissionResolved && ev.PermissionID == "per_waiting" {
			resolved++
			// MADR 0077 §1: arming auto answers previously pending
			// permissions in bulk, not a single fresh human tap — an
			// accidental populate-by-copy-paste here would misattribute a
			// bulk auto-approve to a specific device.
			if ev.DeviceID != "" || ev.OptionID != "" {
				t.Errorf("swept event device_id=%q option_id=%q, want both empty (no device decided this)",
					ev.DeviceID, ev.OptionID)
			}
		}
	}
	if resolved != 1 {
		t.Errorf("permission_resolved for the swept id = %d, want 1 so the "+
			"sheet on the phone is dismissed", resolved)
	}
}

// TestSweepFoldsApprovalsIntoAuditInOrder is the guard for MADR 0051 §4.4: the
// sweep must name what it approved, and always in the same order.
//
// pendingPerms used to hold only the JSON-RPC id, so a swept permission could
// not be described at all. It now carries the descriptor captured when the
// permission was raised, and pendingOrder pins the sequence — map iteration is
// random, so without it the audit list reshuffles between runs.
func TestSweepFoldsApprovalsIntoAuditInOrder(t *testing.T) {
	s := modeTestSession(t, Config{})
	s.respond = func(_ context.Context, _ json.RawMessage, _ any, _ *rpcErrorBody) error {
		return nil
	}

	// Enough entries that Go's map-iteration randomisation cannot coincidentally
	// reproduce insertion order: with three it matched roughly a third of runs,
	// which is a flaky guard rather than a guard.
	const n = 12
	want := make([]struct{ id, tool, detail string }, 0, n)
	for i := range n {
		want = append(want, struct{ id, tool, detail string }{
			id:     "per_" + strconv.Itoa(i),
			tool:   "shell",
			detail: "cmd" + strconv.Itoa(i),
		})
	}
	s.mu.Lock()
	for _, w := range want {
		s.trackPendingLocked(w.id, pendingCallback{
			rpcID: json.RawMessage(`1`), tool: w.tool, detail: w.detail,
		})
	}
	s.mu.Unlock()

	s.sweepPendingApprovals()

	var last event.Event
	var summaries int
	for _, ev := range drainModeEvents(s) {
		if ev.Type == event.TypeApprovalSummary {
			summaries++
			last = ev
		}
	}
	if summaries != len(want) {
		t.Fatalf("approval_summary events = %d, want %d (one per swept approval)",
			summaries, len(want))
	}
	if len(last.Approvals) != len(want) {
		t.Fatalf("final audit has %d items, want %d", len(last.Approvals), len(want))
	}
	for i, w := range want {
		got := last.Approvals[i]
		if got.ToolName != w.tool || got.Detail != w.detail {
			t.Errorf("audit[%d] = %+v, want tool=%q detail=%q",
				i, got, w.tool, w.detail)
		}
	}
}

// A sweep with nothing pending must not reach for the engine framer.
func TestSweepWithNothingPendingIsANoOp(t *testing.T) {
	s := modeTestSession(t, Config{})
	s.sweepPendingApprovals()
}

// Codex must not advertise plan as an autonomy SessionMode. Collaboration
// Plan is a separate axis (MADR 0080 D12). The daemon still resolves KindMode
// /plan from this list, so a plan id here would hijack the wrong handler.
func TestCodexAdvertisesNoPlanMode(t *testing.T) {
	for _, cfg := range []Config{{}, {AllowFullAccess: true}} {
		for _, m := range advertisedModes(cfg) {
			if strings.EqualFold(m.ID, "plan") {
				t.Fatalf("codex advertised a plan mode (%+v); /plan would silently "+
					"start routing to it", m)
			}
		}
	}
	if _, ok := findCodexMode(Config{AllowFullAccess: true}, "plan"); ok {
		t.Fatal("plan must not resolve on codex")
	}
}

func TestApprovalSummaryIsBounded(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := approvalSummary("shell", long)
	if n := utf8.RuneCountInString(got); n > maxApprovalSummary {
		t.Fatalf("summary length %d exceeds cap %d", n, maxApprovalSummary)
	}

	// Multi-byte input must not be cut mid-rune, and the ellipsis must not
	// push the result past the cap.
	wide := approvalSummary("shell", strings.Repeat("é", 500))
	if n := utf8.RuneCountInString(wide); n > maxApprovalSummary {
		t.Errorf("multi-byte summary length %d exceeds cap %d", n, maxApprovalSummary)
	}
	if !utf8.ValidString(wide) {
		t.Error("truncation split a multi-byte rune")
	}
	if got := approvalSummary("shell", "git status"); got != "shell (git status)" {
		t.Errorf("summary = %q", got)
	}
	if got := approvalSummary("", ""); got != "approval" {
		t.Errorf("summary with no detail = %q, want a usable label", got)
	}
}

// Pins that SetMode(auto) puts both never and workspaceWrite on the next
// turn/start. In-memory assertions alone would hide a never-without-sandbox
// regression (MADR 0047 D5).
func TestTurnStartAfterAutoCarriesWorkspaceWrite(t *testing.T) {
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

	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, dead: make(chan struct{})}

	s := newSession(p, Config{}, provider.StartOptions{
		LocalSessionID: "local-1", CWD: t.TempDir(),
	}, testLogger(t))
	s.agentID = "thread-1"

	if err := s.SetMode(context.Background(), modeAuto); err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- s.beginTurn(ctx, []provider.Content{{Type: "text", Text: "hi"}}, true)
	}()

	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if req.Method != "turn/start" {
		t.Fatalf("method = %q, want turn/start", req.Method)
	}
	if got, _ := req.Params["approvalPolicy"].(string); got != "never" {
		t.Errorf("approvalPolicy = %#v, want never", req.Params["approvalPolicy"])
	}
	pol, ok := req.Params["sandboxPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("sandboxPolicy missing or wrong type: %#v", req.Params["sandboxPolicy"])
	}
	if pol["type"] != "workspaceWrite" {
		t.Errorf("sandboxPolicy.type = %#v, want workspaceWrite", pol["type"])
	}

	resp, err := json.Marshal(map[string]any{
		"id":     req.ID,
		"result": map[string]any{"turn": map[string]any{"id": "turn-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engineW.Write(append(resp, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Logf("beginTurn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("beginTurn hang")
	}
}
