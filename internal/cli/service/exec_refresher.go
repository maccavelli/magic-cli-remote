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

	"github.com/maccavelli/magic-cli-remote/internal/update"
)

// ExecRefresher reconciles a service definition by running
// `<binary> setup-service --refresh --json` in a child process.
//
// The child is the point. A process performing an update is the OLD binary and
// carries the OLD template, so it cannot render the definition the release
// ships; only the binary that was just swapped into place can (MADR 0100).
//
// The dependency points this way — service imports update, never the reverse —
// because internal/update deliberately knows nothing about templates, units, or
// systemctl (0065 P1), and this adapter is exactly that knowledge.
type ExecRefresher struct {
	// Timeout bounds the child. Zero means 60s.
	Timeout time.Duration
}

// refreshTimeout is the default bound on the child process.
const refreshTimeout = 60 * time.Second

// RefreshUnit implements update.UnitRefresher.
func (e ExecRefresher) RefreshUnit(product, binary string) (update.UnitRefresh, error) {
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
		return update.UnitRefresh{}, fmt.Errorf("%s setup-service --refresh: %w (%s)", binary, err, detail)
	}

	var res RefreshResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return update.UnitRefresh{}, fmt.Errorf("parse refresh output from %s: %w", binary, err)
	}
	return update.UnitRefresh{
		Changed:    res.Changed,
		Path:       res.Path,
		BackupPath: res.BackupPath,
		Output:     refreshSummary(res, product),
	}, nil
}

// RestoreUnit implements update.UnitRefresher. It renames the backup back and
// reloads in-process: no template is involved, so the old binary can do it
// while rolling an update back.
func (e ExecRefresher) RestoreUnit(product string, r update.UnitRefresh) error {
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
