package providerauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newFlow(device string) *Flow {
	return &Flow{ProviderID: "kilo", UpstreamID: "kilo", DeviceID: device}
}

func TestAddAssignsIDAndDefaultExpiry(t *testing.T) {
	r := NewRegistry()
	f, ctx, err := r.Add(context.Background(), newFlow("dev-1"))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == "" {
		t.Error("flow has no id")
	}
	if f.ExpiresIn() <= 0 {
		t.Error("flow expired on arrival")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Error("flow context has no deadline; a stuck poll would run forever")
	}
}

// A provider promising a two-hour code does not get one: a device code that
// outlives the user's attention is a liability, not a feature.
func TestAddClampsOverlongExpiry(t *testing.T) {
	r := NewRegistry()
	f := newFlow("dev-1")
	f.ExpiresAt = time.Now().Add(4 * time.Hour)
	got, _, err := r.Add(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(got.ExpiresAt) > DefaultTTL+time.Minute {
		t.Fatalf("expiry not clamped: %s", time.Until(got.ExpiresAt))
	}
}

// MADR 0074 D6/D8 scope flows to their owner. A second device must not be
// able to observe or cancel a flow it did not start — and must not be able to
// tell "not yours" from "does not exist".
func TestFlowsAreDeviceScoped(t *testing.T) {
	r := NewRegistry()
	f, _, err := r.Add(context.Background(), newFlow("dev-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(f.ID, "dev-2"); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("another device read the flow: %v", err)
	}
	if err := r.Cancel(f.ID, "dev-2"); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("another device cancelled the flow: %v", err)
	}
	if _, err := r.Get(f.ID, "dev-1"); err != nil {
		t.Fatalf("owner lost access after a foreign attempt: %v", err)
	}
}

func TestCancelEndsTheContext(t *testing.T) {
	r := NewRegistry()
	f, ctx, err := r.Add(context.Background(), newFlow("dev-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Cancel(f.ID, "dev-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop the flow's context; polling would continue")
	}
	select {
	case <-f.Done():
	case <-time.After(time.Second):
		t.Fatal("Done never closed")
	}
	// Gone from the registry, and a repeat cancel is not an error the phone
	// should have to handle after a reconnect.
	if _, err := r.Get(f.ID, "dev-1"); !errors.Is(err, ErrFlowNotFound) {
		t.Error("cancelled flow still resolvable")
	}
}

func TestPerDeviceAndGlobalCaps(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < MaxFlowsPerDevice; i++ {
		if _, _, err := r.Add(context.Background(), newFlow("dev-1")); err != nil {
			t.Fatalf("flow %d refused: %v", i, err)
		}
	}
	if _, _, err := r.Add(context.Background(), newFlow("dev-1")); !errors.Is(err, ErrTooManyFlows) {
		t.Fatalf("per-device cap not enforced: %v", err)
	}
	// A different device is unaffected by another's cap.
	if _, _, err := r.Add(context.Background(), newFlow("dev-2")); err != nil {
		t.Fatalf("second device blocked by the first: %v", err)
	}
}

func TestGlobalCap(t *testing.T) {
	r := NewRegistry()
	added := 0
	for i := 0; added < MaxFlowsTotal; i++ {
		dev := "dev-" + string(rune('a'+i))
		for j := 0; j < MaxFlowsPerDevice && added < MaxFlowsTotal; j++ {
			if _, _, err := r.Add(context.Background(), newFlow(dev)); err != nil {
				t.Fatalf("unexpected refusal at %d: %v", added, err)
			}
			added++
		}
	}
	if _, _, err := r.Add(context.Background(), newFlow("dev-overflow")); !errors.Is(err, ErrTooManyFlows) {
		t.Fatalf("global cap not enforced: %v", err)
	}
}

// An expired flow must disappear on its own; nothing else prompts a sweep.
func TestExpirySweep(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	r.now = func() time.Time { return now }

	f, ctx, err := r.Add(context.Background(), newFlow("dev-1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Len() != 1 {
		t.Fatalf("len = %d", r.Len())
	}
	now = now.Add(DefaultTTL + time.Minute)
	if r.Len() != 0 {
		t.Fatalf("expired flow survived the sweep: %d", r.Len())
	}
	if _, err := r.Get(f.ID, "dev-1"); !errors.Is(err, ErrFlowNotFound) {
		t.Error("expired flow still resolvable")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("expired flow's context still live")
	}
}

// A disconnecting device must not leave a flow polling on behalf of nobody.
func TestCancelDevice(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < MaxFlowsPerDevice; i++ {
		if _, _, err := r.Add(context.Background(), newFlow("dev-1")); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := r.Add(context.Background(), newFlow("dev-2")); err != nil {
		t.Fatal(err)
	}
	if n := r.CancelDevice("dev-1"); n != MaxFlowsPerDevice {
		t.Fatalf("cancelled %d, want %d", n, MaxFlowsPerDevice)
	}
	if r.Len() != 1 {
		t.Fatalf("other device's flow was collateral: %d left", r.Len())
	}
}

func TestAddRequiresOwningDevice(t *testing.T) {
	r := NewRegistry()
	if _, _, err := r.Add(context.Background(), &Flow{ProviderID: "kilo"}); err == nil {
		t.Fatal("accepted a flow with no owner")
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	r := NewRegistry()
	f, _, err := r.Add(context.Background(), newFlow("dev-1"))
	if err != nil {
		t.Fatal(err)
	}
	r.Finish(f.ID)
	r.Finish(f.ID) // must not panic on a double close
	if r.Len() != 0 {
		t.Fatalf("len = %d", r.Len())
	}
}
