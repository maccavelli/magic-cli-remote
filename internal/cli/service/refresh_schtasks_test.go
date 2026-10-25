package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// refreshHarness drives refreshSchtasks through its seams: Task Scheduler (a
// fake behind newTaskClient), the principal, and the backup location.
type refreshHarness struct {
	sched   *fakeTaskScheduler
	creates []string // definitions handed to Register
	backup  string
}

// newRefreshHarness registers export as the mcremote task. An empty export
// registers nothing.
func newRefreshHarness(t *testing.T, export string) *refreshHarness {
	t.Helper()
	h := &refreshHarness{sched: withTaskScheduler(t), backup: filepath.Join(t.TempDir(), "mcremote-task.prev.xml")}
	if export != "" {
		h.sched.xml["mcremote"] = export
	}
	withTaskPrincipal(t, fixtureSID, nil, fixtureAccount, nil)
	prev := taskBackupPath
	taskBackupPath = func(string, string) (string, error) { return h.backup, nil }
	t.Cleanup(func() { taskBackupPath = prev })
	return h
}

// registered returns the definitions refreshSchtasks registered.
func (h *refreshHarness) registered() []string { return h.sched.registered }

// TestRefreshSchtasksAbsentIsNone: nothing registered, nothing to do.
func TestRefreshSchtasksAbsentIsNone(t *testing.T) {
	h := newRefreshHarness(t, "")
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictNone || res.Path != `Task Scheduler\mcremote` {
		t.Errorf("res = %+v, want verdict none at Task Scheduler\\mcremote", res)
	}
	if len(h.registered()) != 0 {
		t.Error("registered a task that did not exist")
	}
}

// TestRefreshSchtasksReadErrorPropagates: a failed Task Scheduler read is an
// error, not "nothing installed" (which would let an update skip
// reconciliation).
func TestRefreshSchtasksReadErrorPropagates(t *testing.T) {
	h := newRefreshHarness(t, "")
	h.sched.statusErr = errors.New("task scheduler: access denied")
	if _, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{}); err == nil {
		t.Fatal("a failed read was reported as success")
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
	if len(h.registered()) != 1 {
		t.Fatalf("issued %d registrations, want 1", len(h.registered()))
	}
	got, err := taskFieldsFromXML(h.registered()[0])
	if err != nil {
		t.Fatal(err)
	}
	was, _ := taskFieldsFromXML(export)
	if len(got.TimeTriggers) != 1 {
		t.Errorf("refreshed task has %d time triggers, want the watchdog", len(got.TimeTriggers))
	}
	if got.Arguments != was.Arguments+detachArg || got.Command != was.Command || got.WorkingDirectory != was.WorkingDirectory {
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

// TestRefreshSchtasksAddsTheDetachFlagToAV0180Task is the v0.18.1 update: the
// task v0.18.0 registered has the watchdog but not the detach flag, so a
// refresh re-registers it with the flag and every other argument kept.
func TestRefreshSchtasksAddsTheDetachFlagToAV0180Task(t *testing.T) {
	export := v0180ShapeExport(t)
	h := newRefreshHarness(t, export)
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictRefreshed || len(h.registered()) != 1 {
		t.Fatalf("res = %+v with %d registrations, want refreshed once", res, len(h.registered()))
	}
	got, err := taskFieldsFromXML(h.registered()[0])
	if err != nil {
		t.Fatal(err)
	}
	was, _ := taskFieldsFromXML(export)
	if got.Arguments != was.Arguments+detachArg {
		t.Errorf("arguments = %q, want %q", got.Arguments, was.Arguments+detachArg)
	}
	if !got.Enabled {
		t.Error("an enabled task was registered disabled")
	}
	if !res.Reloaded {
		t.Error("a re-registered task was not reported reloaded")
	}
}

// disabledExport is export as Task Scheduler exports it once the task is
// disabled, by an update's Stop or by an operator: Settings gains
// <Enabled>false</Enabled>, which an enabled export omits as a default.
func disabledExport(t *testing.T, export string) string {
	t.Helper()
	const anchor = "<StartWhenAvailable>true</StartWhenAvailable>"
	if !strings.Contains(export, anchor) || !definitionEnabled(export) {
		t.Fatalf("the export lacks %q, or is already disabled", anchor)
	}
	out := strings.Replace(export, anchor, anchor+"<Enabled>false</Enabled>", 1)
	if definitionEnabled(out) {
		t.Fatal("the disabled export does not read as disabled")
	}
	return out
}

// TestRefreshSchtasksLeavesADisabledCurrentTaskAlone is what every Windows
// update meets: the updater's Stop disabled the task, which is otherwise
// current. The refresh must not rewrite it (MADR 0054 amendment S10, F2).
func TestRefreshSchtasksLeavesADisabledCurrentTaskAlone(t *testing.T) {
	h := newRefreshHarness(t, disabledExport(t, currentShapeExport(t)))
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictUnchanged || res.Changed || res.Reloaded {
		t.Errorf("res = %+v, want unchanged and not reloaded", res)
	}
	if len(h.registered()) != 0 {
		t.Error("re-registered a task that differed only by being disabled")
	}
	if _, err := os.Stat(h.backup); !errors.Is(err, os.ErrNotExist) {
		t.Error("backed up a task it did not rewrite")
	}
}

// TestRefreshSchtasksKeepsADisabledTaskDisabled: a disabled task that does
// need a new definition is registered disabled, so its watchdog trigger cannot
// start a daemon the operator stopped (MADR 0054 amendment S10, F4).
func TestRefreshSchtasksKeepsADisabledTaskDisabled(t *testing.T) {
	h := newRefreshHarness(t, disabledExport(t, v0180ShapeExport(t)))
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictRefreshed || !res.Changed || !res.Reloaded || len(h.registered()) != 1 {
		t.Fatalf("res = %+v with %d registrations, want refreshed, changed and reloaded once", res, len(h.registered()))
	}
	if definitionEnabled(h.registered()[0]) {
		t.Error("a disabled task was registered enabled")
	}
	if got, err := taskFieldsFromXML(h.registered()[0]); err != nil || !strings.HasSuffix(got.Arguments, detachArg) {
		t.Errorf("the registered task is not the current definition: %+v, %v", got, err)
	}
}

// TestRefreshSchtasksReadsTheSwitchFromTaskScheduler is MADR 0207 D31: whether
// the task is stopped is the switch Task Scheduler reports, not the stored
// definition's text. A task whose switch is off is registered disabled even
// when its stored definition reads enabled.
func TestRefreshSchtasksReadsTheSwitchFromTaskScheduler(t *testing.T) {
	h := newRefreshHarness(t, v0180ShapeExport(t))
	if !definitionEnabled(v0180ShapeExport(t)) {
		t.Fatal("the stored definition must read enabled, or this case tests nothing")
	}
	h.sched.switchOff["mcremote"] = true
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictRefreshed || len(h.registered()) != 1 {
		t.Fatalf("res = %+v with %d registrations, want refreshed once", res, len(h.registered()))
	}
	if definitionEnabled(h.registered()[0]) {
		t.Error("a task Task Scheduler reports disabled was registered enabled")
	}
}

// TestRefreshReceiptRaisesNoReloadWarning feeds the --json receipt of a
// re-registration to the library's exec reconciler, which is what `update`
// runs. A receipt that is changed but not reloaded makes it warn that "the
// next start may run the previous definition", which is never true of Task
// Scheduler (MADR 0054 amendment S10, F3).
func TestRefreshReceiptRaisesNoReloadWarning(t *testing.T) {
	newRefreshHarness(t, v0180ShapeExport(t))
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var receipt bytes.Buffer
	if err := PrintRefreshResult(&receipt, res, "mcremote", true); err != nil {
		t.Fatal(err)
	}
	rec, err := libservice.NewExecReconciler(libservice.ExecOptions{
		Reconcile: []string{"setup-service", "--refresh", "--json"},
		Restore:   []string{"setup-service", "--restore"},
		Runner: libservice.RunnerFunc(func(context.Context, libservice.Command) (libservice.Output, error) {
			return libservice.Output{Stdout: receipt.Bytes()}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rec.Reconcile(context.Background(), "mcremote", `C:\new\mcremote.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed {
		t.Fatalf("the reconciler read %+v, want a change", got)
	}
	for _, w := range got.Warnings.List() {
		if strings.Contains(w, "not reloaded") {
			t.Errorf("the updater would warn: %s", w)
		}
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
	if len(h.registered()) != 0 {
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
	if len(h.registered()) != 0 || res.BackupPath != "" {
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
			if len(h.registered()) != 0 {
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
	if len(h.registered()) != 1 || h.registered()[0] != export {
		t.Fatalf("restore registered %d bodies; want exactly the backup", len(h.registered()))
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
		"--listen-host", "tailscale", "--listen-port", "7531", "--log-level", "debug", "--log-format", "json",
		"--" + DetachConsoleFlag}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("split = %q\nwant  %q", got, want)
	}
	if _, err := splitTaskArgs(`serve --config "unterminated`); err == nil {
		t.Error("an unbalanced quote was accepted")
	}
}
