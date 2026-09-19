package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

// resolveFixtureAccount stands in for a SID lookup: it maps the fixture's
// account name to its SID, the way Task Scheduler stored it.
func resolveFixtureAccount(t *testing.T) {
	t.Helper()
	prev := sameTaskAccount
	sameTaskAccount = func(a, b string) bool {
		norm := func(s string) string {
			if strings.EqualFold(s, fixtureAccount) {
				return fixtureSID
			}
			return s
		}
		return strings.EqualFold(norm(a), norm(b))
	}
	t.Cleanup(func() { sameTaskAccount = prev })
}

// timeTriggerRe matches the watchdog trigger in a rendered definition.
var timeTriggerRe = regexp.MustCompile(`(?s)\s*<TimeTrigger>.*?</TimeTrigger>`)

// renderV0174Shape renders the fixture's options WITHOUT the watchdog trigger
// MADR 0159 P9 added: the shape v0.17.4 rendered, and so the shape the
// fixture's export was registered from.
func renderV0174Shape(t *testing.T) string {
	t.Helper()
	body, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
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

// TestSameTaskDefinitionMatchesARealExport is MADR 0159 F19's regression test.
// The registered copy of a task differs textually from what setup rendered
// (defaults dropped, SID principal, URI and IdleSettings added), and the old
// normalised-text comparison therefore never matched on a real host. The
// fixture was registered by v0.17.4, so it is compared with that shape.
func TestSameTaskDefinitionMatchesARealExport(t *testing.T) {
	resolveFixtureAccount(t)
	want := renderV0174Shape(t)
	if !sameTaskDefinition(loadTaskExportFixture(t), want) {
		a, _ := taskFieldsFromXML(loadTaskExportFixture(t))
		b, _ := taskFieldsFromXML(want)
		t.Fatalf("a registered export of the rendered task must compare equal\nexport: %+v\nrender: %+v", a, b)
	}
}

// TestV0174ExportDiffersFromTheCurrentRender: a task registered before the
// watchdog trigger (MADR 0159 D5) is not what setup renders now, which is what
// makes --refresh report "refreshed" for it (P10).
func TestV0174ExportDiffersFromTheCurrentRender(t *testing.T) {
	resolveFixtureAccount(t)
	want, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
	if sameTaskDefinition(loadTaskExportFixture(t), want) {
		t.Fatal("a task without the watchdog trigger compared equal to the current render")
	}
}

// TestSameTaskDefinitionNeedsTheAccountResolved pins the dependency on the
// principal: a rendered DOMAIN\user and a registered SID are only equal once
// resolved. MADR 0159 P6 removes the dependency by rendering the SID.
func TestSameTaskDefinitionNeedsTheAccountResolved(t *testing.T) {
	if sameTaskDefinition(loadTaskExportFixture(t), renderV0174Shape(t)) {
		t.Fatal("an unresolved account name compared equal to a SID")
	}
}

// TestSameTaskDefinitionDetectsEveryControlledField proves the comparison is
// not vacuous: starting from an export that EQUALS the current render, each
// field setup controls, changed in the registered copy, makes them differ.
func TestSameTaskDefinitionDetectsEveryControlledField(t *testing.T) {
	resolveFixtureAccount(t)
	want, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
	export := currentShapeExport(t)
	if !sameTaskDefinition(export, want) {
		a, _ := taskFieldsFromXML(export)
		b, _ := taskFieldsFromXML(want)
		t.Fatalf("the base export must equal the current render, or every case below is vacuous\nexport: %+v\nrender: %+v", a, b)
	}
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
		"disabled":          {"<StartWhenAvailable>true</StartWhenAvailable>", "<StartWhenAvailable>true</StartWhenAvailable><Enabled>false</Enabled>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(export, c[0]) {
				t.Fatalf("fixture does not contain %q; the case would test nothing", c[0])
			}
			changed := strings.Replace(export, c[0], c[1], 1)
			if sameTaskDefinition(changed, want) {
				t.Fatalf("a registered task with a different %s compared equal", name)
			}
		})
	}
}

// TestSameTaskDefinitionRejectsUnparseable: a definition that cannot be read
// is never "the same", so setup re-registers rather than trusting it.
func TestSameTaskDefinitionRejectsUnparseable(t *testing.T) {
	want, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "not xml", "<Task><Triggers>"} {
		if sameTaskDefinition(bad, want) {
			t.Errorf("unparseable %q compared equal", bad)
		}
	}
}
