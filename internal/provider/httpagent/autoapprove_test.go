package httpagent

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

// newBareSession builds the minimum session needed to exercise the per-session
// auto-approve state and the permission bookkeeping around it. It deliberately
// does not start the engine: these are state accessors, not transport.
func newBareSession() *session {
	return &session{
		localID: "local-1",
		agentID: "ses_test",
		done:    make(chan struct{}),
		pending: map[string]struct{}{},
	}
}

func TestAutoApproveDefaultsOff(t *testing.T) {
	s := newBareSession()
	if s.AutoApprove() {
		t.Fatal("auto-approve must default to off: it is opt-in per session (MADR 0044 D8)")
	}
}

func TestAutoApproveRoundTrips(t *testing.T) {
	s := newBareSession()
	s.SetAutoApprove(true)
	if !s.AutoApprove() {
		t.Fatal("SetAutoApprove(true) did not stick")
	}
	s.SetAutoApprove(false)
	if s.AutoApprove() {
		t.Fatal("SetAutoApprove(false) did not disarm")
	}
}

// TestAutoApproveConcurrentWithAgentSwitch is the -race guard: AutoApprove joins
// Agent/Model as mutable session identity read by the SSE goroutine on every
// permission event, so all three must share s.mu.
func TestAutoApproveConcurrentWithAgentSwitch(t *testing.T) {
	s := newBareSession()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				switch (i + j) % 5 {
				case 0:
					s.SetAutoApprove(j%2 == 0)
				case 1:
					_ = s.AutoApprove()
				case 2:
					s.SetAgent(fmt.Sprintf("agent-%d", j))
				case 3:
					_ = s.Agent()
				case 4:
					_ = s.PendingPermissions()
				}
			}
		}()
	}
	wg.Wait()
}

func TestPendingPermissionsSnapshot(t *testing.T) {
	s := newBareSession()
	if got := s.PendingPermissions(); len(got) != 0 {
		t.Fatalf("empty session reported %v pending", got)
	}

	// TrackPermission with no PermissionTimeout configured must not arm the
	// expiry goroutine, so this stays a pure state test.
	s.p = &Provider{cfg: Config{}}
	s.TrackPermission("per_a")
	s.TrackPermission("per_b")

	got := s.PendingPermissions()
	slices.Sort(got)
	if !slices.Equal(got, []string{"per_a", "per_b"}) {
		t.Fatalf("pending = %v, want [per_a per_b]", got)
	}

	// The returned slice must be a copy: the arming sweep answers each id while
	// iterating, which mutates s.pending.
	got[0] = "mutated"
	again := s.PendingPermissions()
	slices.Sort(again)
	if !slices.Equal(again, []string{"per_a", "per_b"}) {
		t.Fatalf("PendingPermissions returned a live view: %v", again)
	}

	if !s.TakePending("per_a") {
		t.Fatal("TakePending(per_a) should claim it")
	}
	if s.TakePending("per_a") {
		t.Fatal("TakePending must not claim the same id twice")
	}
	if got := s.PendingPermissions(); !slices.Equal(got, []string{"per_b"}) {
		t.Fatalf("after claim, pending = %v, want [per_b]", got)
	}
}

// TestRespondPermissionNotPendingIsSentinel pins the error identity the
// auto-approve retry loop branches on: a lost race must be distinguishable from
// a transport failure without matching on message text (MADR 0044 plan §2.2).
func TestRespondPermissionNotPendingIsSentinel(t *testing.T) {
	s := newBareSession()
	s.p = &Provider{cfg: Config{}}

	err := s.RespondPermission(t.Context(), "per_missing", "once", false, "dev-1")
	if err == nil {
		t.Fatal("answering an untracked permission must fail")
	}
	if !errors.Is(err, ErrPermissionNotPending) {
		t.Fatalf("err = %v, want errors.Is(..., ErrPermissionNotPending)", err)
	}
	if !strings.Contains(err.Error(), "per_missing") {
		t.Errorf("err %q should name the id it rejected", err)
	}
}

func TestDoneClosesOnSessionClose(t *testing.T) {
	s := newBareSession()
	select {
	case <-s.Done():
		t.Fatal("Done must not be closed on a live session")
	default:
	}
	close(s.done)
	select {
	case <-s.Done():
	default:
		t.Fatal("Done must be closed once the session shuts down")
	}
}
