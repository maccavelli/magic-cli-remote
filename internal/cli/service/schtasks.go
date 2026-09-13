package service

// This file is deliberately NOT build-tagged for Windows. Nothing in the
// Task Scheduler path touches a Windows API: it shells out to schtasks.exe
// through the runSchtasks seam and manipulates XML. Compiling it everywhere is
// what lets the Windows branch be exercised from a Unix development host via
// OverrideInstallOS, the same way OverrideRunLaunchctl drives the launchd path
// off Darwin (MADR 0116 D12).

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"
)

// runSchtasks runs schtasks.exe and returns its combined output. It is a
// variable so tests can drive the Windows branch without a task engine,
// mirroring runLaunchctlCapture and runSystemctlCapture.
var runSchtasks = func(args ...string) (string, error) {
	return runCmdOutput("schtasks", args...)
}

// taskName is the scheduled-task name for a product. Bare product, matching
// the systemd unit name and the launchd label mapping.
func taskName(product string) string { return taskNameFor(product) }

// taskDefinition is the Task Scheduler 2.0 XML this project registers.
//
// Implemented against schtasks.exe rather than the COM Task Scheduler API:
// schtasks is a documented, stable CLI, it needs no COM plumbing in a codebase
// that has none, and it keeps the implementation reviewable. XML rather than
// `/create /tr` because /tr cannot express the working directory, the restart
// policy, or the principal's run level.
type taskDefinition struct {
	XMLName   xml.Name `xml:"Task"`
	Version   string   `xml:"version,attr"`
	Namespace string   `xml:"xmlns,attr"`

	RegistrationInfo struct {
		Description string `xml:"Description"`
		Author      string `xml:"Author"`
	} `xml:"RegistrationInfo"`

	Triggers struct {
		LogonTrigger struct {
			Enabled bool   `xml:"Enabled"`
			UserID  string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`

	Principals struct {
		Principal struct {
			ID        string `xml:"id,attr"`
			UserID    string `xml:"UserId"`
			LogonType string `xml:"LogonType"`
			RunLevel  string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`

	Settings struct {
		MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
		DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
		StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
		AllowHardTerminate         bool   `xml:"AllowHardTerminate"`
		StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
		RunOnlyIfNetworkAvailable  bool   `xml:"RunOnlyIfNetworkAvailable"`
		Enabled                    bool   `xml:"Enabled"`
		Hidden                     bool   `xml:"Hidden"`
		ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
		RestartOnFailure           struct {
			Interval string `xml:"Interval"`
			Count    int    `xml:"Count"`
		} `xml:"RestartOnFailure"`
	} `xml:"Settings"`

	Actions struct {
		Context string `xml:"Context,attr"`
		Exec    struct {
			Command          string `xml:"Command"`
			Arguments        string `xml:"Arguments"`
			WorkingDirectory string `xml:"WorkingDirectory"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

// renderTaskXML builds the task definition for opts.
//
// LogonType InteractiveToken and RunLevel LeastPrivilege are the whole point
// of MADR 0116 D12: this must install and run WITHOUT elevation. A change that
// makes either of them ask for admin is a regression, and the tests assert
// both.
func renderTaskXML(opts Options, user string) (string, error) {
	var t taskDefinition
	t.Version = "1.4"
	t.Namespace = "http://schemas.microsoft.com/windows/2004/02/mit/task"
	t.RegistrationInfo.Description = fmt.Sprintf("%s background service (magic-cli-remote)", opts.Product)
	t.RegistrationInfo.Author = user

	t.Triggers.LogonTrigger.Enabled = true
	t.Triggers.LogonTrigger.UserID = user

	t.Principals.Principal.ID = "Author"
	t.Principals.Principal.UserID = user
	t.Principals.Principal.LogonType = "InteractiveToken"
	t.Principals.Principal.RunLevel = "LeastPrivilege"

	t.Settings.MultipleInstancesPolicy = "IgnoreNew"
	t.Settings.DisallowStartIfOnBatteries = false
	t.Settings.StopIfGoingOnBatteries = false
	t.Settings.AllowHardTerminate = true
	t.Settings.StartWhenAvailable = true
	t.Settings.RunOnlyIfNetworkAvailable = false
	t.Settings.Enabled = true
	t.Settings.Hidden = false
	// PT0S = no execution time limit. A daemon that the scheduler kills after
	// three days is not a daemon.
	t.Settings.ExecutionTimeLimit = "PT0S"
	// The closest analogue to the systemd unit's Restart= that the task engine
	// offers.
	t.Settings.RestartOnFailure.Interval = "PT1M"
	t.Settings.RestartOnFailure.Count = 3

	t.Actions.Context = "Author"
	t.Actions.Exec.Command = opts.Binary
	t.Actions.Exec.Arguments = strings.Join(serveArgs(opts), " ")
	t.Actions.Exec.WorkingDirectory = opts.WorkingDirectory

	body, err := xml.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render task xml: %w", err)
	}
	// The declaration names UTF-16 because the file on disk IS UTF-16: see
	// encodeTaskXML, the only thing that writes it. This used to be
	// xml.Header (encoding="UTF-8") written as UTF-8, under a comment claiming
	// schtasks accepted that "in practice". On Windows build 26100 it does not:
	// "The task XML is malformed ... unable to switch the encoding" (MADR 0116
	// F24, measured). The string stays text so tests and --print-only can read
	// it; only the bytes handed to schtasks are UTF-16.
	return taskXMLDeclaration + string(body) + "\n", nil
}

// taskXMLDeclaration is the declaration renderTaskXML emits. It must name the
// encoding encodeTaskXML actually produces; a mismatch is exactly what
// schtasks rejects.
const taskXMLDeclaration = `<?xml version="1.0" encoding="UTF-16"?>` + "\n"

// encodeTaskXML returns body as UTF-16LE with a byte-order mark, the encoding
// Task Scheduler parses and the one `schtasks /query /xml` exports (MADR 0116
// D24). It is the only code that turns a task definition into file bytes, so
// the declaration and the encoding cannot drift apart again (PLAN C9).
//
// Measured on build 26100 with the rendered definition: UTF-8 with or without
// a BOM under a UTF-8 declaration is "malformed"; UTF-16LE with a BOM under a
// UTF-16 declaration parses.
func encodeTaskXML(body string) []byte {
	units := utf16.Encode([]rune(body))
	out := make([]byte, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[2+2*i:], u)
	}
	return out
}

// serveArgs builds the argv the task runs, in the SAME order and with the same
// conditions as the systemd unit template's ExecStart and the launchd plist,
// so the three renderers cannot describe different services.
//
// Quoting: each value is wrapped when it contains a space, because the task
// engine hands Arguments to the process as one string.
func serveArgs(opts Options) []string {
	args := []string{"serve"}
	add := func(flag, value string) {
		if value == "" {
			return
		}
		args = append(args, flag, quoteArg(value))
	}
	add("--config", opts.ConfigPath)
	add("--data-dir", opts.DataDir)
	add("--listen-host", opts.ListenHost)
	if opts.ListenPort != 0 {
		args = append(args, "--listen-port", fmt.Sprintf("%d", opts.ListenPort))
	}
	add("--log-level", opts.LogLevel)
	add("--log-format", opts.LogFormat)
	return args
}

// quoteArg wraps a value containing spaces so the task engine passes it as one
// argument.
func quoteArg(v string) string {
	if strings.ContainsAny(v, " \t") {
		return `"` + v + `"`
	}
	return v
}

// currentTaskUser returns DOMAIN\user for the calling account.
func currentTaskUser() string {
	domain := os.Getenv("USERDOMAIN")
	user := os.Getenv("USERNAME")
	if domain != "" && user != "" {
		return domain + `\` + user
	}
	return user
}
