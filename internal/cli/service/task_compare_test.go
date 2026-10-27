package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// These tests hold go-selfupdate-lib's comparison of task definitions
// (taskscheduler.DiffDefinitions, MADR 0207 D17) to Magic's own definitions:
// what renderTaskXML writes, and what Task Scheduler stored for it on a real
// host. Setup and refresh decide "unchanged" by that comparison alone.

// fixtureSID and fixtureAccount are the scrubbed principal in
// testdata/task-export-v0174.xml: the live mcremote task on the Windows
// acceptance host, exported with `schtasks /query /tn mcremote /xml ONE`
// (MADR 0159 probe 5), with only the SID, the account and the user paths
// replaced.
const (
	fixtureSID     = "S-1-5-21-1-2-3-1001"
	fixtureAccount = `HOST\user`
)

func loadTaskExportFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "task-export-v0174.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixtureOptions are the options v0.17.4's setup-service rendered for the
// fixture's task.
func fixtureOptions() Options {
	return Options{
		Product:          "mcremote",
		Binary:           `C:\Users\user\AppData\Local\Programs\mcremote\mcremote.exe`,
		ConfigPath:       `C:\Users\user\AppData\Roaming\mcremote\config.yaml`,
		WorkingDirectory: `C:\Users\user`,
	}
}

// renderFixture renders the fixture's options as setup does in production:
// the principal is the token's SID, and the logon trigger names its account,
// the two forms the export stores.
func renderFixture(t *testing.T) string {
	t.Helper()
	withTaskPrincipal(t, fixtureSID, nil, fixtureAccount, nil)
	body, err := renderTaskXML(fixtureOptions(), currentTaskUser())
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// timeTriggerRe matches the watchdog trigger in a rendered definition.
var timeTriggerRe = regexp.MustCompile(`(?s)\s*<TimeTrigger>.*?</TimeTrigger>`)

// renderV0174Shape renders the fixture's options WITHOUT the watchdog trigger
// MADR 0159 P9 added, and without the detach flag: the shape v0.17.4
// rendered, and so the shape the fixture's export was registered from.
func renderV0174Shape(t *testing.T) string {
	t.Helper()
	body := renderFixture(t)
	if !timeTriggerRe.MatchString(body) {
		t.Fatal("the current render has no TimeTrigger to strip")
	}
	if !strings.Contains(body, detachArg+"</Arguments>") {
		t.Fatal("the current render does not end its arguments with the detach flag")
	}
	body = strings.Replace(body, detachArg+"</Arguments>", "</Arguments>", 1)
	return timeTriggerRe.ReplaceAllString(body, "")
}

// detachArg is the argument v0.18.1 appended (MADR 0159 D7). Tasks registered
// by v0.18.0 and earlier do not carry it.
const detachArg = " --" + DetachConsoleFlag

// currentShapeExport is the fixture as Task Scheduler would export a task
// registered from the CURRENT render: the detach flag appended to the
// arguments, and the watchdog trigger added, with its Enabled omitted as an
// export omits a default.
func currentShapeExport(t *testing.T) string {
	t.Helper()
	export := v0180ShapeExport(t)
	if !strings.Contains(export, "</Arguments>") {
		t.Fatal("fixture has no Arguments")
	}
	return strings.Replace(export, "</Arguments>", detachArg+"</Arguments>", 1)
}

// v0180ShapeExport is the fixture as v0.18.0 registered it: the watchdog
// trigger, and no detach flag.
func v0180ShapeExport(t *testing.T) string {
	t.Helper()
	export := loadTaskExportFixture(t)
	if !strings.Contains(export, "</LogonTrigger>") {
		t.Fatal("fixture has no LogonTrigger")
	}
	return strings.Replace(export, "</LogonTrigger>", "</LogonTrigger>"+watchdogExportBlock, 1)
}

// watchdogExportBlock is the watchdog trigger as an export formats it (the
// fixture's \r\r\n line endings and indentation).
const watchdogExportBlock = "\r\r\n    <TimeTrigger>\r\r\n      <StartBoundary>2000-01-01T00:00:00</StartBoundary>\r\r\n      <Repetition>\r\r\n        <Interval>PT1M</Interval>\r\r\n        <StopAtDurationEnd>false</StopAtDurationEnd>\r\r\n      </Repetition>\r\r\n    </TimeTrigger>"

// requireSame fails t with the library's differences when stored and wanted
// do not compare the same.
func requireSame(t *testing.T, stored, wanted, why string) {
	t.Helper()
	diffs, err := taskscheduler.DiffDefinitions(stored, wanted)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("%s, but the library finds %d differences: %+v (err %v)", why, len(diffs), diffs, err)
	}
}

// TestTheLibraryMatchesARealExport is MADR 0159 F19's regression test, kept
// under the library's comparison. The registered copy of a task differs
// textually from what setup rendered (defaults dropped, URI and IdleSettings
// added, \r\r\n line ends), and a text comparison never matched on a real
// host. The fixture was registered by v0.17.4, so it is compared with that
// shape.
func TestTheLibraryMatchesARealExport(t *testing.T) {
	requireSame(t, loadTaskExportFixture(t), renderV0174Shape(t), "a registered export of the rendered task must compare the same")
}

// TestV0174ExportDiffersFromTheCurrentRender: a task registered before the
// watchdog trigger (MADR 0159 D5) is not what setup renders now, which is what
// makes --refresh report "refreshed" for it (P10).
func TestV0174ExportDiffersFromTheCurrentRender(t *testing.T) {
	if same, err := taskscheduler.SameDefinition(loadTaskExportFixture(t), renderFixture(t)); err != nil || same {
		t.Fatalf("a task without the watchdog trigger compared the same as the current render (err %v)", err)
	}
}

// TestTheLibraryDetectsEveryControlledField proves the comparison is not
// vacuous for Magic's task: starting from an export that is the same as the
// current render, each field setup controls, changed in the registered copy,
// makes them differ.
func TestTheLibraryDetectsEveryControlledField(t *testing.T) {
	want := renderFixture(t)
	export := currentShapeExport(t)
	requireSame(t, export, want, "the base export must be the same as the current render, or every case below is vacuous")
	cases := map[string][2]string{
		"arguments":         {"serve --config", "serve --data-dir X --config"},
		"command":           {`Programs\mcremote\mcremote.exe`, `Programs\mcremote\other.exe`},
		"working directory": {`<WorkingDirectory>C:\Users\user</WorkingDirectory>`, `<WorkingDirectory>C:\</WorkingDirectory>`},
		"restart count":     {"<Count>3</Count>", "<Count>5</Count>"},
		"multiple instance": {"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "<MultipleInstancesPolicy>Parallel</MultipleInstancesPolicy>"},
		"time limit":        {"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "<ExecutionTimeLimit>PT1H</ExecutionTimeLimit>"},
		"principal":         {fixtureSID, "S-1-5-21-9-9-9-1001"},
		"logon type":        {"<LogonType>InteractiveToken</LogonType>", "<LogonType>Password</LogonType>"},
		"watchdog interval": {"<Interval>PT1M</Interval>\r\r\n        <StopAtDurationEnd>", "<Interval>PT5M</Interval>\r\r\n        <StopAtDurationEnd>"},
		"watchdog boundary": {"<StartBoundary>2000-01-01T00:00:00</StartBoundary>", "<StartBoundary>2026-01-01T00:00:00</StartBoundary>"},
		"watchdog removed":  {watchdogExportBlock, ""},
		"detach removed":    {detachArg + "</Arguments>", "</Arguments>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(export, c[0]) {
				t.Fatalf("fixture does not contain %q; the case would test nothing", c[0])
			}
			changed := strings.Replace(export, c[0], c[1], 1)
			if same, err := taskscheduler.SameDefinition(changed, want); err != nil || same {
				t.Fatalf("a registered task with a different %s compared the same (err %v)", name, err)
			}
		})
	}
}

// TestTheLibraryIgnoresTheTaskSwitch is MADR 0207 D31: the comparison does not
// see Settings/Enabled, the task's switch, so a stopped task with a current
// definition reads as unchanged. Whether it is stopped is read from
// Task Scheduler's Status instead.
func TestTheLibraryIgnoresTheTaskSwitch(t *testing.T) {
	const anchor = "<StartWhenAvailable>true</StartWhenAvailable>"
	export := currentShapeExport(t)
	if !strings.Contains(export, anchor) {
		t.Fatalf("fixture does not contain %q", anchor)
	}
	stopped := strings.Replace(export, anchor, anchor+"<Enabled>false</Enabled>", 1)
	if definitionEnabled(stopped) {
		t.Fatal("the stopped export does not read as disabled; the case would test nothing")
	}
	requireSame(t, stopped, renderFixture(t), "a stopped task with the current definition must compare the same")
}

// TestUnreadableNeverComparesUnchanged: a stored definition that is not task
// XML never reads as unchanged, so setup refuses it without --force rather
// than trusting it. The library reports text with no Task element as a
// difference and malformed XML as an error (measured at v1.22.0: "" and "not
// xml" give one difference and no error; "<Task><Triggers>" an error);
// taskDiffPaths checks the error before the differences, because
// SameDefinition answers true alongside that error.
func TestUnreadableNeverComparesUnchanged(t *testing.T) {
	want := renderFixture(t)
	for _, bad := range []string{"", "not xml", "<Other/>", "<Task><Triggers>"} {
		paths, err := taskDiffPaths(bad, want)
		if err == nil && len(paths) == 0 {
			t.Errorf("%q compared as unchanged", bad)
		}
	}
	opts := fixtureOptions()
	opts.Force = false
	for _, bad := range []string{"not xml", "<Task><Triggers>"} {
		s := withTaskScheduler(t)
		s.xml["mcremote"] = bad
		_, err := setupSchtasks(opts, want, Result{})
		if err == nil || !strings.Contains(err.Error(), "--force") || len(s.registered) != 0 {
			t.Errorf("setup over %q: err %v, %d registrations; want the --force refusal, and nothing registered",
				bad, err, len(s.registered))
		}
	}
}
