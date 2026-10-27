//go:build linux

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// TestSupervisedOptionsAreAccepted: each kind's options are ones
// NewSupervised accepts (MADR 0212 P5, amendment B's V3).
func TestSupervisedOptionsAreAccepted(t *testing.T) {
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			supervisedHost(t, kind)
			o, err := supervisedOptions(kind, "mcremote", HealthOptions{}, "mcremote", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := libservice.NewSupervised(o); err != nil {
				t.Fatalf("NewSupervised refused %s's options: %v", kind, err)
			}
		})
	}
	// What the acceptance guards: s6 kills the daemon itself, and a
	// KillAfter with no Kill is refused.
	supervisedHost(t, KindS6)
	o, err := supervisedOptions(KindS6, "mcremote", HealthOptions{}, "mcremote", "")
	if err != nil {
		t.Fatal(err)
	}
	o.KillAfter = 45 * time.Second
	if _, err := libservice.NewSupervised(o); err == nil || !strings.Contains(err.Error(), "KillAfter") {
		t.Fatalf("s6 with a KillAfter: %v, want the library's refusal", err)
	}
}

// TestNewBackendFollowsTheRecord: on Linux the backend is the recorded
// supervisor's, the library's adapter, which can hand an update off; with
// systemd recorded, or nothing recorded on a systemd host, it is the
// library's systemd backend.
func TestNewBackendFollowsTheRecord(t *testing.T) {
	uninstallHost(t, "linux")
	b, err := NewBackend("mcremote", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(*libservice.Supervised); ok {
		t.Fatal("with nothing recorded on a systemd host, NewBackend gave the supervised adapter")
	}
	for _, kind := range scriptKinds {
		if err := recordBackend("mcremote", kind); err != nil {
			t.Fatal(err)
		}
		b, err = NewBackend("mcremote", "")
		if _, ok := b.(*libservice.Supervised); err != nil || !ok {
			t.Fatalf("with %s recorded: %T, %v; want the supervised adapter", kind, b, err)
		}
		if _, ok := b.(libservice.Detacher); !ok {
			t.Fatalf("with %s recorded: the backend cannot hand off", kind)
		}
	}
	// A bound D17 refuses fails the build, not the update halfway.
	t.Setenv(waitEnv, "5")
	if _, err := NewBackend("mcremote", ""); err == nil || !strings.Contains(err.Error(), waitEnv) {
		t.Fatalf("with %s=5: %v, want a refusal", waitEnv, err)
	}
}

// TestProbeStatusOnRunit: doctor reads the adapter's Installed, Enabled and
// Running, and names the run script and the runit command.
func TestProbeStatusOnRunit(t *testing.T) {
	def := supervisedHost(t, KindRunit)
	if err := recordBackend("mcremote", KindRunit); err != nil {
		t.Fatal(err)
	}
	fakeOutput(t, "sv", "down: "+def.Dir+": 3s")
	st := ProbeStatus("mcremote")
	if st.Kind != KindRunit || st.PlistOrUnit != def.Path || !st.PlistPresent || !st.Enabled || st.Active {
		t.Fatalf("status %+v, want runit, installed, enabled, not running", st)
	}
	if !strings.Contains(st.Hint, "sv up "+def.Dir) {
		t.Fatalf("hint %q, want the runit start command", st.Hint)
	}
	b, err := NewBackend("mcremote", "")
	if err != nil {
		t.Fatal(err)
	}
	if running, err := b.Running(context.Background(), "mcremote"); err != nil || running {
		t.Fatalf("Running = %v, %v; want false", running, err)
	}
}
