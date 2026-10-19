//go:build unix

package service

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fakeStub is one shell stub that stands in for every service-manager binary.
// It appends "<name> <args>" to "$FAKE_CFG/calls", then exits with the status
// in "$FAKE_CFG/<name> <args>", or 0, using shell builtins only, since PATH
// holds nothing else. It is written and warmed once per test, and each name is
// a symlink to it, so macOS scans one new file rather than one per probe (MADR
// 0193).
func fakeStub(t *testing.T) string {
	t.Helper()
	return testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "stub"),
		`echo "${0##*/} $*" >> "$FAKE_CFG/calls"
f="$FAKE_CFG/${0##*/} $*"
if [ -f "$f" ]; then read code < "$f"; exit "$code"; fi
exit 0`)
}

// fakeCalls returns the calls the fakes recorded, in order.
func fakeCalls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("FAKE_CFG"), "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// fakeHost puts the named fakes on an otherwise empty PATH. Each exits with
// the status given for its full argument list, or 0, so a test says exactly
// which probes succeed (MADR 0054 PLAN S5a).
func fakeHost(t *testing.T, stub string, fakes map[string]map[string]int) {
	t.Helper()
	dir, cfg := t.TempDir(), t.TempDir()
	for name, exits := range fakes {
		if err := os.Symlink(stub, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		for args, code := range exits {
			if err := os.WriteFile(filepath.Join(cfg, name+" "+args), []byte(strconv.Itoa(code)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_CFG", cfg)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

func TestDetectLinuxFollowsInstallShOrder(t *testing.T) {
	ok, fail := map[string]int{}, 1
	cases := []struct {
		name  string
		fakes map[string]map[string]int
		want  Kind
	}{
		{"systemd answers is-system-running", map[string]map[string]int{"systemctl": ok}, KindSystemdUser},
		{"degraded systemd answers show-environment", map[string]map[string]int{
			"systemctl": {"--user is-system-running": fail}}, KindSystemdUser},
		{"systemd unreachable", map[string]map[string]int{
			"systemctl": {"--user is-system-running": fail, "--user show-environment": fail}}, KindSystemdBroken},
		{"broken systemd does not fall through to runit", map[string]map[string]int{
			"systemctl": {"--user is-system-running": fail, "--user show-environment": fail},
			"runsvdir":  ok, "sv": ok}, KindSystemdBroken},
		{"systemd wins over runit", map[string]map[string]int{"systemctl": ok, "runsvdir": ok, "sv": ok}, KindSystemdUser},
		{"runit", map[string]map[string]int{"runsvdir": ok, "sv": ok}, KindRunit},
		{"runsvdir without sv", map[string]map[string]int{"runsvdir": ok}, KindNone},
		{"s6", map[string]map[string]int{"s6-svscan": ok, "s6-svc": ok, "s6-svstat": ok}, KindS6},
		{"s6 without s6-svstat", map[string]map[string]int{"s6-svscan": ok, "s6-svc": ok}, KindNone},
		{"user OpenRC", map[string]map[string]int{"rc-service": ok}, KindOpenRCUser},
		{"system OpenRC", map[string]map[string]int{"rc-service": {"--user --help": fail}}, KindOpenRCSystem},
		{"nothing", map[string]map[string]int{}, KindNone},
	}
	stub := fakeStub(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHost(t, stub, tc.fakes)
			if got := detectLinux(); got != tc.want {
				t.Fatalf("detectLinux() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestDetectLinuxDerivesTheRuntimeDir is the one deliberate difference from
// install.sh: with XDG_RUNTIME_DIR unset, the user's runtime directory is
// derived, as every systemctl call here does, so a cron or `su -` session is
// not called broken. Without that directory, systemd is broken.
func TestDetectLinuxDerivesTheRuntimeDir(t *testing.T) {
	fakeHost(t, fakeStub(t), map[string]map[string]int{"systemctl": {}})
	t.Setenv("XDG_RUNTIME_DIR", "")
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	prev := userRuntimeRoot
	userRuntimeRoot = root
	t.Cleanup(func() { userRuntimeRoot = prev })

	if got := detectLinux(); got != KindSystemdBroken {
		t.Fatalf("with no runtime dir, detectLinux() = %s, want %s", got, KindSystemdBroken)
	}
	if err := os.Mkdir(filepath.Join(root, u.Uid), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := detectLinux(); got != KindSystemdUser {
		t.Fatalf("with a derivable runtime dir, detectLinux() = %s, want %s", got, KindSystemdUser)
	}
}

func TestDetectEnvironment(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name, osrelease, cgroup string
		marker                  bool
		want                    hostEnvironment
	}{
		{"WSL2 by name", "5.15.153.1-microsoft-standard-WSL2", "", false, envWSL2},
		{"WSL1", "4.4.0-19041-Microsoft", "", false, envWSL1},
		{"WSL2 lower-case", "5.10.16.3-microsoft-standard", "", false, envWSL2},
		{"container marker", "6.8.0-generic", "", true, envContainer},
		{"container cgroup", "6.8.0-generic", "0::/docker/abc", false, envContainer},
		{"native", "6.8.0-generic", "0::/init.scope", false, envNative},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prevRel, prevCg, prevMarkers := osReleasePath, pid1CgroupPath, containerMarkers
			t.Cleanup(func() { osReleasePath, pid1CgroupPath, containerMarkers = prevRel, prevCg, prevMarkers })
			osReleasePath = write("osrelease-"+tc.name, tc.osrelease)
			pid1CgroupPath = write("cgroup-"+tc.name, tc.cgroup)
			containerMarkers = []string{filepath.Join(dir, "absent")}
			if tc.marker {
				containerMarkers = []string{write("marker-"+tc.name, "")}
			}
			if got := detectEnvironment(); got != tc.want {
				t.Fatalf("detectEnvironment() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestSetupOnAHostWithNothingToSuperviseWritesNothing: no definition, no
// default config, no records, and success with the advice (MADR 0054
// amendment 4, PLAN S5a).
func TestSetupOnAHostWithNothingToSuperviseWritesNothing(t *testing.T) {
	// Setup checks only the executable bit; the stub is never run here.
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	for _, kind := range []Kind{KindSystemdBroken, KindOpenRCSystem, KindNone} {
		t.Run(string(kind), func(t *testing.T) {
			defer OverrideInstallOS("linux")()
			defer OverrideDetectedKind(kind)()
			calls := 0
			defer OverrideRunSystemctl(func(...string) error { calls++; return nil })()
			record := withUnitRecord(t, "mcremote", "")
			dir := t.TempDir()
			unitDir := filepath.Join(dir, "units")
			cfg := filepath.Join(dir, "config.yaml")
			res, err := Setup(Options{Product: "mcremote", Binary: bin, UnitDir: unitDir, ConfigPath: cfg})
			if err != nil {
				t.Fatalf("Setup on %s: %v, want success with advice", kind, err)
			}
			if !res.NotConfigured || res.Scope != string(kind) || len(res.Advice) == 0 {
				t.Fatalf("res = %+v, want NotConfigured with advice", res)
			}
			for _, p := range []string{unitDir, cfg, record, filepath.Join(filepath.Dir(record), backendRecordName)} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s exists after a not-configured setup (%v)", p, err)
				}
			}
			if calls != 0 {
				t.Errorf("systemctl ran %d times on a host with nothing to supervise", calls)
			}
			var out bytes.Buffer
			PrintSetupResult(&out, res, false, "mcremote")
			if !strings.Contains(out.String(), "service:  not configured") || strings.Contains(out.String(), "ExecStart binary") {
				t.Errorf("printed:\n%s\nwant only the advice", out.String())
			}
		})
	}
}

// TestSetupRecordsTheBackendAndRemoveForgetsIt: setup records the kind it used;
// removing that service forgets it, and removing another instance does not.
func TestSetupRecordsTheBackendAndRemoveForgetsIt(t *testing.T) {
	defer OverrideInstallOS("linux")()
	defer OverrideRunSystemctl(func(...string) error { return nil })()
	record := withUnitRecord(t, "mcremote", "")
	path := filepath.Join(filepath.Dir(record), backendRecordName)
	dir := t.TempDir()
	bin := testexec.WriteShellStub(t, filepath.Join(dir, "mcremote"), "")
	unitDir := filepath.Join(dir, "units")
	setup := func(name string) {
		t.Helper()
		if _, err := Setup(Options{Product: "mcremote", UnitName: name, Binary: bin, UnitDir: unitDir,
			ConfigPath: filepath.Join(dir, "config.yaml"), Force: true, NoEnable: true, NoStart: true, NoLinger: true}); err != nil {
			t.Fatal(err)
		}
	}
	setup("other")
	setup("custom")
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) != string(KindSystemdUser) {
		t.Fatalf("backend record = %q, %v; want %s", b, err, KindSystemdUser)
	}
	if _, err := Remove(Options{Product: "mcremote", UnitName: "other", UnitDir: unitDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removing another instance removed the backend record: %v", err)
	}
	if _, err := Remove(Options{Product: "mcremote", UnitDir: unitDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("removing the recorded service left the backend record: %v", err)
	}
}
