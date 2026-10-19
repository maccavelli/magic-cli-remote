package updateclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/selfupdatetest"
)

// commandRun is one run of the update command: its streams and exit status.
type commandRun struct {
	stdout, stderr string
	code           int
	exe            string
	// requests is how many requests the fake GitHub received.
	requests int
}

// lastLine is the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// runCommand runs `mcremote update args…` as a release build of current,
// against a fake GitHub with v0.1.0 and v0.2.0, on a temporary executable,
// without the service manager. stdin is the command's input, never the
// terminal.
func runCommand(t *testing.T, current string, stdin string, args ...string) commandRun {
	t.Helper()
	return runCommandContext(context.Background(), t, current, stdin, args...)
}

// runCommandContext is runCommand under the caller's context ctx.
func runCommandContext(ctx context.Context, t *testing.T, current string, stdin string, args ...string) commandRun {
	t.Helper()
	spec, err := releaseSpec()
	if err != nil {
		t.Fatal(err)
	}
	gh := selfupdatetest.NewGitHubServer(t, Repository.Owner, Repository.Name,
		selfupdatetest.NewRelease("mcremote", "v0.1.0", spec.Targets(), releaseBody("v0.1.0")),
		selfupdatetest.NewRelease("mcremote", "v0.2.0", spec.Targets(), releaseBody("v0.2.0")))
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "mcremote")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("mcremote "+current), 0o755); err != nil {
		t.Fatal(err)
	}
	id := buildinfo.Info{Version: current, Kind: buildinfo.KindRelease}
	prevID, prevNew := commandIdentity, newCommandUpdater
	commandIdentity = func() buildinfo.Info { return id }
	newCommandUpdater = func(product string, id buildinfo.Info) (*selfupdate.Updater, error) {
		return New(Options{Product: product, Identity: id, HTTPClient: gh.Client, APIBaseURL: gh.APIBase.String(), TargetPath: exe})
	}
	t.Cleanup(func() { commandIdentity, newCommandUpdater = prevID, prevNew })

	cmd := NewCommand("mcremote")
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(ctx)
	run := commandRun{stdout: out.String(), stderr: errb.String(), exe: exe, requests: len(gh.Requests())}
	var exit *ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		run.code = exit.Code
	default:
		t.Fatalf("update returned %v, not an *ExitError", err)
	}
	return run
}

// TestUpdateCommandStreamsAndExitCodes is the canonical command's contract
// (MADR 0054 S3, draft amendments 2 and 6): progress, prompts and the outcome
// on stderr, stdout empty without --json, and exit 0, 10 or 1.
func TestUpdateCommandStreamsAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name, current, stdin string
		args                 []string
		code                 int
		last                 string // stderr's last line, or a prefix of it
	}{
		{"an update is available", "v0.1.0", "", []string{"--check"}, 10, "mcremote: update available: v0.1.0 -> v0.2.0"},
		{"up to date", "v0.2.0", "", []string{"--check"}, 0, "mcremote: up to date (v0.2.0)"},
		{"no release with that tag", "v0.1.0", "", []string{"--yes", "--version", "v9.9.9"}, 1, "update failed: "},
		{"--check contradicts --yes", "v0.1.0", "", []string{"--check", "--yes"}, 1, "update failed: "},
		{"no prerelease channel is offered", "v0.1.0", "", []string{"--check", "--channel", "rc"}, 1, "update failed: "},
		{"no --yes without a terminal", "v0.1.0", "", nil, 1, "update failed: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runCommand(t, tc.current, tc.stdin, tc.args...)
			if run.code != tc.code {
				t.Errorf("exit status %d, want %d\nstderr:\n%s", run.code, tc.code, run.stderr)
			}
			if run.stdout != "" {
				t.Errorf("stdout = %q, want it empty without --json", run.stdout)
			}
			if got := lastLine(run.stderr); !strings.HasPrefix(got, tc.last) {
				t.Errorf("stderr's last line = %q, want %q", got, tc.last)
			}
			if strings.Count(run.stderr, "update failed:") > 1 {
				t.Errorf("the failure was reported more than once:\n%s", run.stderr)
			}
		})
	}
}

// TestUpdateCommandRefusals names the refusals the docs promise.
func TestUpdateCommandRefusals(t *testing.T) {
	if run := runCommand(t, "v0.1.0", "", "--check", "--channel", "rc"); !strings.Contains(run.stderr, `channel "rc" is not offered by the version policy`) {
		t.Errorf("--channel: stderr = %q", run.stderr)
	}
	if run := runCommand(t, "v0.1.0", ""); !strings.Contains(run.stderr, "pass --yes") {
		t.Errorf("no --yes: stderr = %q", run.stderr)
	}
}

// TestUpdateCommandJSON: under --json, stdout is JSON Lines ending in one
// result object that carries the exit status, and stderr has no outcome line.
func TestUpdateCommandJSON(t *testing.T) {
	run := runCommand(t, "v0.1.0", "", "--check", "--json")
	if run.code != 10 {
		t.Fatalf("exit status %d, want 10", run.code)
	}
	lines := strings.Split(strings.TrimRight(run.stdout, "\n"), "\n")
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("stdout line %d is not JSON: %q", i+1, line)
		}
	}
	var result struct {
		Kind     string `json:"kind"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &result); err != nil || result.Kind != "result" || result.ExitCode != 10 {
		t.Errorf("last line = %s (%v), want kind result, exit_code 10", lines[len(lines)-1], err)
	}
	if strings.Contains(run.stderr, "update available") {
		t.Errorf("--json still wrote the outcome line to stderr:\n%s", run.stderr)
	}
}

// TestUpdateCommandDryRun downloads and verifies the release and installs
// nothing.
func TestUpdateCommandDryRun(t *testing.T) {
	run := runCommand(t, "v0.1.0", "", "--dry-run", "--yes")
	if run.code != 0 {
		t.Fatalf("exit status %d, want 0\nstderr:\n%s", run.code, run.stderr)
	}
	if got := lastLine(run.stderr); got != "mcremote: dry run: v0.2.0 verified; nothing installed" {
		t.Errorf("stderr's last line = %q", got)
	}
	if b, _ := os.ReadFile(run.exe); string(b) != "mcremote v0.1.0" {
		t.Errorf("the dry run changed the executable: %q", b)
	}
}

// TestUpdateCommandDeclinesAtEndOfInput: a prompt whose input ends before an
// answer declines, and the update exits 0 having installed nothing.
func TestUpdateCommandDeclinesAtEndOfInput(t *testing.T) {
	prev := commandInteractive
	commandInteractive = true
	t.Cleanup(func() { commandInteractive = prev })
	run := runCommand(t, "v0.1.0", "")
	if run.code != 0 || lastLine(run.stderr) != "mcremote: update declined" {
		t.Errorf("exit status %d, stderr's last line %q; want 0 and the decline", run.code, lastLine(run.stderr))
	}
	if b, _ := os.ReadFile(run.exe); string(b) != "mcremote v0.1.0" {
		t.Errorf("a declined update changed the executable: %q", b)
	}
}

// TestNewCommandUsesCallerContext guards against a reintroduced
// context.Background: a cancelled caller aborts the update before any request
// reaches GitHub, and exits 1 reporting the cancellation.
func TestNewCommandUsesCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := runCommandContext(ctx, t, "v0.1.0", "", "--check")
	if run.code != 1 || !strings.Contains(run.stderr, "context canceled") {
		t.Errorf("exit status %d, stderr %q; want 1 and the cancellation", run.code, run.stderr)
	}
	if run.requests != 0 {
		t.Errorf("a cancelled update sent %d requests to GitHub", run.requests)
	}
}
