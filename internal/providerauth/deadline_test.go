package providerauth

import (
	"context"
	"testing"
	"time"
)

// MADR 0074 D33 (PLAN P23 step 3): an owned flow ends when the provider's code
// does. The reservation carries a deadline of its own, apart from the resume
// window's timer, and a flow that is ready_to_activate has outlived its code.

// attachedWithLifetime reserves, attaches a stub whose code lives seconds, and
// returns both.
func attachedWithLifetime(t *testing.T, r *Registry, seconds int) (*Reservation, *stubHandle) {
	t.Helper()
	res, err := r.Reserve(context.Background(), "dev-1", "grok", "xai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(nil)
	h.cancelErr = ErrFlowCancelled
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A", ExpiresIn: seconds}); err != nil {
		t.Fatal(err)
	}
	return res, h
}

// TestOwnedFlowReportsTheCodesLifetime: the reported expires_in is the code's
// own lifetime, not the reservation's DefaultTTL, at both layers that state it.
func TestOwnedFlowReportsTheCodesLifetime(t *testing.T) {
	c, _ := ownedFixture(t)
	bin := fakeLoginScript(t, `sleep 30`)
	flow, err := StartOwnedFlow(context.Background(), OwnedFlowConfig{
		Coordinator:  c,
		Bin:          bin,
		ScanTimeout:  10 * time.Second,
		CodeLifetime: 600 * time.Second,
		EnvFor:       func(home string) []string { return []string{"FAKE_HOME=" + home} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { flow.Cancel(); _ = flow.Wait(context.Background()) }()
	if got := flow.Flow().ExpiresIn; got != 600 {
		t.Fatalf("the owned flow reports expires_in %d, want its code's 600", got)
	}

	r := NewRegistry()
	res, _ := attachedWithLifetime(t, r, flow.Flow().ExpiresIn)
	defer func() { res.Cancel(); <-res.Done() }()
	if got := res.ExpiresIn(); got < 595 || got > 600 {
		t.Fatalf("the reservation reports expires_in %d, want the code's 600, not DefaultTTL's %d",
			got, int(DefaultTTL.Seconds()))
	}
}

// TestOwnedFlowExpiresAtItsDeadline: a flow whose code has run out is
// cancelled through its handle and ends expired, not cancelled.
func TestOwnedFlowExpiresAtItsDeadline(t *testing.T) {
	r := NewRegistry()
	res, h := attachedWithLifetime(t, r, 1)
	select {
	case <-res.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a flow outlived its code: nothing enforced the deadline")
	}
	if h.cancelCount() == 0 {
		t.Fatal("the expired flow's handle was not cancelled")
	}
	if got := res.State(); got != ResultExpired {
		t.Fatalf("state = %q, want %q", got, ResultExpired)
	}
	if !Retryable(res.State()) {
		t.Fatal("an expired flow must be retryable")
	}
	if r.Len() != 0 {
		t.Fatal("the expired flow was not reaped")
	}
}

// TestReadyToActivateOutlivesTheCode: once the flow reports ready_to_activate
// the code's expiry no longer applies; the validated candidate waits for an
// idle provider, bounded by ActivationGrace.
func TestReadyToActivateOutlivesTheCode(t *testing.T) {
	r := NewRegistry()
	res, h := attachedWithLifetime(t, r, 1)
	h.updates <- ResultReadyToActivate

	select {
	case <-res.Done():
		t.Fatalf("a ready_to_activate flow was ended at its code's expiry, state %q", res.State())
	case <-time.After(2500 * time.Millisecond):
	}
	if h.cancelCount() != 0 {
		t.Fatal("a ready_to_activate flow was cancelled at its code's expiry")
	}
	h.finish()
	select {
	case <-res.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the flow did not complete once released")
	}
	if got := res.State(); got != ResultCompleted {
		t.Fatalf("state = %q, want %q", got, ResultCompleted)
	}
}

// TestDeadlineSurvivesDetachAndResume: the resume window's timer is replaced
// by detach and cleared by reattach; the code's deadline is neither.
func TestDeadlineSurvivesDetachAndResume(t *testing.T) {
	r := NewRegistry()
	res, h := attachedWithLifetime(t, r, 2)

	if n := r.DetachDevice("dev-1", time.Minute); n != 1 {
		t.Fatalf("detached %d flows, want 1", n)
	}
	time.Sleep(200 * time.Millisecond)
	if resumed := r.ResumeDevice("dev-1"); len(resumed) != 1 {
		t.Fatalf("resume returned %d flows, want 1", len(resumed))
	}
	select {
	case <-res.Done():
	case <-time.After(6 * time.Second):
		t.Fatal("a resumed flow outlived its code: the deadline was lost with the resume window's timer")
	}
	if got := res.State(); got != ResultExpired {
		t.Fatalf("state = %q, want %q", got, ResultExpired)
	}
	if h.cancelCount() == 0 {
		t.Fatal("the expired flow's handle was not cancelled")
	}
}

// TestUpdateThenFinishDoesNotRace (PLAN P23 deviation 2026-11-11): the owner
// goroutine ends the flow as soon as Wait returns, which can be while forward
// is still sending the handle's last update. finish's close of the updates
// channel must not race, or panic on, that send. Run under -race.
func TestUpdateThenFinishDoesNotRace(t *testing.T) {
	for range 200 {
		r := NewRegistry()
		res, err := r.Reserve(context.Background(), "dev-1", "grok", "xai")
		if err != nil {
			t.Fatal(err)
		}
		h := newStubHandle(nil)
		if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
			t.Fatal(err)
		}
		h.updates <- ResultReadyToActivate
		h.finish()
		<-res.Done()
	}
}

// TestFinishStopsBothTimers: a flow that ends on its own leaves no timer
// armed, so a later fire cannot touch a reaped reservation.
func TestFinishStopsBothTimers(t *testing.T) {
	r := NewRegistry()
	res, h := attachedWithLifetime(t, r, 1)
	r.DetachDevice("dev-1", time.Minute)
	h.finish()
	<-res.Done()
	res.mu.Lock()
	timer, deadline := res.timer, res.deadline
	res.mu.Unlock()
	if timer != nil || deadline != nil {
		t.Fatalf("timers left armed after finish: resume=%v deadline=%v", timer != nil, deadline != nil)
	}
	time.Sleep(1200 * time.Millisecond)
	if got := res.State(); got != ResultCompleted {
		t.Fatalf("state = %q after the old deadline passed, want %q", got, ResultCompleted)
	}
}
