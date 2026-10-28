package updateclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/selfupdatetest"
	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/systemd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// commandRun is one run of the update command: its streams and exit status.
type commandRun struct {
	stdout, stderr string
	code           int
	exe            string
	// requests is how many requests the fake GitHub received.
	requests int
	// downloads is how many of them were for a release asset.
	downloads int
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
	return runCommandContext(context.Background(), t, false, current, stdin, args...)
}

// runManagedCommand is runCommand with the service path on: the update asks
// the backend and the reconciler that withBackend and withChild set.
func runManagedCommand(t *testing.T, current string, args ...string) commandRun {
	t.Helper()
	return runCommandContext(context.Background(), t, true, current, "", args...)
}

// runCommandContext is runCommand under the caller's context ctx, managed or
// not.
func runCommandContext(ctx context.Context, t *testing.T, managed bool, current string, stdin string, args ...string) commandRun {
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
	prevID, prevNew, prevDet, prevIns := commandIdentity, newCommandUpdater, newCommandDetacher, newCommandInsider
	commandIdentity = func() buildinfo.Info { return id }
	newCommandUpdater = func(product string, id buildinfo.Info, _ service.HealthOptions) (*selfupdate.Updater, error) {
		return New(Options{Product: product, Identity: id, HTTPClient: gh.Client, APIBaseURL: gh.APIBase.String(), TargetPath: exe, Managed: managed})
	}
	// No test reaches the host's real service manager: the handoff backend is
	// the one withDetacher sets, or none (MADR 0210 D1), and the refusal
	// backend the one withInsider sets, or none (D8).
	newCommandDetacher = func(string, service.HealthOptions) (libservice.Detacher, bool) {
		return commandTestDetacher, commandTestDetacher != nil
	}
	newCommandInsider = func(string, service.HealthOptions) (insider, bool) {
		return commandTestInsider, commandTestInsider != nil
	}
	t.Cleanup(func() {
		commandIdentity, newCommandUpdater, newCommandDetacher, newCommandInsider = prevID, prevNew, prevDet, prevIns
	})

	cmd := NewCommand("mcremote", service.HealthOptions{})
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(ctx)
	run := commandRun{stdout: out.String(), stderr: errb.String(), exe: exe}
	for _, r := range gh.Requests() {
		run.requests++
		if r.Host != gh.APIBase.Host {
			run.downloads++
		}
	}
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
	run := runCommandContext(ctx, t, false, "v0.1.0", "", "--check")
	if run.code != 1 || !strings.Contains(run.stderr, "context canceled") {
		t.Errorf("exit status %d, stderr %q; want 1 and the cancellation", run.code, run.stderr)
	}
	if run.requests != 0 {
		t.Errorf("a cancelled update sent %d requests to GitHub", run.requests)
	}
}

// TestUpdateWarnsWhenTheServiceRunsAnotherBinary is MADR 0201 D26 (0073 §6.6):
// when the service's definition runs a different binary than the one the
// update replaced, the refresh child says so in its receipt's warnings, and
// the update prints each as `warning: …` on stderr and still exits 0. Before
// MADR 0054 the receipt's detail was dropped and nothing was printed.
func TestUpdateWarnsWhenTheServiceRunsAnotherBinary(t *testing.T) {
	warning := "the definition runs /x, not this binary (/y)"
	withBackend(t, &fakeService{installed: true, enabled: true, running: true}, nil)
	withChild(t, libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{
		Verdict: service.VerdictUnchanged, Path: "/u/mcremote.service", Reloaded: true, Warnings: []string{warning},
	})})
	run := runManagedCommand(t, "v0.1.0", "--yes")
	if run.code != 0 {
		t.Fatalf("exit status %d, want 0\nstderr:\n%s", run.code, run.stderr)
	}
	if !strings.Contains(run.stderr, "warning: "+warning+"\n") {
		t.Errorf("stderr does not carry the refresh's warning:\n%s", run.stderr)
	}
	if got := lastLine(run.stderr); got != "mcremote: updated v0.1.0 -> v0.2.0" {
		t.Errorf("stderr's last line = %q, want the update", got)
	}
}

// TestUpdateOnAHostWithNoServiceInstallsStandalone is 0073 §6.1, fixed before
// MADR 0054 moved the update to go-selfupdate-lib, and kept by that move
// (MADR 0201): a host with no service definition gets the binary alone, with
// no stop, no start, no refresh child and no rollback.
func TestUpdateOnAHostWithNoServiceInstallsStandalone(t *testing.T) {
	svc := &fakeService{}
	withBackend(t, svc, nil)
	runs := withChild(t, libservice.Output{})
	run := runManagedCommand(t, "v0.1.0", "--yes")
	if run.code != 0 {
		t.Fatalf("exit status %d, want 0\nstderr:\n%s", run.code, run.stderr)
	}
	requireReplaced(t, run.exe)
	if svc.stops != 0 || svc.starts != 0 || len(*runs) != 0 {
		t.Errorf("stops=%d starts=%d refresh runs=%d, want none", svc.stops, svc.starts, len(*runs))
	}
	if strings.Contains(run.stderr, "rolled back") || strings.Contains(run.stderr, "rollback") {
		t.Errorf("the update rolled back:\n%s", run.stderr)
	}
}

// commandTestDetacher is the handoff backend runCommandContext gives the
// command; nil means none, as when no backend can be built. Every backend
// Magic builds hands off since MADR 0212 P5.
var commandTestDetacher libservice.Detacher

// fakeDetacher records the handoffs the command asks for.
type fakeDetacher struct {
	inside      bool
	insideCalls int
	specs       []libservice.HandOff
}

func (f *fakeDetacher) Inside(context.Context) (bool, error) {
	f.insideCalls++
	return f.inside, nil
}

func (f *fakeDetacher) Detach(_ context.Context, spec libservice.HandOff) (libservice.Detached, error) {
	f.specs = append(f.specs, spec)
	return libservice.Detached{ID: "fake", Where: "a fake transient unit", ResultPath: spec.ResultPath}, nil
}

// withDetacher makes the command's handoff backend d, and its arguments args.
func withDetacher(t *testing.T, d libservice.Detacher, args ...string) {
	t.Helper()
	prevD, prevArgs := commandTestDetacher, commandArgs
	commandTestDetacher = d
	commandArgs = func() []string { return args }
	t.Cleanup(func() { commandTestDetacher, commandArgs = prevD, prevArgs })
}

// TestUpdateHandsOffFromInsideTheService is MADR 0210 D1. An update run inside
// the service, as by an agent the daemon spawned, starts a detached copy of
// the same command through the backend, and exits 0 at once, replacing
// nothing itself.
func TestUpdateHandsOffFromInsideTheService(t *testing.T) {
	d := &fakeDetacher{inside: true}
	withDetacher(t, d, "update", "--yes")
	run := runCommand(t, "v0.1.0", "", "--yes")
	if run.code != 0 {
		t.Fatalf("exit status %d, want 0\nstderr:\n%s", run.code, run.stderr)
	}
	if !strings.Contains(run.stderr, "update handed off: ") {
		t.Errorf("stderr does not say the update was handed off:\n%s", run.stderr)
	}
	if len(d.specs) != 1 || !slices.Equal(d.specs[0].Args, []string{"update", "--yes"}) {
		t.Fatalf("handoffs = %+v, want one, with the command's own arguments", d.specs)
	}
	if b, _ := os.ReadFile(run.exe); string(b) != "mcremote v0.1.0" {
		t.Errorf("the handing-off run replaced the executable itself: %q", b)
	}
}

// TestUpdateHandOffUnderJSON: under --json the result object says handed_off.
func TestUpdateHandOffUnderJSON(t *testing.T) {
	withDetacher(t, &fakeDetacher{inside: true}, "update", "--yes", "--json")
	run := runCommand(t, "v0.1.0", "", "--yes", "--json")
	if run.code != 0 {
		t.Fatalf("exit status %d, want 0\nstderr:\n%s", run.code, run.stderr)
	}
	lines := strings.Split(strings.TrimRight(run.stdout, "\n"), "\n")
	var result map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &result); err != nil {
		t.Fatalf("last stdout line is not JSON: %q", lines[len(lines)-1])
	}
	if result["kind"] != "result" || result["handed_off"] == nil || result["handed_off"] == "" {
		t.Errorf("result object = %v, want kind result with handed_off", result)
	}
}

// TestUpdateHandOffNeedsYes: the detached run cannot prompt, so inside the
// service an update a person confirmed at a prompt is still refused, and
// nothing is detached or replaced (MADR 0210 D6; the library's HandOffFunc).
func TestUpdateHandOffNeedsYes(t *testing.T) {
	prev := commandInteractive
	commandInteractive = true
	t.Cleanup(func() { commandInteractive = prev })
	d := &fakeDetacher{inside: true}
	withDetacher(t, d, "update")
	run := runCommand(t, "v0.1.0", "y\n")
	if run.code != 1 || !strings.Contains(run.stderr, selfupdate.ErrConfirmationRequired.Error()) {
		t.Errorf("exit status %d, stderr:\n%s\nwant 1 and %q", run.code, run.stderr, selfupdate.ErrConfirmationRequired)
	}
	if len(d.specs) != 0 {
		t.Errorf("an unconfirmed update was handed off: %+v", d.specs)
	}
	if b, _ := os.ReadFile(run.exe); string(b) != "mcremote v0.1.0" {
		t.Errorf("an unconfirmed update changed the executable: %q", b)
	}
}

// TestUpdateOutsideTheServiceRunsItself: an operator's update, outside the
// service, is today's update.
func TestUpdateOutsideTheServiceRunsItself(t *testing.T) {
	d := &fakeDetacher{inside: false}
	withDetacher(t, d, "update", "--yes")
	run := runCommand(t, "v0.1.0", "", "--yes")
	if run.code != 0 || lastLine(run.stderr) != "mcremote: updated v0.1.0 -> v0.2.0" {
		t.Fatalf("exit status %d, stderr's last line %q; want 0 and the update", run.code, lastLine(run.stderr))
	}
	if d.insideCalls == 0 || len(d.specs) != 0 {
		t.Errorf("Inside calls %d, handoffs %d; want Inside asked and no handoff", d.insideCalls, len(d.specs))
	}
	requireReplaced(t, run.exe)
}

// TestUpdateChecksAndDryRunsNeverHandOff: a check or a dry run installs
// nothing, so it runs where it is, inside the service or not.
func TestUpdateChecksAndDryRunsNeverHandOff(t *testing.T) {
	for _, args := range [][]string{{"--check"}, {"--dry-run", "--yes"}} {
		d := &fakeDetacher{inside: true}
		withDetacher(t, d, append([]string{"update"}, args...)...)
		run := runCommand(t, "v0.1.0", "", args...)
		if len(d.specs) != 0 || strings.Contains(run.stderr, "handed off") {
			t.Errorf("%v was handed off: %+v\n%s", args, d.specs, run.stderr)
		}
	}
}

// detacherService is a backend that is also a Detacher, as the library's
// systemd, launchd and Task Scheduler backends are.
type detacherService struct {
	fakeService
	fakeDetacher
}

// insiderService is a backend that answers Inside and is not a Detacher, as
// Magic's runit, s6 and OpenRC backend was until MADR 0212 P5 made it the
// library's adapter. No backend Magic builds is one now.
type insiderService struct {
	fakeService
	fakeInsider
}

// The library's four backends Magic builds hand off (MADR 0210 D1, D11; MADR
// 0212 P5), checked on every OS this compiles for.
var (
	_ libservice.Detacher = (*systemd.Unit)(nil)
	_ libservice.Detacher = (*launchd.Job)(nil)
	_ libservice.Detacher = (*taskscheduler.Task)(nil)
	_ libservice.Detacher = (*libservice.Supervised)(nil)
)

// TestDetacherForGatesBackendsThatCannotHandOff is MADR 0210 D1, D4, D8 and
// D11: the handoff is set wherever the backend is a Detacher, Windows' Task
// Scheduler backend included since go-selfupdate-lib v1.19.0 (#6), and the
// runit, s6 and OpenRC adapter since MADR 0212 P5. Where it is not set, a
// backend that answers Inside gets the refusal instead.
func TestDetacherForGatesBackendsThatCannotHandOff(t *testing.T) {
	for _, tc := range []struct {
		name           string
		backend        any
		detach, refuse bool
	}{
		{"a Detacher (systemd, launchd, Task Scheduler, the supervised adapter)", &detacherService{}, true, false},
		{"a backend that answers Inside and cannot hand off", &insiderService{}, false, true},
		{"a backend that answers neither", &fakeService{}, false, false},
	} {
		if _, ok := detacherFor(tc.backend); ok != tc.detach {
			t.Errorf("%s: detacherFor = %v, want %v", tc.name, ok, tc.detach)
		}
		if _, ok := insiderFor(tc.backend); ok != tc.refuse {
			t.Errorf("%s: insiderFor = %v, want %v", tc.name, ok, tc.refuse)
		}
	}
}

// commandTestInsider is the refusal backend runCommandContext gives the
// command; nil means none, as where a handoff is set.
var commandTestInsider insider

// fakeInsider is a backend that answers Inside and cannot hand off, as Magic's
// runit, s6 and OpenRC backend did before MADR 0212 P5.
type fakeInsider struct {
	inside bool
	err    error
	calls  int
}

func (f *fakeInsider) Inside(context.Context) (bool, error) {
	f.calls++
	return f.inside, f.err
}

// withInsider makes the command's refusal backend i.
func withInsider(t *testing.T, i insider) {
	t.Helper()
	prev := commandTestInsider
	commandTestInsider = i
	t.Cleanup(func() { commandTestInsider = prev })
}

// TestUpdateRefusesInsideBeforeDownloading is MADR 0210 D8: where nothing can
// hand off, an update inside the service fails with ErrInsideService, names
// the remedy, and downloads and replaces nothing.
func TestUpdateRefusesInsideBeforeDownloading(t *testing.T) {
	i := &fakeInsider{inside: true}
	withInsider(t, i)
	run := runCommand(t, "v0.1.0", "", "--yes")
	if run.code != 1 {
		t.Fatalf("exit status %d, want 1\nstderr:\n%s", run.code, run.stderr)
	}
	want := "update failed: this update runs inside the mcremote service and cannot hand off here; run it from a shell: " +
		libservice.ErrInsideService.Error()
	if got := lastLine(run.stderr); got != want {
		t.Errorf("stderr's last line = %q, want %q", got, want)
	}
	if run.downloads != 0 {
		t.Errorf("the refused update downloaded %d asset(s) first", run.downloads)
	}
	if b, _ := os.ReadFile(run.exe); string(b) != "mcremote v0.1.0" {
		t.Errorf("the refused update changed the executable: %q", b)
	}
}

// TestUpdateOutsideRunsWhereNothingHandsOff: outside the service, the
// refusal backend lets an operator's update run as today.
func TestUpdateOutsideRunsWhereNothingHandsOff(t *testing.T) {
	i := &fakeInsider{inside: false}
	withInsider(t, i)
	run := runCommand(t, "v0.1.0", "", "--yes")
	if run.code != 0 || lastLine(run.stderr) != "mcremote: updated v0.1.0 -> v0.2.0" {
		t.Fatalf("exit status %d, stderr's last line %q; want 0 and the update", run.code, lastLine(run.stderr))
	}
	if i.calls == 0 {
		t.Error("Inside was never asked")
	}
	requireReplaced(t, run.exe)
}

// TestUpdateFailsWhenInsideCannotBeTold: an Inside error fails the update
// before the download, and is never read as outside.
func TestUpdateFailsWhenInsideCannotBeTold(t *testing.T) {
	withInsider(t, &fakeInsider{err: errors.New("read the process ancestry: permission denied")})
	run := runCommand(t, "v0.1.0", "", "--yes")
	if run.code != 1 || !strings.Contains(run.stderr, "read the process ancestry: permission denied") {
		t.Errorf("exit status %d, stderr:\n%s\nwant 1 and the Inside error", run.code, run.stderr)
	}
	if run.downloads != 0 {
		t.Errorf("the failed update downloaded %d asset(s)", run.downloads)
	}
	if b, _ := os.ReadFile(run.exe); string(b) != "mcremote v0.1.0" {
		t.Errorf("the failed update changed the executable: %q", b)
	}
}

// TestUpdateChecksAndDryRunsNeverAskInside: a check or a dry run installs
// nothing, so it runs where it is, and never asks.
func TestUpdateChecksAndDryRunsNeverAskInside(t *testing.T) {
	for _, args := range [][]string{{"--check"}, {"--dry-run", "--yes"}} {
		i := &fakeInsider{inside: true}
		withInsider(t, i)
		run := runCommand(t, "v0.1.0", "", args...)
		if i.calls != 0 || strings.Contains(run.stderr, "runs inside") {
			t.Errorf("%v: Inside calls %d\n%s", args, i.calls, run.stderr)
		}
	}
}
