package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// setupSchtasks registers a per-user Task Scheduler at-logon task (MADR 0116
// D12). It requires no elevation: the principal runs with an InteractiveToken
// at LeastPrivilege.
//
// Idempotency (contract C2 at the service layer): the rendered XML is compared
// against what is already registered, and /create is issued only when they
// differ, so a second Setup reports Unchanged and performs no writes. This
// mirrors the Linux and macOS paths, which already report res.Unchanged.
func setupSchtasks(opts Options, body string, res Result) (Result, error) {
	name := taskNameFor(opts.Product)
	res.UnitPath = `Task Scheduler\` + name
	res.Scope = "windows-task"
	res.Label = name

	if existing, err := runSchtasks("/query", "/tn", name, "/xml", "ONE"); err == nil {
		res.AlreadyExisted = true
		if sameTaskDefinition(existing, body) {
			res.Unchanged = true
		} else if !opts.Force {
			return res, fmt.Errorf("scheduled task %q exists with different content (pass --force to overwrite)", name)
		}
	}

	if !res.Unchanged {
		tmp, err := os.CreateTemp("", "mc-task-*.xml")
		if err != nil {
			return res, fmt.Errorf("stage task xml: %w", err)
		}
		defer os.Remove(tmp.Name())
		// UTF-16LE with a BOM, never the string as-is: see encodeTaskXML.
		if _, err := tmp.Write(encodeTaskXML(body)); err != nil {
			tmp.Close()
			return res, fmt.Errorf("write task xml: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return res, fmt.Errorf("close task xml: %w", err)
		}
		if out, err := runSchtasks("/create", "/tn", name, "/xml", tmp.Name(), "/f"); err != nil {
			return res, fmt.Errorf("register scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
		}
	}

	res.Enabled = true
	if err := startWindows(opts.Product); err != nil {
		// A task that registers but will not start is worth reporting, but it
		// is registered — the same posture the systemd path takes.
		return res, err
	}
	res.Started = true
	return res, nil
}

// removeSchtasks deletes the scheduled task. A task that is not registered is
// not an error — Remove is idempotent, like its Unix counterparts.
func removeSchtasks(opts Options) (Result, error) {
	name := taskNameFor(opts.Product)
	res := Result{UnitName: name, Label: name, Scope: "windows-task", UnitPath: `Task Scheduler\` + name}
	if _, err := runSchtasks("/query", "/tn", name); err != nil {
		return res, nil
	}
	// Best-effort stop first so the delete does not race a running instance.
	_ = stopWindows(opts.Product)
	if out, err := runSchtasks("/delete", "/tn", name, "/f"); err != nil {
		return res, fmt.Errorf("delete scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	res.Removed = true
	return res, nil
}

// sameTaskDefinition reports whether a registered definition and a freshly
// rendered one describe the same task.
//
// It compares the fields setup controls, not text (MADR 0159 D14). Task
// Scheduler rewrites a definition on registration: it drops elements that hold
// their default value (RunLevel, AllowHardTerminate, Enabled, Hidden,
// RunOnlyIfNetworkAvailable, the trigger's Enabled), stores the principal as a
// SID, and adds URI, IdleSettings and UseUnifiedSchedulingEngine. The previous
// normalised-text comparison therefore never matched a real registered task,
// so setup was never idempotent on Windows (F19, measured against the
// installed v0.17.4). A definition that cannot be parsed is never "the same".
func sameTaskDefinition(existing, want string) bool {
	a, err := taskFieldsFromXML(existing)
	if err != nil {
		return false
	}
	b, err := taskFieldsFromXML(want)
	if err != nil {
		return false
	}
	return a.equal(b)
}

// sameTaskAccount reports whether two principal UserIds name the same account.
// Task Scheduler stores a SID where setup may have written DOMAIN\user, so a
// rendered name and a registered SID are equal only once resolved. It is a
// seam: the default compares case-insensitively, and MADR 0159 P6 renders the
// principal as the SID itself so a registered task compares equal without a
// lookup.
var sameTaskAccount = func(a, b string) bool { return strings.EqualFold(a, b) }

// taskTimeTrigger is the part of a TimeTrigger setup controls.
type taskTimeTrigger struct {
	StartBoundary     string
	Enabled           bool
	Interval          string
	StopAtDurationEnd bool
}

// taskFields is the part of a task definition setup controls, with Task
// Scheduler's defaults applied, so a rendered definition and the registered
// copy of it compare equal.
type taskFields struct {
	LogonTriggers              int
	LogonUser                  string
	LogonEnabled               bool
	TimeTriggers               []taskTimeTrigger
	PrincipalUser              string
	LogonType                  string
	RunLevel                   string
	MultipleInstancesPolicy    string
	DisallowStartIfOnBatteries bool
	StopIfGoingOnBatteries     bool
	AllowHardTerminate         bool
	StartWhenAvailable         bool
	RunOnlyIfNetworkAvailable  bool
	Enabled                    bool
	Hidden                     bool
	ExecutionTimeLimit         string
	RestartInterval            string
	RestartCount               int
	Execs                      int
	Command                    string
	Arguments                  string
	WorkingDirectory           string
}

func (a taskFields) equal(b taskFields) bool {
	if !sameTaskAccount(a.PrincipalUser, b.PrincipalUser) || !sameTaskAccount(a.LogonUser, b.LogonUser) {
		return false
	}
	a.PrincipalUser, b.PrincipalUser, a.LogonUser, b.LogonUser = "", "", "", ""
	if len(a.TimeTriggers) != len(b.TimeTriggers) {
		return false
	}
	for i := range a.TimeTriggers {
		if a.TimeTriggers[i] != b.TimeTriggers[i] {
			return false
		}
	}
	a.TimeTriggers, b.TimeTriggers = nil, nil
	return reflect.DeepEqual(a, b)
}

// taskParse mirrors the registered XML with pointers where an element may be
// omitted, so an absent element can take its schema default.
type taskParse struct {
	Triggers struct {
		Logon []struct {
			Enabled *string `xml:"Enabled"`
			UserID  string  `xml:"UserId"`
		} `xml:"LogonTrigger"`
		Time []struct {
			StartBoundary string  `xml:"StartBoundary"`
			Enabled       *string `xml:"Enabled"`
			Repetition    struct {
				Interval          string  `xml:"Interval"`
				StopAtDurationEnd *string `xml:"StopAtDurationEnd"`
			} `xml:"Repetition"`
		} `xml:"TimeTrigger"`
	} `xml:"Triggers"`
	Principals struct {
		Principal struct {
			UserID    string  `xml:"UserId"`
			LogonType string  `xml:"LogonType"`
			RunLevel  *string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`
	Settings struct {
		MultipleInstancesPolicy    *string `xml:"MultipleInstancesPolicy"`
		DisallowStartIfOnBatteries *string `xml:"DisallowStartIfOnBatteries"`
		StopIfGoingOnBatteries     *string `xml:"StopIfGoingOnBatteries"`
		AllowHardTerminate         *string `xml:"AllowHardTerminate"`
		StartWhenAvailable         *string `xml:"StartWhenAvailable"`
		RunOnlyIfNetworkAvailable  *string `xml:"RunOnlyIfNetworkAvailable"`
		Enabled                    *string `xml:"Enabled"`
		Hidden                     *string `xml:"Hidden"`
		ExecutionTimeLimit         *string `xml:"ExecutionTimeLimit"`
		RestartOnFailure           *struct {
			Interval string `xml:"Interval"`
			Count    int    `xml:"Count"`
		} `xml:"RestartOnFailure"`
	} `xml:"Settings"`
	Actions struct {
		Exec []struct {
			Command          string `xml:"Command"`
			Arguments        string `xml:"Arguments"`
			WorkingDirectory string `xml:"WorkingDirectory"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

// taskFieldsFromXML parses a rendered or exported task definition. The XML
// declaration is skipped: an export read through a pipe is 8-bit text that
// still declares UTF-16 (MADR 0116 F26), which encoding/xml would refuse.
func taskFieldsFromXML(s string) (taskFields, error) {
	i := strings.Index(s, "<Task")
	if i < 0 {
		return taskFields{}, fmt.Errorf("no <Task> element")
	}
	var p taskParse
	if err := xml.Unmarshal([]byte(s[i:]), &p); err != nil {
		return taskFields{}, fmt.Errorf("parse task xml: %w", err)
	}
	str := func(v *string, def string) string {
		if v == nil {
			return def
		}
		return strings.TrimSpace(*v)
	}
	boolean := func(v *string, def bool) bool {
		if v == nil {
			return def
		}
		return strings.EqualFold(strings.TrimSpace(*v), "true")
	}
	// Schema defaults (Task Scheduler 2.0): the values an export omits.
	f := taskFields{
		LogonTriggers:              len(p.Triggers.Logon),
		PrincipalUser:              strings.TrimSpace(p.Principals.Principal.UserID),
		LogonType:                  strings.TrimSpace(p.Principals.Principal.LogonType),
		RunLevel:                   str(p.Principals.Principal.RunLevel, "LeastPrivilege"),
		MultipleInstancesPolicy:    str(p.Settings.MultipleInstancesPolicy, "IgnoreNew"),
		DisallowStartIfOnBatteries: boolean(p.Settings.DisallowStartIfOnBatteries, true),
		StopIfGoingOnBatteries:     boolean(p.Settings.StopIfGoingOnBatteries, true),
		AllowHardTerminate:         boolean(p.Settings.AllowHardTerminate, true),
		StartWhenAvailable:         boolean(p.Settings.StartWhenAvailable, false),
		RunOnlyIfNetworkAvailable:  boolean(p.Settings.RunOnlyIfNetworkAvailable, false),
		Enabled:                    boolean(p.Settings.Enabled, true),
		Hidden:                     boolean(p.Settings.Hidden, false),
		ExecutionTimeLimit:         str(p.Settings.ExecutionTimeLimit, "PT72H"),
		Execs:                      len(p.Actions.Exec),
	}
	if len(p.Triggers.Logon) > 0 {
		f.LogonUser = strings.TrimSpace(p.Triggers.Logon[0].UserID)
		f.LogonEnabled = boolean(p.Triggers.Logon[0].Enabled, true)
	}
	for _, t := range p.Triggers.Time {
		f.TimeTriggers = append(f.TimeTriggers, taskTimeTrigger{
			StartBoundary:     strings.TrimSpace(t.StartBoundary),
			Enabled:           boolean(t.Enabled, true),
			Interval:          strings.TrimSpace(t.Repetition.Interval),
			StopAtDurationEnd: boolean(t.Repetition.StopAtDurationEnd, false),
		})
	}
	if r := p.Settings.RestartOnFailure; r != nil {
		f.RestartInterval, f.RestartCount = strings.TrimSpace(r.Interval), r.Count
	}
	if len(p.Actions.Exec) > 0 {
		e := p.Actions.Exec[0]
		f.Command = strings.TrimSpace(e.Command)
		f.Arguments = strings.TrimSpace(e.Arguments)
		f.WorkingDirectory = strings.TrimSpace(e.WorkingDirectory)
	}
	return f, nil
}
