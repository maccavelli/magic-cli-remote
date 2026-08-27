package service

import (
	"fmt"
	"os"
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
		if _, err := tmp.WriteString(body); err != nil {
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

// sameTaskDefinition compares a registered definition with a freshly rendered
// one, ignoring the whitespace and XML declaration the task engine rewrites.
//
// schtasks /query /xml returns UTF-16 with its own formatting, so a byte
// comparison would report every task as changed and re-register on every run —
// defeating the idempotency contract this function exists to uphold.
func sameTaskDefinition(existing, want string) bool {
	return normalizeTaskXML(existing) == normalizeTaskXML(want)
}

// normalizeTaskXML strips the declaration, BOM and all whitespace between
// tags, leaving the semantic content.
func normalizeTaskXML(s string) string {
	s = strings.TrimPrefix(s, "\ufeff") // UTF-8 BOM
	if i := strings.Index(s, "<Task"); i >= 0 {
		s = s[i:]
	}
	var b strings.Builder
	b.Grow(len(s))
	inTag, prevSpace := false, false
	for _, r := range s {
		switch {
		case r == '<':
			inTag, prevSpace = true, false
			b.WriteRune(r)
		case r == '>':
			inTag, prevSpace = false, false
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			if inTag && !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			prevSpace = false
			b.WriteRune(r)
		}
	}
	return b.String()
}
