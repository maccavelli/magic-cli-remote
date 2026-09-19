package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// taskPathPrefix is how a Task Scheduler definition is named in a
// RefreshResult / UnitRefresh Path. RestoreUnitBackup keys on it.
const taskPathPrefix = `Task Scheduler\`

// refreshSchtasks re-renders the registered Task Scheduler definition from
// this binary's template, preserving the options baked into it (MADR 0159 D1).
//
// It mirrors refreshLaunchd: a definition this package did not write, or
// cannot reproduce, is reported and left alone; an unchanged one is not
// touched; a changed one is backed up, then re-registered. The comparison is
// semantic (D14): Task Scheduler rewrites what it stores, so text never
// matches.
func refreshSchtasks(opts Options, ro RefreshOptions) (RefreshResult, error) {
	name := taskNameFor(opts.Product)
	res := RefreshResult{Verdict: VerdictNone, Path: taskPathPrefix + name}

	_, found, err := taskState(name)
	if err != nil {
		return res, err
	}
	if !found {
		return res, nil
	}
	existing, err := runSchtasks("/query", "/tn", name, "/xml", "ONE")
	if err != nil {
		return res, fmt.Errorf("export scheduled task %q: %w (%s)", name, err, strings.TrimSpace(existing))
	}

	rec, why, ok := recoverTaskOptions(opts.Product, existing)
	if !ok {
		res.Verdict, res.Reason = VerdictKept, why
		return res, nil
	}
	res.Warnings = append(res.Warnings, binaryWarning(rec.Binary)...)

	want, err := renderTaskXML(rec, currentTaskUser())
	if err != nil {
		return res, err
	}
	res.Body = want
	if sameTaskDefinition(existing, want) {
		res.Verdict = VerdictUnchanged
		return res, nil
	}

	res.Verdict, res.Changed = VerdictRefreshed, true
	if ro.PrintOnly {
		return res, nil
	}

	backup, err := taskBackupPath(opts.Product, name)
	if err != nil {
		return res, err
	}
	if err := fsutil.WriteFileAtomic(backup, []byte(existing), fsutil.AtomicOptions{Perm: 0o600}); err != nil {
		return res, fmt.Errorf("back up scheduled task %q to %s: %w", name, backup, err)
	}
	res.BackupPath = backup
	if err := registerTaskXML(name, want); err != nil {
		return res, err
	}
	return res, nil
}

// restoreSchtasksBackup re-registers a definition saved by refreshSchtasks. It
// renders nothing, so it is safe for the binary rolling an update back.
func restoreSchtasksBackup(path, backup string) error {
	name := strings.TrimPrefix(path, taskPathPrefix)
	body, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("restore %s from %s: %w", path, backup, err)
	}
	if err := registerTaskXML(name, string(body)); err != nil {
		return fmt.Errorf("restore %s from %s: %w", path, backup, err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("restored %s, but could not remove %s: %w", path, backup, err)
	}
	return nil
}

// registerTaskXML registers body under name, replacing any existing task.
// The file handed to schtasks is UTF-16LE with a BOM (encodeTaskXML): Task
// Scheduler rejects anything else (MADR 0116 F24).
func registerTaskXML(name, body string) error {
	tmp, err := os.CreateTemp("", "mc-task-*.xml")
	if err != nil {
		return fmt.Errorf("stage task xml: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(encodeTaskXML(body)); err != nil {
		tmp.Close()
		return fmt.Errorf("write task xml: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close task xml: %w", err)
	}
	if out, err := runSchtasks("/create", "/tn", name, "/xml", tmp.Name(), "/f"); err != nil {
		return fmt.Errorf("register scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	return nil
}

// taskBackupPath is where a refresh keeps the previous definition: the
// product's private state directory, which roams with nothing and is readable
// by no other principal.
var taskBackupPath = func(product, name string) (string, error) {
	prod, ok := appdirs.ProductByName(product)
	if !ok {
		return "", fmt.Errorf("unknown product %q", product)
	}
	roots, _, err := appdirs.SystemRoots(prod)
	if err != nil {
		return "", err
	}
	paths, err := appdirs.Resolve(prod, roots, "")
	if err != nil {
		return "", err
	}
	if err := appdirs.EnsurePrivateDir(paths.StateDir); err != nil {
		return "", fmt.Errorf("prepare %s: %w", paths.StateDir, err)
	}
	return filepath.Join(paths.StateDir, name+"-task.prev.xml"), nil
}

// taskManagedDescription is the Description setup writes; a task without it
// was not registered by this package.
func taskManagedDescription(product string) string {
	return fmt.Sprintf("%s background service (magic-cli-remote)", product)
}

// recoverTaskOptions rebuilds the Options a registered task was rendered from.
// It accepts only what renderTaskXML and serveArgs write; anything else means
// the task was edited by hand or written by something else, and the refresh
// keeps it rather than silently dropping that edit.
func recoverTaskOptions(product, existing string) (Options, string, bool) {
	i := strings.Index(existing, "<Task")
	if i < 0 {
		return Options{}, "not a task definition", false
	}
	var p struct {
		RegistrationInfo struct {
			Description string `xml:"Description"`
		} `xml:"RegistrationInfo"`
		Actions struct {
			Exec []struct {
				Command          string `xml:"Command"`
				Arguments        string `xml:"Arguments"`
				WorkingDirectory string `xml:"WorkingDirectory"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	if err := xml.Unmarshal([]byte(existing[i:]), &p); err != nil {
		return Options{}, "unreadable task definition: " + err.Error(), false
	}
	if strings.TrimSpace(p.RegistrationInfo.Description) != taskManagedDescription(product) {
		return Options{}, "not written by " + product + " setup-service (description differs)", false
	}
	if len(p.Actions.Exec) != 1 {
		return Options{}, fmt.Sprintf("has %d actions; setup-service writes exactly one", len(p.Actions.Exec)), false
	}
	e := p.Actions.Exec[0]
	opts := Options{
		Product:          product,
		UnitName:         product,
		Binary:           strings.TrimSpace(e.Command),
		WorkingDirectory: strings.TrimSpace(e.WorkingDirectory),
	}
	args, err := splitTaskArgs(e.Arguments)
	if err != nil {
		return Options{}, "unreadable arguments: " + err.Error(), false
	}
	if len(args) == 0 || args[0] != "serve" {
		return Options{}, "the action does not run serve", false
	}
	for k := 1; k < len(args); k++ {
		flag := args[k]
		if flag == "--"+DetachConsoleFlag {
			// A boolean with no value. serveArgs always writes it, so the
			// recovered options need no field for it.
			continue
		}
		if k+1 >= len(args) {
			return Options{}, "carries an argument this binary did not write: " + flag, false
		}
		value := args[k+1]
		k++
		switch flag {
		case "--config":
			opts.ConfigPath = value
		case "--data-dir":
			opts.DataDir = value
		case "--listen-host":
			opts.ListenHost = value
		case "--listen-port":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Options{}, "unreadable --listen-port " + value, false
			}
			opts.ListenPort = n
		case "--log-level":
			opts.LogLevel = value
		case "--log-format":
			opts.LogFormat = value
		default:
			return Options{}, "carries an argument this binary did not write: " + flag, false
		}
	}
	return opts, "", true
}

// splitTaskArgs splits an Exec Arguments string the way quoteArg wrote it:
// whitespace-separated, with a value containing whitespace wrapped in double
// quotes.
func splitTaskArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inQuote, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, have = !inQuote, true
		case (r == ' ' || r == '\t') && !inQuote:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unbalanced quote in %q", s)
	}
	if have {
		out = append(out, cur.String())
	}
	return out, nil
}
