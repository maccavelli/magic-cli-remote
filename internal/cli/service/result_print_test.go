package service

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the result_print golden files")

// printFixtures are one representative Result per Unix scope. Their output is
// pinned by golden files captured BEFORE MADR 0159 P7 taught the printer the
// windows-task scope, so the Windows arm provably changes nothing for systemd
// and launchd (PLAN 0159 C5).
func printFixtures() map[string]Result {
	return map[string]Result{
		"systemd": {
			Binary: "/home/dev/.local/bin/mcremote", UnitPath: "/home/dev/.config/systemd/user/mcremote.service",
			UnitName: "mcremote", Label: "mcremote", Scope: "systemd-user",
			ConfigPath: "/home/dev/.config/mcremote/config.yaml", ConfigCreated: true,
			Enabled: true, Started: true,
		},
		"launchd": {
			Binary: "/Users/dev/.local/bin/mcremote", UnitPath: "/Users/dev/Library/LaunchAgents/com.maccavelli.mcremote.plist",
			UnitName: "mcremote", Label: "com.maccavelli.mcremote", Scope: "launchd-agent",
			ConfigPath: "/Users/dev/.config/mcremote/config.yaml", ConfigCreated: true,
			Enabled: true, Started: true, Domain: "gui/501", LogDir: "/Users/dev/Library/Logs/mcremote",
		},
	}
}

// TestPrintSetupResultUnixUnchanged compares each Unix scope's summary with
// its golden file. Run with -update only to re-capture a deliberate change.
func TestPrintSetupResultUnixUnchanged(t *testing.T) {
	for name, res := range printFixtures() {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintSetupResult(&buf, res, false, "mcremote")
			golden := filepath.Join("testdata", "result_print_"+name+".golden")
			if *updateGolden {
				if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("%s summary changed:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// TestPrintSetupResultWindows is MADR 0159 F2's regression test: a Windows
// install must be described in Task Scheduler terms and never told to run a
// command that does not exist there.
func TestPrintSetupResultWindows(t *testing.T) {
	res := Result{
		Binary: `C:\Users\dev\AppData\Local\Programs\mcremote\mcremote.exe`, UnitPath: `Task Scheduler\mcremote`,
		UnitName: "mcremote", Label: "mcremote", Scope: "windows-task",
		ConfigPath: `C:\Users\dev\AppData\Roaming\mcremote\config.yaml`, ConfigCreated: true,
		Enabled: true, Started: true,
	}
	var buf bytes.Buffer
	PrintSetupResult(&buf, res, false, "mcremote")
	got := buf.String()
	for _, want := range []string{
		`Task Scheduler\mcremote`,
		"per-user Task Scheduler task",
		"schtasks /query /tn mcremote",
		"schtasks /run /tn mcremote",
		"schtasks /change /tn mcremote /disable",
		"schtasks /end /tn mcremote",
		"mcremote setup-service --remove",
		"install.ps1",
		"MADR 0157",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Windows summary lacks %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"systemctl", "journalctl", "loginctl", ".service", "make install", "launchctl"} {
		if strings.Contains(got, bad) {
			t.Errorf("Windows summary names %q, which does not exist on Windows:\n%s", bad, got)
		}
	}
}

// TestNormalizeWindowsRefusesDroppedFlags: on Windows, --unit-name (other than
// the product) and --env used to be accepted and silently ignored (MADR 0159
// F17, F9). They are now errors; the product's own name is still accepted.
func TestNormalizeWindowsRefusesDroppedFlags(t *testing.T) {
	defer OverrideInstallOS("windows")()
	base := Options{Product: "mcremote", PrintOnly: true, Binary: `C:\x\mcremote.exe`}

	named := base
	named.UnitName = "other"
	if _, err := normalize(named); err == nil || !strings.Contains(err.Error(), "--unit-name") {
		t.Errorf("--unit-name other on Windows: err = %v, want a --unit-name refusal", err)
	}
	withEnv := base
	withEnv.ExtraEnviron = []string{"A=b"}
	if _, err := normalize(withEnv); err == nil || !strings.Contains(err.Error(), "--env") {
		t.Errorf("--env on Windows: err = %v, want an --env refusal", err)
	}
	same := base
	same.UnitName = "mcremote"
	if _, err := normalize(same); err != nil && strings.Contains(err.Error(), "--unit-name") {
		t.Errorf("--unit-name equal to the product was refused: %v", err)
	}
}
