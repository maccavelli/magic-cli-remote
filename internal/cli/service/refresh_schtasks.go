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
// by value, by go-selfupdate-lib's rule (MADR 0207 D17): Task Scheduler
// rewrites what it stores, so text never matches.
func refreshSchtasks(opts Options, ro RefreshOptions) (RefreshResult, error) {
	name := taskNameFor(opts.Product)
	res := RefreshResult{Verdict: VerdictNone, Path: taskPathPrefix + name}

	st, err := taskStatus(name)
	if err != nil {
		return res, err
	}
	if !st.Installed {
		return res, nil
	}
	existing, err := taskDefinitionOf(name)
	if err != nil {
		return res, err
	}

	rec, why, ok := recoverTaskOptions(opts.Product, existing)
	if !ok {
		res.Verdict, res.Reason = VerdictKept, why
		return res, nil
	}

	// The folder move (MADR 0054 D9, PLAN S5g). It writes files, so a
	// preview only says it would happen.
	var mig migration
	moved, inNew := false, samePath(filepath.Dir(rec.Binary), newWindowsDir())
	if inOldLayout(opts.Product, rec.Binary) {
		if ro.PrintOnly {
			res.Warnings = append(res.Warnings, "the task would move to "+newWindowsDir())
		} else {
			var bin string
			bin, mig = planMigration(opts.Product, rec.Binary)
			moved = bin != rec.Binary
			rec.Binary = bin
		}
	}
	if !moved {
		// After a move the refresh child still runs from the old folder,
		// so the definition runs another binary by design.
		res.Warnings = append(res.Warnings, binaryWarning(rec.Binary)...)
	}

	want, err := renderTaskXML(rec, currentTaskUser())
	if err != nil {
		return res, err
	}
	// A disabled task stays disabled: an update's Stop disables the task
	// before it refreshes it, and so does an operator who stops the service
	// (MADR 0054 amendment S10, S10-1). Registered enabled, an operator's stop
	// would be undone by the watchdog trigger within a minute. Disabled is the
	// task's switch, as Task Scheduler reports it (MADR 0207 D31).
	if st.Disabled {
		if want, err = withTaskDisabled(want); err != nil {
			return res, err
		}
	}
	res.Body = want
	paths, err := taskDiffPaths(existing, want)
	if err != nil {
		return res, fmt.Errorf("compare scheduled task %q with this release's definition: %w", name, err)
	}
	if len(paths) == 0 {
		res.Verdict = VerdictUnchanged
		if inNew && !ro.PrintOnly {
			// Already moved: only what a previous move left behind.
			finishMigration(&mig, userPath)
		}
		res.Warnings = append(res.Warnings, mig.Notes...)
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
	// Task Scheduler has no reload step: the registered definition is what
	// the next run uses. Unset, the updater warns that the next start may
	// run the previous definition (MADR 0054 amendment S10, S10-2).
	res.Reloaded = true
	if moved || inNew {
		finishMigration(&mig, userPath)
	}
	if moved {
		res.Migrated = mig.To
		res.Reason = "moved the task to " + filepath.Join(mig.To, opts.Product+".exe")
	}
	res.Warnings = append(res.Warnings, mig.Notes...)
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

// taskBackupPath is where a refresh keeps the previous definition: the
// product's private state directory, which roams with nothing and is readable
// by no other principal.
var taskBackupPath = func(product, name string) (string, error) {
	path, err := taskBackupFile(product, name)
	if err != nil {
		return "", err
	}
	if err := appdirs.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return "", fmt.Errorf("prepare %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

// taskBackupFile is taskBackupPath's file, looked up without creating
// anything: a remove deletes a backup there, and must not make the state
// directory to find none (MADR 0210 D9).
var taskBackupFile = func(product, name string) (string, error) {
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
