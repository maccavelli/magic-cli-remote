package service

import (
	"os"
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
	var calls []string
	withSchtasks(t, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "/query" && len(args) > 3 && args[3] == "/xml" {
			return "<Task><Actions><Exec><Command>C:\\old.exe</Command></Exec></Actions></Task>", nil
		}
		return "", nil
	})
	res, err := setupSchtasks(opts, body, Result{})
	if err != nil {
		t.Fatalf("setupSchtasks: %v", err)
	}
	created := false
	for _, c := range calls {
		created = created || strings.HasPrefix(c, "/create ")
		if strings.HasPrefix(c, "/run ") {
			t.Errorf("--no-start ran schtasks %s", c)
		}
	}
	if !created {
		t.Errorf("--no-start did not register the task: %q", calls)
	}
	if res.Started {
		t.Error("Started = true under --no-start")
	}
}

// TestNoStartKeepsAStoppedWindowsTaskStopped is MADR 0201 D22 as amended
// (PLAN Deviation 8). Stopping the task disables it, so that its one-minute
// watchdog trigger cannot undo the stop. --force --no-start used to
// re-register the definition enabled, and on a real host the watchdog then
// started the daemon within a minute. A stopped task must stay registered
// disabled.
func TestNoStartKeepsAStoppedWindowsTaskStopped(t *testing.T) {
	opts := windowsOpts()
	opts.NoStart = true
	body, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	// The registered task: stopped (disabled), and on an older binary, so the
	// definition must be written again.
	settingsEnabled := regexp.MustCompile(`(<RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>\s*)<Enabled>true</Enabled>`)
	if !settingsEnabled.MatchString(body) {
		t.Fatal("rendered definition has no Settings Enabled element to disable; the fixture would test nothing")
	}
	registered := settingsEnabled.ReplaceAllString(body, "${1}<Enabled>false</Enabled>")
	registered = strings.Replace(registered, opts.Binary, `C:\old\mcremote.exe`, 1)

	var calls []string
	var created string
	withSchtasks(t, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		switch {
		case args[0] == "/query" && len(args) > 3 && args[3] == "/xml":
			return registered, nil
		case args[0] == "/create":
			b, err := os.ReadFile(args[4])
			if err != nil {
				t.Fatalf("read the registered xml: %v", err)
			}
			created = decodeUTF16LEWithBOM(t, b)
		}
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
	if created == "" {
		t.Fatalf("the changed definition was not registered: %q", calls)
	}
	f, err := taskFieldsFromXML(created)
	if err != nil {
		t.Fatal(err)
	}
	if f.Enabled {
		t.Error("--no-start registered a stopped task enabled: its watchdog trigger would start the daemon")
	}
}
