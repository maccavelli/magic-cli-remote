package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnsupervisedAdviceIsInstallShs checks the advice ports install.sh's
// advisories (:854-893): the su story only where it can be true, WSL's own
// advice instead of it, and the root steps for a system-only OpenRC.
func TestUnsupervisedAdviceIsInstallShs(t *testing.T) {
	cases := []struct {
		name      string
		kind      Kind
		env       hostEnvironment
		pid1      string
		want, not []string
	}{
		{"broken systemd, su", KindSystemdBroken, envNative, "systemd",
			[]string{"user bus is unreachable", "entered with 'su'", "machinectl shell"},
			[]string{"PID 1 on this host", "WSL"}},
		{"broken systemd, PID 1 is not systemd", KindSystemdBroken, envContainer, "runit",
			[]string{"user bus is unreachable", "PID 1 on this host is 'runit'"},
			[]string{"entered with 'su'"}},
		{"broken systemd, PID 1 unknown", KindSystemdBroken, envNative, "unknown",
			[]string{"entered with 'su'"}, []string{"PID 1 on this host"}},
		{"broken systemd on WSL2", KindSystemdBroken, envWSL2, "systemd",
			[]string{"WSL2 without systemd", "systemd=true"},
			[]string{"user bus is unreachable", "entered with 'su'"}},
		{"WSL1", KindNone, envWSL1, "init",
			[]string{"WSL1 has no service manager"}, []string{"WSL2 without systemd"}},
		{"none, native", KindNone, envNative, "init",
			[]string{"no supported service manager detected: none", "run it:   /b/mcremote serve", "nohup /b/mcremote serve"},
			[]string{"user bus", "WSL", "OpenRC"}},
		{"system OpenRC", KindOpenRCSystem, envNative, "init",
			[]string{"OpenRC here runs system services only", "/etc/init.d/mcremote", `command="/b/mcremote"`, "rc-update add mcremote default"},
			[]string{"user bus"}},
		{"runit, before S5b", KindRunit, envNative, "runit",
			[]string{"does not manage runit services yet"}, []string{"user bus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(unsupervisedAdvice(tc.kind, tc.env, tc.pid1, "mcremote", "/b/mcremote", "mcremote"), "\n")
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("advice lacks %q:\n%s", w, got)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(got, n) {
					t.Errorf("advice has %q:\n%s", n, got)
				}
			}
		})
	}
}

func TestBackendRecord(t *testing.T) {
	defer OverrideInstallOS("linux")()
	record := withUnitRecord(t, "mcremote", "")
	path := filepath.Join(filepath.Dir(record), backendRecordName)

	if got, err := ResolveBackend("mcremote"); err != nil || got != KindSystemdUser {
		t.Fatalf("with no record, ResolveBackend = %s, %v; want the OS default %s", got, err, KindSystemdUser)
	}
	if err := recordBackend("mcremote", KindLaunchdAgent); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveBackend("mcremote"); err != nil || got != KindLaunchdAgent {
		t.Fatalf("ResolveBackend = %s, %v; want the recorded %s", got, err, KindLaunchdAgent)
	}
	if err := os.WriteFile(path, []byte("none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBackend("mcremote"); err == nil || !strings.Contains(err.Error(), backendRecordName) {
		t.Fatalf("ResolveBackend accepted a record of a kind it does not install: %v", err)
	}
	if err := forgetBackend("mcremote"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the record survived forgetBackend: %v", err)
	}
}
