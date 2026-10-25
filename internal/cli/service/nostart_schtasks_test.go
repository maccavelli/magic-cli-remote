package service

import (
	"regexp"
	"strings"
	"testing"
)

// TestNoStartDoesNotStartTheWindowsTask is MADR 0201 D22 (0073 §6.2):
// setupSchtasks ignored --no-start and ran the task. Under --no-start the task
// is registered and nothing is started.
func TestNoStartDoesNotStartTheWindowsTask(t *testing.T) {
	opts := windowsOpts()
	opts.NoStart = true
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	s.xml["mcremote"] = strings.Replace(body, opts.Binary, `C:\old\mcremote.exe`, 1)
	var calls []string
	withSchtasks(t, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "/run ") {
			t.Errorf("--no-start ran schtasks %s", c)
		}
	}
	if len(s.registered) != 1 {
		t.Errorf("--no-start registered %d definitions, want 1", len(s.registered))
	}
	if res.Started {
		t.Error("Started = true under --no-start")
	}
}

// settingsEnabled matches the task's own switch in a rendered definition.
var settingsEnabled = regexp.MustCompile(`(<RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>\s*)<Enabled>true</Enabled>`)

// stoppedCopy is body as a stopped task stores it: the switch off, and, when
// changed, on an older binary, so the definition must be written again.
func stoppedCopy(t *testing.T, body, binary string, changed bool) string {
	t.Helper()
	if !settingsEnabled.MatchString(body) {
		t.Fatal("rendered definition has no Settings Enabled element to disable; the fixture would test nothing")
	}
	stored := settingsEnabled.ReplaceAllString(body, "${1}<Enabled>false</Enabled>")
	if changed {
		stored = strings.Replace(stored, binary, `C:\old\mcremote.exe`, 1)
	}
	return stored
}

// TestNoStartKeepsAStoppedWindowsTaskStopped is MADR 0201 D22 as amended
// (PLAN Deviation 8). Stopping the task disables it, so that its one-minute
// watchdog trigger cannot undo the stop. --force --no-start used to
// re-register the definition enabled, and on a real host the watchdog then
// started the daemon within a minute. A stopped task must stay registered
// disabled (MADR 0207 D31).
func TestNoStartKeepsAStoppedWindowsTaskStopped(t *testing.T) {
	opts := windowsOpts()
	opts.NoStart = true
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	s := withTaskScheduler(t)
	s.xml["mcremote"] = stoppedCopy(t, body, opts.Binary, true)
	var calls []string
	withSchtasks(t, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	if res.Enabled {
		t.Error("Result.Enabled = true for a task registered disabled; setup would print \"Enabled: yes\"")
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "/run ") || strings.Contains(c, "/enable") {
			t.Errorf("--no-start on a stopped task ran schtasks %s", c)
		}
	}
	if len(s.registered) != 1 {
		t.Fatalf("the changed definition was registered %d times, want once", len(s.registered))
	}
	if definitionEnabled(s.registered[0]) {
		t.Error("--no-start registered a stopped task enabled: its watchdog trigger would start the daemon")
	}
}

// TestAStoppedUnchangedTaskNeedsNoForce is MADR 0207 D31: the library's
// comparison does not see the task's switch, so a stopped task whose
// definition is current reads as unchanged. Nothing is registered, and no
// --force is asked for, with --no-start or without.
func TestAStoppedUnchangedTaskNeedsNoForce(t *testing.T) {
	for _, noStart := range []bool{true, false} {
		t.Run(map[bool]string{true: "--no-start", false: "start"}[noStart], func(t *testing.T) {
			opts := windowsOpts()
			opts.Force, opts.NoStart = false, noStart
			body, err := renderTaskXML(opts, `CORP\dev`)
			if err != nil {
				t.Fatal(err)
			}
			s := withTaskScheduler(t)
			s.xml["mcremote"] = stoppedCopy(t, body, opts.Binary, false)
			withSchtasks(t, func(...string) (string, error) { return "", nil })
			res, err := setupSchtasks(opts, body, Result{})
			if err != nil {
				t.Fatalf("setupSchtasks on a stopped, current task: %v", err)
			}
			if !res.Unchanged || len(s.registered) != 0 {
				t.Errorf("res = %+v with %d registrations, want unchanged and none", res, len(s.registered))
			}
		})
	}
}

// TestNoStartReadsTheSwitchFromTaskScheduler is MADR 0207 D31: "stopped" is
// the switch Task Scheduler reports, not the stored definition's text. A task
// reported disabled is kept disabled though its definition reads enabled; one
// reported enabled is registered enabled though its definition reads disabled.
func TestNoStartReadsTheSwitchFromTaskScheduler(t *testing.T) {
	opts := windowsOpts()
	opts.NoStart = true
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(body, opts.Binary, `C:\old\mcremote.exe`, 1)

	t.Run("reported disabled", func(t *testing.T) {
		s := withTaskScheduler(t)
		s.xml["mcremote"] = changed
		s.switchOff["mcremote"] = true
		withSchtasks(t, func(...string) (string, error) { return "", nil })
		if _, err := setupSchtasks(opts, body, Result{}); err != nil {
			t.Fatal(err)
		}
		if len(s.registered) != 1 || definitionEnabled(s.registered[0]) {
			t.Error("a task Task Scheduler reports disabled was registered enabled")
		}
	})
	t.Run("reported enabled", func(t *testing.T) {
		s := withTaskScheduler(t)
		s.xml["mcremote"] = changed
		withSchtasks(t, func(...string) (string, error) { return "", nil })
		if _, err := setupSchtasks(opts, body, Result{}); err != nil {
			t.Fatal(err)
		}
		if len(s.registered) != 1 || !definitionEnabled(s.registered[0]) {
			t.Error("a task Task Scheduler reports enabled was registered disabled")
		}
	})
}
