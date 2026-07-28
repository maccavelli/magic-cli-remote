package codex

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
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
// opt-in decision rather than one tap away (MADR 0044 D5).
func TestFullAccessModeIsGated(t *testing.T) {
	t.Run("hidden_by_default", func(t *testing.T) {
		got := advertisedIDs(Config{})
		if strings.Join(got, ",") != "read-only,auto" {
			t.Fatalf("modes = %v, want [read-only auto]", got)
		}
	})

	t.Run("advertised_when_allowed", func(t *testing.T) {
		got := advertisedIDs(Config{AllowFullAccess: true})
		if strings.Join(got, ",") != "read-only,auto,full-access" {
			t.Fatalf("modes = %v, want all three", got)
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

	if err := s.SetMode(context.Background(), modeReadOnly); err != nil {
		t.Fatalf("SetMode(read-only): %v", err)
	}
	approval, sandbox = s.policy()
	if approval != "on-request" || sandbox != "read-only" {
		t.Fatalf("policy = (%q,%q), want (on-request, read-only)", approval, sandbox)
	}
	s.mu.Lock()
	auto = s.autoApprove
	s.mu.Unlock()
	if auto {
		t.Error("switching away from auto must disarm the interception layer")
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

// TestSeededPolicyReportsMatchingMode: config that happens to match a mode
// should show that mode as current; config that matches none shows nothing.
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

	t.Run("unmatched_reports_no_current", func(t *testing.T) {
		s := modeTestSession(t, Config{ApprovalPolicy: "untrusted", SandboxMode: "read-only"})
		s.emitModes()
		for _, ev := range drainModeEvents(s) {
			if ev.Type == event.TypeMode && ev.CurrentModeID != "" {
				t.Fatalf("current mode = %q, want empty for a config matching no mode",
					ev.CurrentModeID)
			}
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

	// An approval this session surfaced and is blocked on.
	s.mu.Lock()
	s.pendingPerms["per_waiting"] = json.RawMessage(`7`)
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
		}
	}
	if resolved != 1 {
		t.Errorf("permission_resolved for the swept id = %d, want 1 so the "+
			"sheet on the phone is dismissed", resolved)
	}
}

// A sweep with nothing pending must not reach for the engine framer.
func TestSweepWithNothingPendingIsANoOp(t *testing.T) {
	s := modeTestSession(t, Config{})
	s.sweepPendingApprovals()
}

// Codex has no plan agent, so adding modes must not accidentally make /plan
// look supported. The daemon resolves plan mode by id from the advertised list;
// codex must simply not offer one (MADR 0044 plan §4.9).
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
