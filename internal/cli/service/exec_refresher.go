package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// UnitRefresh is what a post-swap definition reconciliation did. It lives here
// rather than in the updater because the updater is now
// github.com/maccavelli/mcplib/selfupdate, which owns no template, unit or
// systemctl knowledge (MADR 0005). internal/updateclient adapts this value
// into selfupdate.ReconcileResult.
type UnitRefresh struct {
	Changed    bool
	Path       string
	BackupPath string
	// Output is one already-formatted human line, or empty when there is
	// nothing worth saying.
	Output string
}

// ExecRefresher reconciles a service definition by running
// `<binary> setup-service --refresh --json` in a child process.
//
// The child is the point. A process performing an update is the OLD binary and
// carries the OLD template, so it cannot render the definition the release
// ships; only the binary that was just swapped into place can (MADR 0100).
//
// This package owns that knowledge deliberately. The shared updater knows
// nothing about templates, units, or systemctl (0065 P1, MADR 0005), so the
// adapter that does live here and in internal/updateclient.
type ExecRefresher struct {
	// Timeout bounds the child. Zero means 60s.
	Timeout time.Duration
}

// refreshTimeout is the default bound on the child process.
const refreshTimeout = 60 * time.Second

// RefreshUnit implements UnitRefresher.
func (e ExecRefresher) RefreshUnit(product, binary string) (UnitRefresh, error) {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = refreshTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// #nosec G204 — binary is the path this process just verified and installed.
	cmd := exec.CommandContext(ctx, binary, "setup-service", "--refresh", "--json")
	cmd.Env = withUserRuntimeEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Also the downgrade path: an older binary has no --refresh flag, exits
		// non-zero, and the caller carries on with the swap.
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return UnitRefresh{}, fmt.Errorf("%s setup-service --refresh: %w (%s)", binary, err, detail)
	}

	var res RefreshResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return UnitRefresh{}, fmt.Errorf("parse refresh output from %s: %w", binary, err)
	}
	return UnitRefresh{
		Changed:    res.Changed,
		Path:       res.Path,
		BackupPath: res.BackupPath,
		Output:     refreshSummary(res, product),
	}, nil
}

// RestoreUnit implements UnitRefresher. It renames the backup back and
// reloads in-process: no template is involved, so the old binary can do it
// while rolling an update back.
func (e ExecRefresher) RestoreUnit(product string, r UnitRefresh) error {
	return RestoreUnitBackup(r.Path, r.BackupPath)
}

// refreshSummary renders the same lines the CLI prints, for the update log.
func refreshSummary(res RefreshResult, product string) string {
	var buf bytes.Buffer
	if err := PrintRefreshResult(&buf, res, product, false); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}
