package service

import (
	"os"
	"path/filepath"
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

// TestSameTaskDefinitionMatchesARealExport is MADR 0159 F19's regression test.
// The registered copy of a task differs textually from what setup rendered
// (defaults dropped, SID principal, URI and IdleSettings added), and the old
// normalised-text comparison therefore never matched on a real host.
func TestSameTaskDefinitionMatchesARealExport(t *testing.T) {
	resolveFixtureAccount(t)
	want, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
	if !sameTaskDefinition(loadTaskExportFixture(t), want) {
		a, _ := taskFieldsFromXML(loadTaskExportFixture(t))
		b, _ := taskFieldsFromXML(want)
		t.Fatalf("a registered export of the rendered task must compare equal\nexport: %+v\nrender: %+v", a, b)
	}
}

// TestSameTaskDefinitionNeedsTheAccountResolved pins the dependency on the
// principal: a rendered DOMAIN\user and a registered SID are only equal once
// resolved. MADR 0159 P6 removes the dependency by rendering the SID.
func TestSameTaskDefinitionNeedsTheAccountResolved(t *testing.T) {
	want, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
	if sameTaskDefinition(loadTaskExportFixture(t), want) {
		t.Fatal("an unresolved account name compared equal to a SID")
	}
}

// TestSameTaskDefinitionDetectsEveryControlledField proves the comparison is
// not vacuous: each field setup controls, changed in the registered copy,
// makes the definitions differ.
func TestSameTaskDefinitionDetectsEveryControlledField(t *testing.T) {
	resolveFixtureAccount(t)
	want, err := renderTaskXML(fixtureOptions(), fixtureAccount)
	if err != nil {
		t.Fatal(err)
	}
	export := loadTaskExportFixture(t)
	cases := map[string][2]string{
		"arguments":         {"serve --config", "serve --data-dir X --config"},
		"command":           {`Programs\mcremote\mcremote.exe`, `Programs\mcremote\other.exe`},
		"working directory": {`<WorkingDirectory>C:\Users\user</WorkingDirectory>`, `<WorkingDirectory>C:\</WorkingDirectory>`},
		"restart count":     {"<Count>3</Count>", "<Count>5</Count>"},
		"multiple instance": {"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "<MultipleInstancesPolicy>Parallel</MultipleInstancesPolicy>"},
		"time limit":        {"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "<ExecutionTimeLimit>PT1H</ExecutionTimeLimit>"},
		"principal":         {fixtureSID, "S-1-5-21-9-9-9-1001"},
		"logon type":        {"<LogonType>InteractiveToken</LogonType>", "<LogonType>Password</LogonType>"},
		"extra trigger":     {"</LogonTrigger>", "</LogonTrigger><TimeTrigger><StartBoundary>2000-01-01T00:00:00</StartBoundary><Repetition><Interval>PT1M</Interval></Repetition></TimeTrigger>"},
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
