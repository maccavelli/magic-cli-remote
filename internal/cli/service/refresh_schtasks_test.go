package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refreshHarness drives refreshSchtasks through its seams: the task-state
// probe, schtasks itself, the principal, and the backup location.
type refreshHarness struct {
	state   int
	found   bool
	stateEr error
	export  string
	creates []string // decoded bodies handed to /create
	backup  string
}

func newRefreshHarness(t *testing.T, export string) *refreshHarness {
	t.Helper()
	h := &refreshHarness{state: taskStateRunning, found: true, export: export,
		backup: filepath.Join(t.TempDir(), "mcremote-task.prev.xml")}
	withTaskState(t, func(string) (int, bool, error) { return h.state, h.found, h.stateEr })
	withTaskPrincipal(t, fixtureSID, nil, fixtureAccount, nil)
	withSchtasks(t, func(args ...string) (string, error) {
		switch {
		case args[0] == "/query" && len(args) > 3 && args[3] == "/xml":
			return h.export, nil
		case args[0] == "/create":
			b, err := os.ReadFile(args[4])
			if err != nil {
				t.Fatalf("read staged task file: %v", err)
			}
			h.creates = append(h.creates, decodeUTF16LEWithBOM(t, b))
			return "", nil
		}
		return "", nil
	})
	prev := taskBackupPath
	taskBackupPath = func(string, string) (string, error) { return h.backup, nil }
	t.Cleanup(func() { taskBackupPath = prev })
	return h
}

// TestRefreshSchtasksAbsentIsNone: nothing registered, nothing to do.
func TestRefreshSchtasksAbsentIsNone(t *testing.T) {
	h := newRefreshHarness(t, "")
	h.found = false
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictNone || res.Path != `Task Scheduler\mcremote` {
		t.Errorf("res = %+v, want verdict none at Task Scheduler\\mcremote", res)
	}
	if len(h.creates) != 0 {
		t.Error("registered a task that did not exist")
	}
}

// TestRefreshSchtasksProbeErrorPropagates: a failed state probe is an error,
// not "nothing installed" (which would let an update skip reconciliation).
func TestRefreshSchtasksProbeErrorPropagates(t *testing.T) {
	h := newRefreshHarness(t, "")
	h.stateEr = errors.New("powershell failed")
	if _, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{}); err == nil {
		t.Fatal("a failed probe was reported as success")
	}
}

// TestRefreshSchtasksRefreshesTheV0174Task is the F1 path: the task v0.17.4
// registered lacks the watchdog trigger, so a refresh re-registers it with the
// current template while keeping every option baked into it, and keeps a
// backup the rollback path can restore.
func TestRefreshSchtasksRefreshesTheV0174Task(t *testing.T) {
	export := loadTaskExportFixture(t)
	h := newRefreshHarness(t, export)
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictRefreshed || !res.Changed {
		t.Fatalf("res = %+v, want refreshed and changed", res)
	}
	if len(h.creates) != 1 {
		t.Fatalf("issued %d /create calls, want 1", len(h.creates))
	}
	got, err := taskFieldsFromXML(h.creates[0])
	if err != nil {
		t.Fatal(err)
	}
	was, _ := taskFieldsFromXML(export)
	if len(got.TimeTriggers) != 1 {
		t.Errorf("refreshed task has %d time triggers, want the watchdog", len(got.TimeTriggers))
	}
	if got.Arguments != was.Arguments || got.Command != was.Command || got.WorkingDirectory != was.WorkingDirectory {
		t.Errorf("options not preserved:\nwas %q %q %q\ngot %q %q %q",
			was.Command, was.Arguments, was.WorkingDirectory, got.Command, got.Arguments, got.WorkingDirectory)
	}
	if got.PrincipalUser != fixtureSID {
		t.Errorf("principal = %q, want the token SID", got.PrincipalUser)
	}
	if res.BackupPath != h.backup {
		t.Errorf("BackupPath = %q, want %q", res.BackupPath, h.backup)
	}
	saved, err := os.ReadFile(h.backup)
	if err != nil {
		t.Fatalf("no backup written: %v", err)
	}
	if string(saved) != export {
		t.Error("the backup is not the definition that was replaced")
	}
}

// TestRefreshSchtasksCurrentTaskIsUnchanged: a task already in the current
// shape is left alone, and the verdict says so (D14 makes this reachable).
func TestRefreshSchtasksCurrentTaskIsUnchanged(t *testing.T) {
	h := newRefreshHarness(t, currentShapeExport(t))
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictUnchanged || res.Changed {
		t.Errorf("res = %+v, want unchanged", res)
	}
	if len(h.creates) != 0 {
		t.Error("re-registered an unchanged task")
	}
}

// TestRefreshSchtasksPrintOnlyWritesNothing: --print-only reports the verdict
// and the body, and neither backs up nor registers.
func TestRefreshSchtasksPrintOnlyWritesNothing(t *testing.T) {
	h := newRefreshHarness(t, loadTaskExportFixture(t))
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{PrintOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictRefreshed || res.Body == "" {
		t.Errorf("res = %+v, want refreshed with a body", res)
	}
	if len(h.creates) != 0 || res.BackupPath != "" {
		t.Error("--print-only registered or backed up")
	}
	if _, err := os.Stat(h.backup); !errors.Is(err, os.ErrNotExist) {
		t.Error("--print-only wrote a backup file")
	}
}

// TestRefreshSchtasksKeepsWhatItDidNotWrite: a foreign task, a hand-added
// argument, or a second action is reported and left alone.
func TestRefreshSchtasksKeepsWhatItDidNotWrite(t *testing.T) {
	export := loadTaskExportFixture(t)
	cases := map[string][2]string{
		"foreign description": {"mcremote background service (magic-cli-remote)", "something else"},
		"unknown argument":    {"serve --config", "serve --verbose yes --config"},
		"not serve":           {"<Arguments>serve ", "<Arguments>pair "},
		"second action":       {"</Exec>", "</Exec><Exec><Command>C:\\x.exe</Command></Exec>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(export, c[0]) {
				t.Fatalf("fixture lacks %q", c[0])
			}
			h := newRefreshHarness(t, strings.Replace(export, c[0], c[1], 1))
			res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Verdict != VerdictKept || res.Reason == "" {
				t.Errorf("res = %+v, want kept with a reason", res)
			}
			if len(h.creates) != 0 {
				t.Error("re-registered a task it did not write")
			}
		})
	}
}

// TestRestoreUnitBackupReRegistersATask is the rollback half of D1: a Task
// Scheduler "path" is re-registered from the backup, which is then removed.
func TestRestoreUnitBackupReRegistersATask(t *testing.T) {
	export := loadTaskExportFixture(t)
	h := newRefreshHarness(t, export)
	if err := os.WriteFile(h.backup, []byte(export), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreUnitBackup(`Task Scheduler\mcremote`, h.backup); err != nil {
		t.Fatal(err)
	}
	if len(h.creates) != 1 || h.creates[0] != export {
		t.Fatalf("restore registered %d bodies; want exactly the backup", len(h.creates))
	}
	if _, err := os.Stat(h.backup); !errors.Is(err, os.ErrNotExist) {
		t.Error("the backup was not removed after a successful restore")
	}
}

// TestSplitTaskArgsRoundTripsServeArgs: whatever serveArgs writes, recovery
// reads back — including a path with a space, which quoteArg wraps.
func TestSplitTaskArgsRoundTripsServeArgs(t *testing.T) {
	opts := windowsOpts()
	opts.ConfigPath = `C:\Users\Dev User\config.yaml`
	opts.DataDir = `C:\Data Dir`
	opts.ListenHost = "tailscale"
	opts.ListenPort = 7531
	opts.LogLevel = "debug"
	opts.LogFormat = "json"
	got, err := splitTaskArgs(strings.Join(serveArgs(opts), " "))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"serve", "--config", opts.ConfigPath, "--data-dir", opts.DataDir,
		"--listen-host", "tailscale", "--listen-port", "7531", "--log-level", "debug", "--log-format", "json"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("split = %q\nwant  %q", got, want)
	}
	if _, err := splitTaskArgs(`serve --config "unterminated`); err == nil {
		t.Error("an unbalanced quote was accepted")
	}
}
