package service

import (
	"strings"
	"testing"
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

// TestTaskStatusRunning pins the /query parsing, including the conservative
// answer for a localised or unexpected status.
func TestTaskStatusRunning(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		want bool
	}{
		{"running", "TaskName: \\mcremote\r\nStatus:  Running\r\n", true},
		{"ready", "TaskName: \\mcremote\r\nStatus:  Ready\r\n", false},
		{"disabled", "Status: Disabled\r\n", false},
		{"localised", "Status: En cours d'exécution\r\n", false},
		{"absent", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskStatusRunning(tc.out); got != tc.want {
				t.Errorf("taskStatusRunning(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

// TestStopMapsToEnd pins that Stop uses /end — and, by the comment it carries,
// that the ungraceful termination is a recorded decision (MADR 0116 D9).
func TestStopMapsToEnd(t *testing.T) {
	var got []string
	withSchtasks(t, func(args ...string) (string, error) {
		got = args
		return "", nil
	})
	if err := stopWindows("mcremote"); err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 || got[0] != "/end" || got[1] != "/tn" || got[2] != "mcremote" {
		t.Errorf("stopWindows issued %v, want /end /tn mcremote", got)
	}
}

// TestSetupDispatchesToWindows proves the installOS switch reaches the Windows
// arm, which is how the branch is testable off Windows at all.
func TestSetupDispatchesToWindows(t *testing.T) {
	restore := OverrideInstallOS("windows")
	defer restore()

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
