package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// withSchtasks substitutes the schtasks runner so the Windows branch is
// exercised on any host, the same way OverrideRunLaunchctl drives the launchd
// path off Darwin. Only the enable, run and end that remain go through it
// (MADR 0207 D30).
func withSchtasks(t *testing.T, fn func(args ...string) (string, error)) {
	t.Helper()
	prev := runSchtasks
	runSchtasks = fn
	t.Cleanup(func() { runSchtasks = prev })
}

// fakeTaskScheduler stands in for Task Scheduler behind newTaskClient, so the
// Windows branch runs on any host (MADR 0207 D17). Tasks are kept by name as
// their stored definitions. A definition's Settings/Enabled is the task's
// switch, as Task Scheduler keeps it, and Register sets it from the
// definition.
type fakeTaskScheduler struct {
	xml     map[string]string
	running map[string]bool
	// switchOff turns a task's switch off apart from its stored definition,
	// so a test can tell which of the two the code reads (D31). Register
	// clears it, as it sets the switch.
	switchOff map[string]bool
	statusErr error
	// calls are the client calls made, as "<method> <task>", in order.
	calls []string
	// registered are the definitions handed to Register, in order.
	registered []string
	// schtasks are the schtasks.exe commands run, as their joined arguments.
	schtasks []string
}

// withTaskScheduler makes newTaskClient open tasks in a new fake, and returns
// it. It also records schtasks.exe, which succeeds, so no test reaches the
// real one: a regression on a Windows host would otherwise enable, run or end
// the host's real task. A test that needs another answer calls withSchtasks
// after it.
func withTaskScheduler(t *testing.T) *fakeTaskScheduler {
	t.Helper()
	s := &fakeTaskScheduler{xml: map[string]string{}, running: map[string]bool{}, switchOff: map[string]bool{}}
	prev := newTaskClient
	newTaskClient = func(name string) (taskClient, error) { return fakeTaskClient{s: s, name: name}, nil }
	t.Cleanup(func() { newTaskClient = prev })
	withSchtasks(t, func(args ...string) (string, error) {
		s.schtasks = append(s.schtasks, strings.Join(args, " "))
		return "", nil
	})
	return s
}

// called reports how many calls of method the fake received.
func (s *fakeTaskScheduler) called(method string) int {
	n := 0
	for _, c := range s.calls {
		if strings.HasPrefix(c, method+" ") {
			n++
		}
	}
	return n
}

type fakeTaskClient struct {
	s    *fakeTaskScheduler
	name string
}

func (c fakeTaskClient) Status(context.Context, string) (taskscheduler.Status, error) {
	c.s.calls = append(c.s.calls, "status "+c.name)
	if c.s.statusErr != nil {
		return taskscheduler.Status{}, c.s.statusErr
	}
	body, ok := c.s.xml[c.name]
	if !ok {
		return taskscheduler.Status{}, nil
	}
	disabled := c.s.switchOff[c.name] || !definitionEnabled(body)
	return taskscheduler.Status{Installed: true, Disabled: disabled, Enabled: !disabled, Running: c.s.running[c.name]}, nil
}

func (c fakeTaskClient) Definition(context.Context, string) (string, error) {
	c.s.calls = append(c.s.calls, "definition "+c.name)
	body, ok := c.s.xml[c.name]
	if !ok {
		return "", fmt.Errorf("task %s: %w", c.name, libservice.ErrNotInstalled)
	}
	return body, nil
}

func (c fakeTaskClient) Register(_ context.Context, _, definition string) error {
	c.s.calls = append(c.s.calls, "register "+c.name)
	c.s.xml[c.name] = definition
	c.s.registered = append(c.s.registered, definition)
	delete(c.s.switchOff, c.name)
	return nil
}

func (c fakeTaskClient) Delete(context.Context, string) error {
	c.s.calls = append(c.s.calls, "delete "+c.name)
	delete(c.s.xml, c.name)
	return nil
}

// definitionEnabled reports a definition's own switch, Settings/Enabled. An
// unreadable definition counts as enabled, as Task Scheduler's default is.
func definitionEnabled(body string) bool {
	f, err := taskFieldsFromXML(body)
	return err != nil || f.Enabled
}

func windowsOpts() Options {
	return Options{
		Product:          "mcremote",
		UnitName:         "mcremote",
		Binary:           `C:\Users\dev\AppData\Local\Programs\mcremote\mcremote.exe`,
		ConfigPath:       `C:\Users\dev\AppData\Roaming\mcremote\config.yaml`,
		WorkingDirectory: `C:\Users\dev`,
		Force:            true,
	}
}

// TestRenderTaskXMLIsUnelevated is the load-bearing assertion of MADR 0116
// D12: setup-service must install and run WITHOUT elevation. If either of
// these two values changes, Windows acquires the project's first sudo-
// equivalent, and this test must fail before that ships.
func TestRenderTaskXMLIsUnelevated(t *testing.T) {
	body, err := renderTaskXML(windowsOpts(), `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Error("task XML lacks RunLevel LeastPrivilege — it would request elevation")
	}
	if !strings.Contains(body, "<LogonType>InteractiveToken</LogonType>") {
		t.Error("task XML lacks LogonType InteractiveToken — it would not run as the user")
	}
	if strings.Contains(body, "HighestAvailable") {
		t.Error("task XML requests HighestAvailable — that is elevation")
	}
	if strings.Contains(body, "S-1-5-18") || strings.Contains(body, "LocalSystem") {
		t.Error("task XML names a system principal; D12 selects a per-user task")
	}
}

// TestRenderTaskXMLCarriesServeArgs proves the task runs the same argv the
// systemd unit's ExecStart does.
func TestRenderTaskXMLCarriesServeArgs(t *testing.T) {
	opts := windowsOpts()
	opts.LogLevel = "debug"
	opts.ListenPort = 7531
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		opts.Binary,
		"serve",
		"--config",
		"--log-level debug",
		"--listen-port 7531",
		"<WorkingDirectory>" + opts.WorkingDirectory,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task XML missing %q", want)
		}
	}
	// No execution time limit: a daemon the scheduler kills is not a daemon.
	if !strings.Contains(body, "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>") {
		t.Error("task XML has an execution time limit")
	}
}

// TestRenderTaskXMLDeclaresUTF16: the rendered declaration names UTF-16, the
// encoding Task Scheduler keeps a definition in, and never UTF-8, which it
// refused (MADR 0116 F24).
func TestRenderTaskXMLDeclaresUTF16(t *testing.T) {
	body, err := renderTaskXML(windowsOpts(), `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body, `<?xml version="1.0" encoding="UTF-16"?>`) {
		t.Errorf("rendered declaration does not name UTF-16: %q", body[:min(len(body), 60)])
	}
	if strings.Contains(body, `encoding="UTF-8"`) {
		t.Error("rendered task XML declares UTF-8")
	}
}

// TestServeArgsQuotesSpaces proves a path with a space survives as one
// argument — Windows home directories routinely contain them.
func TestServeArgsQuotesSpaces(t *testing.T) {
	opts := windowsOpts()
	opts.ConfigPath = `C:\Users\Dev User\config.yaml`
	args := serveArgs(opts)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `"C:\Users\Dev User\config.yaml"`) {
		t.Errorf("args = %q, want the spaced path quoted", joined)
	}
}

// TestServeArgsEndWithTheDetachFlag: the task, and only the task, tells the
// daemon to detach from the console Windows gives it (MADR 0159 D7).
func TestServeArgsEndWithTheDetachFlag(t *testing.T) {
	args := serveArgs(windowsOpts())
	if len(args) == 0 || args[len(args)-1] != "--"+DetachConsoleFlag {
		t.Errorf("args = %q, want the last to be --%s", args, DetachConsoleFlag)
	}
}

// TestSetupSchtasksRegistersAnAbsentTask: a task Task Scheduler does not know
// is registered once, from exactly the rendered definition, and its stored
// definition is never asked for.
func TestSetupSchtasksRegistersAnAbsentTask(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	withSchtasks(t, func(...string) (string, error) { return "", nil })
	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if res.AlreadyExisted || res.Unchanged {
		t.Errorf("res = %+v, want a new task", res)
	}
	if len(s.registered) != 1 || s.registered[0] != body {
		t.Fatalf("registered %d definitions, want exactly the rendered one", len(s.registered))
	}
	if s.called("definition") != 0 {
		t.Errorf("read the definition of a task that is not registered: %q", s.calls)
	}
}

// TestSetupSchtasksIdempotent pins contract C2 at the service layer: a second
// Setup against an identical registered task registers nothing.
func TestSetupSchtasksIdempotent(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	// The task engine re-formats what it stores; the comparison must survive
	// that or every run would re-register.
	s.xml["mcremote"] = strings.ReplaceAll(body, "\n", "\r\n")
	withSchtasks(t, func(...string) (string, error) { return "", nil })

	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if !res.AlreadyExisted {
		t.Error("AlreadyExisted = false for a registered task")
	}
	if !res.Unchanged {
		t.Error("Unchanged = false for an equivalent task definition")
	}
	if len(s.registered) != 0 {
		t.Errorf("registered %d definitions for an unchanged task, want 0", len(s.registered))
	}
}

// TestSetupSchtasksRegistersWhenChanged is the other half: a differing
// definition is rewritten under --force.
func TestSetupSchtasksRegistersWhenChanged(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	s.xml["mcremote"] = strings.Replace(body, opts.Binary, `C:\old\mcremote.exe`, 1)
	withSchtasks(t, func(...string) (string, error) { return "", nil })
	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if res.Unchanged {
		t.Error("Unchanged = true for a different definition")
	}
	if len(s.registered) != 1 || s.registered[0] != body {
		t.Errorf("registered %d definitions, want the rendered one once", len(s.registered))
	}
}

// TestSetupSchtasksNamesWhatDiffers: a changed task without --force is
// refused, and the error names each path the library's comparison reports, so
// the operator sees what --force would overwrite (MADR 0207 D17).
func TestSetupSchtasksNamesWhatDiffers(t *testing.T) {
	opts := windowsOpts()
	opts.Force = false
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	stored := strings.Replace(body, opts.Binary, `C:\old\mcremote.exe`, 1)
	stored = strings.Replace(stored, "<Count>3</Count>", "<Count>5</Count>", 1)
	diffs, err := taskscheduler.DiffDefinitions(stored, body)
	if err != nil || len(diffs) != 2 {
		t.Fatalf("the library finds %d differences (%v); the fixture expects two", len(diffs), err)
	}
	s := withTaskScheduler(t)
	s.xml["mcremote"] = stored
	_, err = setupSchtasks(opts, body, Result{})
	if err == nil {
		t.Fatal("a changed task was overwritten without --force")
	}
	for _, d := range diffs {
		if !strings.Contains(err.Error(), d.Path) {
			t.Errorf("the refusal does not name %s: %v", d.Path, err)
		}
	}
	if !strings.Contains(err.Error(), "exists with different content") || !strings.Contains(err.Error(), "--force") ||
		len(s.registered) != 0 || len(s.schtasks) != 0 {
		t.Errorf("err = %v, %d registrations, schtasks %q; want the refusal, nothing registered and nothing started",
			err, len(s.registered), s.schtasks)
	}
}

// TestSetupSchtasksPropagatesAFailedRead: a Task Scheduler read that fails is
// an error, never "not registered", which would register over a task setup
// could not see (MADR 0159 F12).
func TestSetupSchtasksPropagatesAFailedRead(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	s.statusErr = fmt.Errorf("task scheduler: %w", libservice.ErrPermission)
	if _, err := setupSchtasks(opts, body, Result{}); !errors.Is(err, libservice.ErrPermission) {
		t.Fatalf("err = %v, want the read's permission error", err)
	}
	if len(s.registered) != 0 || len(s.schtasks) != 0 {
		t.Errorf("registered %d and ran schtasks %q after a failed read; want neither", len(s.registered), s.schtasks)
	}
}

// TestRemoveSchtasksIdempotent proves removing an absent task is not an error,
// and deletes nothing.
func TestRemoveSchtasksIdempotent(t *testing.T) {
	s := withTaskScheduler(t)
	withTaskBackupFile(t, filepath.Join(t.TempDir(), "mcremote-task.prev.xml"))
	res, err := removeSchtasks(windowsOpts())
	if err != nil {
		t.Fatalf("removeSchtasks on an absent task: %v", err)
	}
	if res.Removed || s.called("delete") != 0 {
		t.Errorf("Removed = %v with %d deletes for a task that was not registered", res.Removed, s.called("delete"))
	}
}

// TestRemoveSchtasksDeletesThroughTheLibrary: a registered task is stopped,
// then deleted once through Task Scheduler (MADR 0207 D17).
func TestRemoveSchtasksDeletesThroughTheLibrary(t *testing.T) {
	s := withTaskScheduler(t)
	s.xml["mcremote"] = "<Task/>"
	var schtasks []string
	withSchtasks(t, func(args ...string) (string, error) {
		schtasks = append(schtasks, strings.Join(args, " "))
		return "", nil
	})
	withTaskBackupFile(t, filepath.Join(t.TempDir(), "mcremote-task.prev.xml"))
	res, err := removeSchtasks(windowsOpts())
	if err != nil || !res.Removed {
		t.Fatalf("removeSchtasks: %+v, %v", res, err)
	}
	if s.called("delete") != 1 || len(s.xml) != 0 {
		t.Errorf("calls %q, tasks %v; want one delete", s.calls, s.xml)
	}
	if slices.ContainsFunc(schtasks, func(c string) bool { return strings.HasPrefix(c, "/delete") || strings.HasPrefix(c, "/query") }) {
		t.Errorf("schtasks ran %q; the delete and the query are the library's", schtasks)
	}
}

// withTaskBackupFile points the remover's backup lookup at path, and fails
// the test if the remover asks taskBackupPath instead, which creates the state
// directory (MADR 0210 D9).
func withTaskBackupFile(t *testing.T, path string) {
	t.Helper()
	prevFile, prevPath := taskBackupFile, taskBackupPath
	taskBackupFile = func(string, string) (string, error) { return path, nil }
	taskBackupPath = func(string, string) (string, error) {
		t.Error("the remove looked the backup up through taskBackupPath, which creates the state directory")
		return path, nil
	}
	t.Cleanup(func() { taskBackupFile, taskBackupPath = prevFile, prevPath })
}

// TestRemoveSchtasksDeletesTheBackup is MADR 0210 D9 on Windows: the
// definition a refresh kept goes with the task, and the result names it.
func TestRemoveSchtasksDeletesTheBackup(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "mcremote-task.prev.xml")
	if err := os.WriteFile(backup, []byte("<Task/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	s.xml["mcremote"] = "<Task/>"
	withSchtasks(t, func(...string) (string, error) { return "", nil })
	withTaskBackupFile(t, backup)
	res, err := removeSchtasks(windowsOpts())
	if err != nil || !res.Removed {
		t.Fatalf("removeSchtasks: %+v, %v", res, err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("%s survived the remove: %v", backup, err)
	}
	if res.RemovedBackup != backup {
		t.Errorf("RemovedBackup = %q, want %q", res.RemovedBackup, backup)
	}
}

// TestRemoveSchtasksWithNoBackup: no backup is no error, and nothing is named.
func TestRemoveSchtasksWithNoBackup(t *testing.T) {
	s := withTaskScheduler(t)
	s.xml["mcremote"] = "<Task/>"
	withSchtasks(t, func(...string) (string, error) { return "", nil })
	withTaskBackupFile(t, filepath.Join(t.TempDir(), "mcremote-task.prev.xml"))
	res, err := removeSchtasks(windowsOpts())
	if err != nil || !res.Removed || res.RemovedBackup != "" {
		t.Fatalf("removeSchtasks: %+v, %v; want Removed and no backup named", res, err)
	}
}

// TestRemoveSchtasksAbsentTaskDropsItsBackup: a backup outlives its task when
// the task was deleted by hand; the remove still deletes it.
func TestRemoveSchtasksAbsentTaskDropsItsBackup(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "mcremote-task.prev.xml")
	if err := os.WriteFile(backup, []byte("<Task/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	withTaskScheduler(t)
	withTaskBackupFile(t, backup)
	res, err := removeSchtasks(windowsOpts())
	if err != nil || res.Removed || res.RemovedBackup != backup {
		t.Fatalf("removeSchtasks: %+v, %v; want not Removed, and the backup named", res, err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("%s survived the remove: %v", backup, err)
	}
}

// TestTaskBackupFileCreatesNothing: the remover's lookup resolves the path
// without creating the state directory, which taskBackupPath does for a
// refresh. Off Windows the state root comes from XDG_STATE_HOME; on Windows
// it is the account's real LocalAppData, so the seam test above covers it.
func TestTaskBackupFileCreatesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the state root is the account's LocalAppData; TestRemoveSchtasksDeletesTheBackup covers the seam")
	}
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", state)
	path, err := realTaskBackupFile("mcremote", "mcremote")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, state) || filepath.Base(path) != "mcremote-task.prev.xml" {
		t.Errorf("taskBackupFile = %q, want mcremote-task.prev.xml under %s", path, state)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Errorf("the lookup created %s: %v", state, err)
	}
}

// TestStopDisablesThenEnds pins stop's sequence (MADR 0159 D5): disable first,
// so the watchdog trigger cannot relaunch a daemon that was stopped on purpose,
// then /end only if Task Scheduler reports the task running. /end is still the
// ungraceful termination MADR 0116 D9 recorded.
func TestStopDisablesThenEnds(t *testing.T) {
	for _, tc := range []struct {
		name                string
		registered, running bool
		want                []string
	}{
		{"running", true, true, []string{"/change /tn mcremote /disable", "/end /tn mcremote"}},
		{"ready", true, false, []string{"/change /tn mcremote /disable"}},
		{"absent", false, false, []string{"/change /tn mcremote /disable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := withTaskScheduler(t)
			if tc.registered {
				s.xml["mcremote"] = "<Task/>"
			}
			s.running["mcremote"] = tc.running
			var calls []string
			withSchtasks(t, func(args ...string) (string, error) {
				calls = append(calls, strings.Join(args, " "))
				return "", nil
			})
			if err := stopWindows("mcremote"); err != nil {
				t.Fatal(err)
			}
			if strings.Join(calls, " | ") != strings.Join(tc.want, " | ") {
				t.Errorf("stopWindows issued %q, want %q", calls, tc.want)
			}
		})
	}
}

// TestStartEnablesThenRuns: start undoes a stop's disable before /run, so the
// watchdog is live again (MADR 0159 D5).
func TestStartEnablesThenRuns(t *testing.T) {
	var calls []string
	withSchtasks(t, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	if err := startWindows("mcremote"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/change /tn mcremote /enable", "/run /tn mcremote"}
	if strings.Join(calls, " | ") != strings.Join(want, " | ") {
		t.Errorf("startWindows issued %q, want %q", calls, want)
	}
}

// TestRenderTaskXMLHasTheWatchdogTrigger: the task carries the repeating
// trigger alongside the logon trigger, and the render is byte-stable so setup
// stays idempotent (MADR 0159 D5, D14).
func TestRenderTaskXMLHasTheWatchdogTrigger(t *testing.T) {
	a, err := renderTaskXML(windowsOpts(), `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := renderTaskXML(windowsOpts(), `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("two renders of the same options differ; setup could never report unchanged")
	}
	f, err := taskFieldsFromXML(a)
	if err != nil {
		t.Fatal(err)
	}
	if f.LogonTriggers != 1 {
		t.Errorf("logon triggers = %d, want 1", f.LogonTriggers)
	}
	if len(f.TimeTriggers) != 1 {
		t.Fatalf("time triggers = %d, want 1", len(f.TimeTriggers))
	}
	w := f.TimeTriggers[0]
	if w.StartBoundary != taskWatchdogBoundary || w.Interval != "PT1M" || !w.Enabled || w.StopAtDurationEnd {
		t.Errorf("watchdog trigger = %+v", w)
	}
	if f.MultipleInstancesPolicy != "IgnoreNew" {
		t.Errorf("MultipleInstancesPolicy = %q; anything else lets the watchdog start a second daemon", f.MultipleInstancesPolicy)
	}
}

// TestSetupDispatchesToWindows proves the installOS switch reaches the Windows
// arm, which is how the branch is testable off Windows at all.
func TestSetupDispatchesToWindows(t *testing.T) {
	restore := OverrideInstallOS("windows")
	defer restore()
	// The Windows branch resolves the principal from the process token (MADR
	// 0159 D8); off Windows there is no token to read, so inject it.
	withTaskPrincipal(t, "S-1-5-21-7-7-7-1001", nil, `CORP\dev`, nil)
	s := withTaskScheduler(t)

	opts := windowsOpts()
	opts.PrintOnly = true
	res, err := Setup(opts)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Scope != "windows-task" {
		t.Errorf("Scope = %q, want windows-task", res.Scope)
	}
	if !strings.Contains(res.UnitBody, "LeastPrivilege") {
		t.Error("previewed body is not the task XML")
	}
	if len(s.registered) != 0 {
		t.Error("--print-only registered a task")
	}
}

// withTaskPrincipal injects the token SID and its account lookup, so the
// production principal path runs on any host.
func withTaskPrincipal(t *testing.T, sid string, sidErr error, account string, lookupErr error) {
	t.Helper()
	prevSID, prevAcct := taskPrincipalSID, taskAccountForSID
	taskPrincipalSID = func() (string, error) { return sid, sidErr }
	taskAccountForSID = func(string) (string, error) { return account, lookupErr }
	t.Cleanup(func() { taskPrincipalSID, taskAccountForSID = prevSID, prevAcct })
}

// TestRenderTaskXMLPrincipalIsTheTokenSID is MADR 0159 F11's regression test:
// the principal comes from the process token, so an overridden USERNAME or
// USERDOMAIN no longer changes the task's owner.
func TestRenderTaskXMLPrincipalIsTheTokenSID(t *testing.T) {
	const sid = "S-1-5-21-7-7-7-1001"
	withTaskPrincipal(t, sid, nil, `CORP\dev`, nil)
	t.Setenv("USERNAME", "bogus")
	t.Setenv("USERDOMAIN", "")
	body, err := renderTaskXML(windowsOpts(), currentTaskUser())
	if err != nil {
		t.Fatal(err)
	}
	f, err := taskFieldsFromXML(body)
	if err != nil {
		t.Fatal(err)
	}
	if f.PrincipalUser != sid {
		t.Errorf("principal UserId = %q, want the token SID %q", f.PrincipalUser, sid)
	}
	if f.LogonUser != `CORP\dev` {
		t.Errorf("logon trigger UserId = %q, want the looked-up account", f.LogonUser)
	}
	if strings.Contains(body, "bogus") {
		t.Error("the rendered task carries the overridden USERNAME")
	}
}

// TestRenderTaskXMLRefusesAnUnknownPrincipal: an unreadable token or an
// unresolvable SID is an error, never an empty or guessed UserId.
func TestRenderTaskXMLRefusesAnUnknownPrincipal(t *testing.T) {
	t.Run("token unreadable", func(t *testing.T) {
		withTaskPrincipal(t, "", errors.New("no token"), "", nil)
		if _, err := renderTaskXML(windowsOpts(), currentTaskUser()); err == nil {
			t.Fatal("rendered a task with no principal")
		}
	})
	t.Run("sid unresolvable", func(t *testing.T) {
		withTaskPrincipal(t, "S-1-5-21-7-7-7-1001", nil, "", errors.New("no such account"))
		if _, err := renderTaskXML(windowsOpts(), currentTaskUser()); err == nil {
			t.Fatal("rendered a task whose principal could not be resolved")
		}
	})
}

// TestSetupFailsBeforeTaskSchedulerWithoutAPrincipal: when the principal
// cannot be determined, Setup stops before Task Scheduler is asked anything.
func TestSetupFailsBeforeTaskSchedulerWithoutAPrincipal(t *testing.T) {
	defer OverrideInstallOS("windows")()
	withTaskPrincipal(t, "", errors.New("no token"), "", nil)
	s := withTaskScheduler(t)
	var calls int
	withSchtasks(t, func(args ...string) (string, error) {
		calls++
		return "", nil
	})
	opts := windowsOpts()
	opts.PrintOnly = true
	if _, err := Setup(opts); err == nil {
		t.Fatal("Setup succeeded with no principal")
	}
	if calls != 0 || len(s.calls) != 0 {
		t.Errorf("schtasks ran %d times and Task Scheduler was called %q before the principal error", calls, s.calls)
	}
}

// TestTaskStatusDrivesInstalled: Task Scheduler's own state decides, from one
// read (MADR 0207 D17), and a failed read is an error rather than "not
// installed" (MADR 0159 F12).
func TestTaskStatusDrivesInstalled(t *testing.T) {
	readErr := fmt.Errorf("task scheduler: %w", libservice.ErrPermission)
	for _, tc := range []struct {
		name          string
		registered    bool
		disabled      bool
		err           error
		wantInstalled bool
		wantErr       bool
	}{
		{"registered", true, false, nil, true, false},
		{"disabled", true, true, nil, true, false},
		{"absent", false, false, nil, false, false},
		{"read error", true, false, readErr, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := withTaskScheduler(t)
			if tc.registered {
				s.xml["mcremote"] = "<Task/>"
				s.switchOff["mcremote"] = tc.disabled
			}
			s.statusErr = tc.err
			installed, err := isInstalledWindows("mcremote")
			if (err != nil) != tc.wantErr || installed != tc.wantInstalled {
				t.Errorf("isInstalledWindows = %v, %v; want %v, err=%v", installed, err, tc.wantInstalled, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, libservice.ErrPermission) {
				t.Errorf("the read error was not propagated: %v", err)
			}
			if !slices.Equal(s.calls, []string{"status mcremote"}) {
				t.Errorf("calls %q, want one status read of mcremote", s.calls)
			}
		})
	}
}

// TestOpenTaskRejectsAnUnsafeName: only product-shaped names in the root
// folder are opened.
func TestOpenTaskRejectsAnUnsafeName(t *testing.T) {
	s := withTaskScheduler(t)
	for _, name := range []string{"", "a b", "x';calc;'", `a\b`} {
		if _, err := taskStatus(name); err == nil {
			t.Errorf("taskStatus(%q) accepted an unsafe name", name)
		}
	}
	if len(s.calls) != 0 {
		t.Errorf("an unsafe name reached Task Scheduler: %q", s.calls)
	}
}
