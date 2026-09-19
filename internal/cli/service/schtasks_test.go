package service

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

// withSchtasks substitutes the schtasks runner so the Windows branch is
// exercised on any host, the same way OverrideRunLaunchctl drives the launchd
// path off Darwin.
func withSchtasks(t *testing.T, fn func(args ...string) (string, error)) {
	t.Helper()
	prev := runSchtasks
	runSchtasks = fn
	t.Cleanup(func() { runSchtasks = prev })
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

// TestSetupSchtasksIdempotent pins contract C2 at the service layer: a second
// Setup against an identical registered task performs no /create.
func TestSetupSchtasksIdempotent(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	// The task engine re-formats what it stores; the comparison must survive
	// that or every run would re-register.
	stored := "\ufeff" + strings.ReplaceAll(body, "\n", "\r\n")

	var creates int
	withSchtasks(t, func(args ...string) (string, error) {
		switch {
		case args[0] == "/query" && len(args) > 3 && args[3] == "/xml":
			return stored, nil
		case args[0] == "/create":
			creates++
			return "", nil
		}
		return "", nil
	})

	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if !res.AlreadyExisted {
		t.Error("AlreadyExisted = false for a registered task")
	}
	if !res.Unchanged {
		t.Error("Unchanged = false for a byte-equivalent task definition")
	}
	if creates != 0 {
		t.Errorf("issued %d /create calls for an unchanged task, want 0", creates)
	}
}

// TestSetupSchtasksRegistersWhenChanged is the other half: a differing
// definition must be rewritten.
func TestSetupSchtasksRegistersWhenChanged(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	var creates int
	withSchtasks(t, func(args ...string) (string, error) {
		switch {
		case args[0] == "/query" && len(args) > 3 && args[3] == "/xml":
			return "<Task><Actions><Exec><Command>C:\\old.exe</Command></Exec></Actions></Task>", nil
		case args[0] == "/create":
			creates++
			return "", nil
		}
		return "", nil
	})
	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if res.Unchanged {
		t.Error("Unchanged = true for a different definition")
	}
	if creates != 1 {
		t.Errorf("issued %d /create calls, want 1", creates)
	}
}

// TestRemoveSchtasksIdempotent proves removing an absent task is not an error.
func TestRemoveSchtasksIdempotent(t *testing.T) {
	withSchtasks(t, func(args ...string) (string, error) {
		return "", errNotRegistered{}
	})
	res, err := removeSchtasks(windowsOpts())
	if err != nil {
		t.Fatalf("removeSchtasks on an absent task: %v", err)
	}
	if res.Removed {
		t.Error("Removed = true for a task that was not registered")
	}
}

type errNotRegistered struct{}

func (errNotRegistered) Error() string { return "ERROR: The system cannot find the file specified." }

// TestStopDisablesThenEnds pins stop's sequence (MADR 0159 D5): disable first,
// so the watchdog trigger cannot relaunch a daemon that was stopped on purpose,
// then /end only if the task is running. /end is still the ungraceful
// termination MADR 0116 D9 recorded.
func TestStopDisablesThenEnds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state int
		found bool
		want  []string
	}{
		{"running", taskStateRunning, true, []string{"/change /tn mcremote /disable", "/end /tn mcremote"}},
		{"ready", 3, true, []string{"/change /tn mcremote /disable"}},
		{"absent", 0, false, []string{"/change /tn mcremote /disable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			withSchtasks(t, func(args ...string) (string, error) {
				calls = append(calls, strings.Join(args, " "))
				return "", nil
			})
			withTaskState(t, func(string) (int, bool, error) { return tc.state, tc.found, nil })
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

	var sawCreate bool
	withSchtasks(t, func(args ...string) (string, error) {
		if args[0] == "/create" {
			sawCreate = true
		}
		if args[0] == "/query" {
			return "", errNotRegistered{}
		}
		return "", nil
	})

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
	if sawCreate {
		t.Error("--print-only issued a /create")
	}
}

// decodeUTF16LEWithBOM is the test's own reading of the task file bytes, kept
// independent of encodeTaskXML so the two cannot share a bug.
func decodeUTF16LEWithBOM(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("task file does not start with the UTF-16LE BOM FF FE: first bytes % X", b[:min(len(b), 4)])
	}
	if len(b)%2 != 0 {
		t.Fatalf("task file has an odd byte count (%d); not UTF-16", len(b))
	}
	units := make([]uint16, (len(b)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2+2*i:])
	}
	return string(utf16.Decode(units))
}

// TestEncodeTaskXMLIsUTF16LEWithBOM is PLAN 0116 row 14 (MADR 0116 D24): the
// bytes are UTF-16LE with a BOM, they decode back to exactly the rendered text,
// and that text declares the encoding the bytes are actually in. The mismatch
// between those last two is what Task Scheduler rejected (F24).
func TestEncodeTaskXMLIsUTF16LEWithBOM(t *testing.T) {
	body, err := renderTaskXML(windowsOpts(), `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body, `<?xml version="1.0" encoding="UTF-16"?>`) {
		t.Fatalf("rendered declaration does not name UTF-16: %q", body[:min(len(body), 60)])
	}
	if strings.Contains(body, `encoding="UTF-8"`) {
		t.Error("rendered task XML still declares UTF-8")
	}
	if got := decodeUTF16LEWithBOM(t, encodeTaskXML(body)); got != body {
		t.Error("encodeTaskXML does not round-trip the rendered text")
	}
	// Non-ASCII must survive: a Windows user or path can carry it.
	const s = `C:\Users\Zoë\AppData\Local\Programs\mcremote\mcremote.exe`
	if got := decodeUTF16LEWithBOM(t, encodeTaskXML(s)); got != s {
		t.Errorf("non-ASCII did not round-trip: %q", got)
	}
}

// TestSetupSchtasksWritesUTF16 is PLAN C9: the file setupSchtasks hands to
// `schtasks /create /xml` is encodeTaskXML's output, not the string. The stub
// reads the staged file while it still exists.
func TestSetupSchtasksWritesUTF16(t *testing.T) {
	opts := windowsOpts()
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	var staged []byte
	withSchtasks(t, func(args ...string) (string, error) {
		if args[0] == "/create" {
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "/xml" {
					b, rerr := os.ReadFile(args[i+1])
					if rerr != nil {
						t.Errorf("read staged task file: %v", rerr)
					}
					staged = b
				}
			}
			return "", nil
		}
		if args[0] == "/query" {
			return "", errNotRegistered{} // nothing registered yet, so /create runs
		}
		return "", nil // /run and anything else succeed
	})
	if _, err := setupSchtasks(opts, body, Result{}); err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if staged == nil {
		t.Fatal("setupSchtasks never issued /create with an /xml file")
	}
	if got := decodeUTF16LEWithBOM(t, staged); got != body {
		t.Error("the staged task file does not decode to the rendered definition")
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

// TestSetupFailsBeforeSchtasksWithoutAPrincipal: when the principal cannot be
// determined, Setup stops before running schtasks at all.
func TestSetupFailsBeforeSchtasksWithoutAPrincipal(t *testing.T) {
	defer OverrideInstallOS("windows")()
	withTaskPrincipal(t, "", errors.New("no token"), "", nil)
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
	if calls != 0 {
		t.Errorf("schtasks ran %d times before the principal error", calls)
	}
}

// withTaskState injects the Get-ScheduledTask probe.
func withTaskState(t *testing.T, fn func(name string) (int, bool, error)) {
	t.Helper()
	prev := taskState
	taskState = fn
	t.Cleanup(func() { taskState = prev })
}

// TestTaskStateDrivesActiveAndInstalled replaces the English-text parser
// (MADR 0159 F12, D9): the numeric state decides, and a probe failure is an
// error rather than "not running".
func TestTaskStateDrivesActiveAndInstalled(t *testing.T) {
	probeErr := errors.New("powershell failed")
	for _, tc := range []struct {
		name          string
		state         int
		found         bool
		err           error
		wantActive    bool
		wantInstalled bool
		wantErr       bool
	}{
		{"running", 4, true, nil, true, true, false},
		{"ready", 3, true, nil, false, true, false},
		{"disabled", 1, true, nil, false, true, false},
		{"absent", 0, false, nil, false, false, false},
		{"probe error", 0, false, probeErr, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var asked string
			withTaskState(t, func(name string) (int, bool, error) {
				asked = name
				return tc.state, tc.found, tc.err
			})
			active, err := isActiveWindows("mcremote")
			if (err != nil) != tc.wantErr || active != tc.wantActive {
				t.Errorf("isActiveWindows = %v, %v; want %v, err=%v", active, err, tc.wantActive, tc.wantErr)
			}
			installed, err := isInstalledWindows("mcremote")
			if (err != nil) != tc.wantErr || installed != tc.wantInstalled {
				t.Errorf("isInstalledWindows = %v, %v; want %v, err=%v", installed, err, tc.wantInstalled, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, probeErr) {
				t.Errorf("the probe error was not propagated: %v", err)
			}
			if asked != "mcremote" {
				t.Errorf("probed task %q, want mcremote", asked)
			}
		})
	}
}

// TestTaskStateRejectsAnUnsafeName: only product-shaped names reach the
// PowerShell command line.
func TestTaskStateRejectsAnUnsafeName(t *testing.T) {
	for _, name := range []string{"", "a b", "x';calc;'", `a\b`} {
		if _, _, err := taskState(name); err == nil {
			t.Errorf("taskState(%q) accepted an unsafe name", name)
		}
	}
}
