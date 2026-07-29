package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// replyLog records the engine-side permission replies a test provoked, so the
// assertions can talk about "how many replies reached the engine" rather than
// about internal state.
type replyLog struct {
	mu      sync.Mutex
	paths   []string
	bodies  []string
	failFor int   // fail the first N calls
	err     error // error to return while failing
}

func (r *replyLog) api(_ context.Context, method, path string, body, _ any) error {
	r.mu.Lock()
	n := len(r.paths)
	r.paths = append(r.paths, method+" "+path)
	if b, err := json.Marshal(body); err == nil {
		r.bodies = append(r.bodies, string(b))
	}
	fail := n < r.failFor
	err := r.err
	r.mu.Unlock()
	if fail {
		return err
	}
	return nil
}

func (r *replyLog) replies() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.paths))
	for _, p := range r.paths {
		if strings.Contains(p, "/permission/") || strings.Contains(p, "/permissions/") {
			out = append(out, p)
		}
	}
	return out
}

// autoSession wires a dialect session to a captureHost with auto-approve armed
// and the engine replaced by log.
func autoSession(t *testing.T, log *replyLog, auto bool) (*httpSession, *captureHost) {
	t.Helper()
	h := &captureHost{api: log.api}
	h.SetAutoApprove(auto)
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)
	h.mu.Lock()
	h.ds = s
	h.mu.Unlock()
	return s, h
}

// waitForEvent polls for an event of type typ. Auto-approval is asynchronous by
// design (Emit of a control event blocks), so tests need a bounded wait rather
// than a sleep.
func waitForEvent(t *testing.T, h *captureHost, typ event.Type) event.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, e := range h.events {
			if e.Type == typ {
				h.mu.Unlock()
				return e
			}
		}
		h.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no %s event within 3s; saw %v", typ, eventTypes(h))
	return event.Event{}
}

func eventTypes(h *captureHost) []event.Type {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]event.Type, 0, len(h.events))
	for _, e := range h.events {
		out = append(out, e.Type)
	}
	return out
}

// TestAutoApprovalSummaryConcurrent is the regression guard for the ordering
// hazard in MADR 0051 §4.3: autoApprove gives every permission its own
// goroutine, so without approvalMu two approvals can emit their snapshots out
// of order and the shorter one — landing last — replaces the longer under the
// event's replace semantics, silently dropping an approval from the audit.
//
// It asserts two things a single-threaded test cannot: that the final snapshot
// is complete, and that snapshot lengths never go backwards in emit order.
// Remove the approvalMu lock/unlock in emitApprovalSummary and this fails.
func TestAutoApprovalSummaryConcurrent(t *testing.T) {
	log := &replyLog{}
	s, h := autoSession(t, log, true)

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.HandleEvent("permission.asked", json.RawMessage(
				`{"id":"per_`+strconv.Itoa(i)+`","sessionID":"ses_test",`+
					`"permission":"bash","patterns":["cmd`+strconv.Itoa(i)+`"]}`))
		}()
	}
	wg.Wait()

	// Each approval is answered on its own goroutine, so wait for the last
	// summary rather than assuming they have all landed.
	deadline := time.Now().Add(5 * time.Second)
	var sums []event.Event
	for time.Now().Before(deadline) {
		sums = summaries(h)
		if len(sums) >= n {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(sums) != n {
		t.Fatalf("approval_summary events = %d, want %d", len(sums), n)
	}

	// Monotonic: a snapshot must never be shorter than the one before it.
	for i := 1; i < len(sums); i++ {
		if len(sums[i].Approvals) < len(sums[i-1].Approvals) {
			t.Fatalf("snapshot %d has %d approvals after %d: a stale snapshot "+
				"replaced a longer one, losing approvals",
				i, len(sums[i].Approvals), len(sums[i-1].Approvals))
		}
	}
	if got := len(sums[len(sums)-1].Approvals); got != n {
		t.Errorf("final snapshot has %d approvals, want %d", got, n)
	}

	// Every distinct permission must appear exactly once in the final list.
	seen := make(map[string]int, n)
	for _, a := range sums[len(sums)-1].Approvals {
		seen[a.Detail]++
	}
	for i := range n {
		if c := seen["cmd"+strconv.Itoa(i)]; c != 1 {
			t.Errorf("cmd%d appears %d times in the final summary, want 1", i, c)
		}
	}
}

func summaries(h *captureHost) []event.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]event.Event, 0, len(h.events))
	for _, e := range h.events {
		if e.Type == event.TypeApprovalSummary {
			out = append(out, e)
		}
	}
	return out
}

func countEvents(h *captureHost, typ event.Type) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, e := range h.events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func TestAutoApproveAnswersInsteadOfSurfacing(t *testing.T) {
	log := &replyLog{}
	s, h := autoSession(t, log, true)

	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_1","sessionID":"ses_test","permission":"bash","patterns":["git status"]
	}`))

	// The audit is a structured approval_summary card, not one notice line per
	// approval (MADR 0051 Part I).
	sum := waitForEvent(t, h, event.TypeApprovalSummary)
	if sum.ApprovalGroupID != approvalGroupID {
		t.Errorf("approval_group_id = %q, want %q", sum.ApprovalGroupID, approvalGroupID)
	}
	if sum.Status != event.ApprovalStatusRunning {
		t.Errorf("status = %q, want %q", sum.Status, event.ApprovalStatusRunning)
	}
	if len(sum.Approvals) != 1 {
		t.Fatalf("approvals = %v, want exactly one", sum.Approvals)
	}
	if got := sum.Approvals[0]; got.ToolName != "bash" || got.Detail != "git status" {
		t.Errorf("approval = %+v, want tool_name=bash detail=%q", got, "git status")
	}
	// The fallback line is what an older client renders in place of the card.
	if !strings.Contains(sum.Text, "Auto-approved (1)") {
		t.Errorf("fallback text = %q, want it to name the count", sum.Text)
	}
	if n := countEvents(h, event.TypePermission); n != 0 {
		t.Errorf("emitted %d permission sheets; auto-approve must not surface one", n)
	}
	if n := countEvents(h, event.TypePermissionResolved); n != 1 {
		t.Errorf("permission_resolved count = %d, want exactly 1", n)
	}
	if got := log.replies(); len(got) != 1 {
		t.Errorf("engine replies = %v, want exactly one", got)
	}
	log.mu.Lock()
	bodies := strings.Join(log.bodies, " ")
	log.mu.Unlock()
	if !strings.Contains(bodies, `"once"`) {
		t.Errorf("reply body %q must be \"once\", never \"always\" (MADR 0044 D4.4)", bodies)
	}
}

// TestAutoApproveClaimsThePermissionID is the guard for MADR 0044 plan §2.0 /
// R1b, and the reason this phase exists.
//
// emitPermissionAsk calls TrackPermission, which on a real session arms the
// PermissionTimeout fail-safe. The auto path must therefore answer through the
// *transport* RespondPermission, which claims the id and disarms that expiry.
// Answering through the dialect method instead (same receiver name, compatible
// signature, compiles fine) leaves the id tracked — and one PermissionTimeout
// later the fail-safe cancels a permission the daemon already approved and
// tells the user it timed out.
//
// The transport-side invariant is covered by
// httpagent.TestPermissionAnsweredBeforeExpiryIsNotRejected. This asserts the
// dialect actually routes through it: after an auto-approval, nothing is left
// pending for the expiry to find.
func TestAutoApproveClaimsThePermissionID(t *testing.T) {
	log := &replyLog{}
	s, h := autoSession(t, log, true)

	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_claim","sessionID":"ses_test","permission":"bash","patterns":["ls"]
	}`))
	waitForEvent(t, h, event.TypeApprovalSummary)

	if left := h.PendingPermissions(); len(left) != 0 {
		t.Fatalf("auto-approve left %v pending; the expiry fail-safe will later "+
			"cancel an already-approved permission (MADR 0044 plan §2.0)", left)
	}
	if h.TakePending("per_claim") {
		t.Fatal("per_claim was still claimable: the auto path did not go through " +
			"the transport RespondPermission")
	}
}

// TestAutoApproveDedupesDoubleShape covers the engine sending both
// permission.asked and permission.updated for one id: exactly one reply.
func TestAutoApproveDedupesDoubleShape(t *testing.T) {
	log := &replyLog{}
	s, h := autoSession(t, log, true)

	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_dup","sessionID":"ses_test","permission":"edit","patterns":["a.go"]
	}`))
	s.HandleEvent("permission.updated", json.RawMessage(`{
		"id":"per_dup","sessionID":"ses_test","type":"edit","pattern":"a.go"
	}`))

	waitForEvent(t, h, event.TypeApprovalSummary)
	time.Sleep(50 * time.Millisecond) // let a second reply land if it is going to

	if got := log.replies(); len(got) != 1 {
		t.Fatalf("engine replies = %v, want exactly one for a duplicated id", got)
	}
	if n := countEvents(h, event.TypePermissionResolved); n != 1 {
		t.Errorf("permission_resolved count = %d, want exactly 1", n)
	}
}

func TestAutoApproveHandlesV2Shape(t *testing.T) {
	log := &replyLog{}
	s, h := autoSession(t, log, true)

	s.HandleEvent("permission.v2.asked", json.RawMessage(`{
		"id":"per_v2","sessionID":"ses_test","action":"webfetch","resources":["https://x"]
	}`))

	waitForEvent(t, h, event.TypeApprovalSummary)
	if n := countEvents(h, event.TypePermission); n != 0 {
		t.Errorf("v2 permission surfaced a sheet (%d) despite auto-approve", n)
	}
	if got := log.replies(); len(got) != 1 {
		t.Errorf("engine replies = %v, want one", got)
	}
}

func TestAutoApproveOffSurfacesSheet(t *testing.T) {
	log := &replyLog{}
	s, h := autoSession(t, log, false)

	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_off","sessionID":"ses_test","permission":"bash","patterns":["ls"]
	}`))

	sheet := waitForEvent(t, h, event.TypePermission)
	if sheet.PermissionID != "per_off" {
		t.Errorf("sheet id = %q", sheet.PermissionID)
	}
	if got := log.replies(); len(got) != 0 {
		t.Errorf("auto-approve is off; no reply should have been sent, got %v", got)
	}
}

// TestAutoApproveFallsBackToSheetOnPersistentFailure is the fail-safe: a
// permission the daemon cannot answer must reach the user, because the agent is
// blocked until someone answers it (MADR 0044 D4.1).
func TestAutoApproveFallsBackToSheetOnPersistentFailure(t *testing.T) {
	log := &replyLog{failFor: 100, err: context.DeadlineExceeded}
	s, h := autoSession(t, log, true)

	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_fail","sessionID":"ses_test","permission":"bash","patterns":["rm -rf /"]
	}`))

	sheet := waitForEvent(t, h, event.TypePermission)
	if sheet.PermissionID != "per_fail" {
		t.Errorf("fallback sheet id = %q, want per_fail", sheet.PermissionID)
	}
	var sawFailureNotice bool
	h.mu.Lock()
	for _, e := range h.events {
		if e.Type == event.TypeNotice && strings.Contains(e.Text, "Auto-approve failed") {
			sawFailureNotice = true
		}
	}
	h.mu.Unlock()
	if !sawFailureNotice {
		t.Error("a failed auto-approval must say so, not fail silently")
	}
}

// TestAutoApproveRetriesThenSucceeds: one transient failure must not cost a
// sheet. Both the global and the session-scoped reply paths are tried per
// attempt, so the first attempt burns two calls.
func TestAutoApproveRetriesThenSucceeds(t *testing.T) {
	log := &replyLog{failFor: 2, err: context.DeadlineExceeded}
	s, h := autoSession(t, log, true)

	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_retry","sessionID":"ses_test","permission":"edit","patterns":["b.go"]
	}`))

	waitForEvent(t, h, event.TypeApprovalSummary)
	if n := countEvents(h, event.TypePermission); n != 0 {
		t.Errorf("a retried-then-successful approval must not surface a sheet (got %d)", n)
	}
	if n := countEvents(h, event.TypePermissionResolved); n != 1 {
		t.Errorf("permission_resolved = %d, want 1", n)
	}
}

// TestAutoApproveStopsWhenClaimedElsewhere: the phone answering first is a lost
// race, not an error — no retry storm, no sheet.
func TestAutoApproveStopsWhenClaimedElsewhere(t *testing.T) {
	log := &replyLog{}
	h := &captureHost{api: log.api}
	h.SetAutoApprove(true)
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)
	h.mu.Lock()
	h.ds = s
	h.mu.Unlock()

	// Claim the id before the dialect can, standing in for a phone answer that
	// won the race.
	h.TrackPermission("per_race")
	if !h.TakePending("per_race") {
		t.Fatal("setup: claim failed")
	}

	s.autoApprove(permAsk{ID: "per_race", Name: "bash"})
	time.Sleep(50 * time.Millisecond)

	if got := log.replies(); len(got) != 0 {
		t.Errorf("a lost race must not reply to the engine, got %v", got)
	}
	if n := countEvents(h, event.TypePermission); n != 0 {
		t.Errorf("a lost race must not surface a sheet (got %d)", n)
	}
}

// TestConfigAlwaysApproveStillWorks guards the pre-existing global flag: it must
// keep auto-approving even with the per-session flag off.
func TestConfigAlwaysApproveStillWorks(t *testing.T) {
	log := &replyLog{}
	h := &alwaysApproveHost{captureHost: captureHost{api: log.api}}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)
	h.mu.Lock()
	h.ds = s
	h.mu.Unlock()

	if h.AutoApprove() {
		t.Fatal("setup: the per-session flag must be off for this test to mean anything")
	}
	s.HandleEvent("permission.asked", json.RawMessage(`{
		"id":"per_cfg","sessionID":"ses_test","permission":"bash","patterns":["ls"]
	}`))

	waitForEvent(t, h.captureHost.eventsHost(), event.TypeApprovalSummary)
	if got := log.replies(); len(got) != 1 {
		t.Errorf("config always_approve should still answer, replies=%v", got)
	}
}

// alwaysApproveHost is captureHost with Config().AlwaysApprove set.
type alwaysApproveHost struct{ captureHost }

func (h *alwaysApproveHost) Config() httpagent.Config {
	return httpagent.Config{AlwaysApprove: true}
}

func (h *captureHost) eventsHost() *captureHost { return h }
