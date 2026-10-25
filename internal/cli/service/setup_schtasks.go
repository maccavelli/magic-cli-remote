package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
)

// setupSchtasks registers a per-user Task Scheduler at-logon task (MADR 0116
// D12). It requires no elevation: the principal runs with an InteractiveToken
// at LeastPrivilege.
//
// Idempotency (contract C2 at the service layer): the rendered definition is
// compared with the one Task Scheduler stores, by go-selfupdate-lib's rule,
// and registered only when they differ, so a second Setup reports Unchanged
// and performs no writes (MADR 0207 D17). That rule does not compare the
// task's switch, so a stopped task with an unchanged definition reads as
// unchanged, and needs no --force for that alone (D31).
func setupSchtasks(opts Options, body string, res Result) (Result, error) {
	name := taskNameFor(opts.Product)
	res.UnitPath = `Task Scheduler\` + name
	res.Scope = "windows-task"
	res.Label = name

	task, err := openTask(name)
	if err != nil {
		return res, err
	}
	ctx := context.Background()
	st, err := task.Status(ctx, name)
	if err != nil {
		return res, fmt.Errorf("query scheduled task %q: %w", name, err)
	}
	keptDisabled := false
	if st.Installed {
		res.AlreadyExisted = true
		existing, err := task.Definition(ctx, name)
		if err != nil {
			return res, fmt.Errorf("read scheduled task %q: %w", name, err)
		}
		// --no-start keeps a stopped task stopped. Stop disables the task, so
		// that its watchdog trigger cannot undo the stop; registering the new
		// definition enabled let that trigger start the daemon within a minute
		// (MADR 0201 D22, amended; measured on a real host). Whether it is
		// stopped is the task's switch, as Task Scheduler reports it (MADR 0207
		// D31). Register sets the switch from the definition, so a definition
		// rendered disabled never enables the task, even for a moment.
		if opts.NoStart && st.Disabled {
			disabled, err := withTaskDisabled(body)
			if err != nil {
				return res, err
			}
			body = disabled
			keptDisabled = true
		}
		paths, err := taskDiffPaths(existing, body)
		switch {
		case err != nil && !opts.Force:
			return res, fmt.Errorf("scheduled task %q cannot be compared with this release's definition: %w (pass --force to overwrite)", name, err)
		case err == nil && len(paths) == 0:
			res.Unchanged = true
		case err == nil && !opts.Force:
			return res, fmt.Errorf("scheduled task %q exists with different content: %s (pass --force to overwrite)",
				name, strings.Join(paths, ", "))
		}
	}

	if !res.Unchanged {
		if err := task.Register(ctx, name, body); err != nil {
			return res, fmt.Errorf("register scheduled task %q: %w", name, err)
		}
	}

	// A stopped task kept stopped is registered, but not enabled.
	res.Enabled = !keptDisabled
	// --no-start registers the task and starts nothing (MADR 0201 D22).
	if opts.NoStart {
		return res, nil
	}
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
	task, err := openTask(name)
	if err != nil {
		return res, err
	}
	ctx := context.Background()
	st, err := task.Status(ctx, name)
	if err != nil {
		return res, fmt.Errorf("query scheduled task %q: %w", name, err)
	}
	if st.Installed {
		// Best-effort stop first: a deleted task's running instance outlives
		// the deletion.
		_ = stopWindows(opts.Product)
		if err := task.Delete(ctx, name); err != nil {
			return res, fmt.Errorf("delete scheduled task %q: %w", name, err)
		}
		res.Removed = true
	}
	// The definition a refresh kept, even when the task itself was already
	// gone, looked up without creating the state directory (MADR 0210 D9).
	path, err := taskBackupFile(opts.Product, name)
	if err != nil {
		return res, err
	}
	if res.RemovedBackup, err = removeBackup(path); err != nil {
		return res, err
	}
	return res, nil
}

// withTaskDisabled returns body, a definition renderTaskXML produced, with the
// task itself disabled: the one Enabled element inside Settings. Triggers keep
// their own Enabled. A body without exactly one is refused rather than guessed
// at.
func withTaskDisabled(body string) (string, error) {
	start := strings.Index(body, "<Settings>")
	end := strings.Index(body, "</Settings>")
	if start < 0 || end < start {
		return "", fmt.Errorf("disable task definition: no Settings element")
	}
	settings := body[start:end]
	const on, off = "<Enabled>true</Enabled>", "<Enabled>false</Enabled>"
	if n := strings.Count(settings, on); n != 1 {
		return "", fmt.Errorf("disable task definition: %d enabled elements in Settings, want 1", n)
	}
	return body[:start] + strings.Replace(settings, on, off, 1) + body[end:], nil
}

// taskTimeTrigger is the part of a TimeTrigger setup controls.
type taskTimeTrigger struct {
	StartBoundary     string
	Enabled           bool
	Interval          string
	StopAtDurationEnd bool
}

// taskFields is the part of a task definition setup controls, with Task
// Scheduler's defaults applied. Comparing two definitions is the library's
// (MADR 0207 D17); this package reads fields only where it needs one, such as
// the uninstall's program, and its tests read what a render holds.
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
