package service

import (
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
