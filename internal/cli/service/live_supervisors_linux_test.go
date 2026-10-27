//go:build linux

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The live runit, s6 and OpenRC tests of MADR 0212 D13-A, which `make
// live-supervisors` runs inside each supervisor's container
// (test/supervisors, scripts/live_supervisors.py). Each is off unless its
// MCREMOTE_REQUIRE_<KIND>=1, and then it requires:
//
//   - MCREMOTE_LIVE_BINARY, the mcremote build under test;
//   - MCREMOTE_LIVE_SMOKE, scripts/smoke-protocol, built;
//   - MCREMOTE_LIVE_AGENT, test/supervisors/probeagent, built;
//   - MCREMOTE_LIVE_RELEASE, the published release to update to, by default
//     liveRelease (the 0212 PLAN's P6 Deviation 1).
//
// It installs a scratch service, stops and starts it through the update's
// own backend, updates it from a shell, puts the build under test back,
// updates it from inside, through the daemon's own agent, which must hand
// off, and removes it. It ends with the released build running, read through
// /proc/<pid>/exe. The update downloads the release from GitHub; GH_TOKEN or
// GITHUB_TOKEN raises the rate limit.
const (
	liveBinaryEnv  = "MCREMOTE_LIVE_BINARY"
	liveSmokeEnv   = "MCREMOTE_LIVE_SMOKE"
	liveAgentEnv   = "MCREMOTE_LIVE_AGENT"
	liveReleaseEnv = "MCREMOTE_LIVE_RELEASE"
	liveRelease    = "v0.23.0"
	liveUnit       = "mcremote-live"
	livePort       = 17641
)

func TestLiveRunit(t *testing.T)  { liveSupervisor(t, KindRunit, "MCREMOTE_REQUIRE_RUNIT") }
func TestLiveS6(t *testing.T)     { liveSupervisor(t, KindS6, "MCREMOTE_REQUIRE_S6") }
func TestLiveOpenRC(t *testing.T) { liveSupervisor(t, KindOpenRCUser, "MCREMOTE_REQUIRE_OPENRC") }

// liveHost is one live run's scratch service: the binary it runs, under
// $HOME, since an update replaces a binary only there; its config, whose grok
// provider is the probe agent; and the tools that drive it.
type liveHost struct {
	build   string // the build under test, never replaced
	bin     string // the copy the service runs, which an update replaces
	config  string
	work    string
	smoke   string
	release string
}

func liveSupervisor(t *testing.T, kind Kind, requireEnv string) {
	if os.Getenv(requireEnv) != "1" {
		t.Skipf("set %s=1, with %s, %s and %s; `make live-supervisors` runs it in a container", requireEnv,
			liveBinaryEnv, liveSmokeEnv, liveAgentEnv)
	}
	h := newLiveHost(t)
	ctx := context.Background()

	step := func(name string, fn func(t *testing.T)) {
		t.Helper()
		if !t.Run(name, fn) {
			t.FailNow()
		}
	}
	step("install", func(t *testing.T) {
		h.mc(t, h.bin, "setup-service", "--unit-name", liveUnit, "--binary", h.bin, "--service-config", h.config,
			"--listen-port", strconv.Itoa(livePort), "--working-directory", h.work, "--no-linger", "--force")
		if got, err := ResolveBackend("mcremote"); err != nil || got != kind {
			t.Fatalf("recorded backend %s, %v; want %s", got, err, kind)
		}
		h.healthy(t)
		h.runs(t, h.build)
	})
	step("stop and start through the update's backend", func(t *testing.T) {
		b := h.backend(t)
		before := h.pid(t)
		if err := b.Stop(ctx, "mcremote"); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if running, err := b.Running(ctx, "mcremote"); err != nil || running {
			t.Fatalf("after Stop: running %v, %v", running, err)
		}
		if err := b.Start(ctx, "mcremote"); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := b.WaitHealthy(ctx, "mcremote"); err != nil {
			t.Fatalf("WaitHealthy: %v", err)
		}
		if after := h.pid(t); after == before {
			t.Fatalf("the daemon is still process %d", before)
		}
		h.healthy(t)
	})
	step("update from a shell", func(t *testing.T) {
		before := h.pid(t)
		out := h.mc(t, h.bin, "update", "--yes", "--force", "--version", h.release, "--json")
		doc := lastResult(t, out)
		h.updated(t, doc, before)
	})
	step("the build under test again", func(t *testing.T) {
		placeBinary(t, h.build, h.bin)
		b := h.backend(t)
		if err := b.Stop(ctx, "mcremote"); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if err := b.Start(ctx, "mcremote"); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := b.WaitHealthy(ctx, "mcremote"); err != nil {
			t.Fatalf("WaitHealthy: %v", err)
		}
		h.runs(t, h.build)
	})
	step("update from inside, handed off", func(t *testing.T) {
		before := h.pid(t)
		result := filepath.Join(filepath.Dir(h.bin), "."+filepath.Base(h.bin)+".selfupdate.handoff")
		_ = os.Remove(result)
		said := h.prompt(t, "RUN "+h.bin+" --config "+h.config+" update --yes --force --version "+h.release)
		if !strings.Contains(said, "update handed off") {
			t.Fatalf("the agent's update did not hand off:\n%s", said)
		}
		var res libservice.HandOffResult
		if !poll(240*time.Second, func() bool {
			r, err := libservice.ReadHandOffResult(result)
			res = r
			return err == nil
		}) {
			t.Fatalf("no handoff result at %s within 240 s", result)
		}
		if res.ExitCode != 0 || res.Error != "" {
			t.Fatalf("the handed-off update: exit %d, %q", res.ExitCode, res.Error)
		}
		var doc resultLine
		doc.Result.Applied, doc.Result.ReplacedBeforeStop = res.Result.Applied, res.Result.ReplacedBeforeStop
		doc.Result.ServiceStarted, doc.Result.RolledBack = res.Result.ServiceStarted, res.Result.RolledBack
		h.updated(t, doc, before)
	})
	step("remove", func(t *testing.T) {
		h.mc(t, h.build, "setup-service", "--remove", "--unit-name", liveUnit)
		if _, err := os.Stat(scriptDefFor(kind, liveUnit, "").Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the definition is still there: %v", err)
		}
		if !poll(30*time.Second, func() bool { return !h.answers() }) {
			t.Fatal("the daemon still answers after the remove")
		}
	})
}

func newLiveHost(t *testing.T) *liveHost {
	t.Helper()
	paths := map[string]string{}
	for _, env := range []string{liveBinaryEnv, liveSmokeEnv, liveAgentEnv} {
		p := os.Getenv(env)
		if !filepath.IsAbs(p) {
			t.Fatalf("%s must be an absolute path; got %q", env, p)
		}
		paths[env] = p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(home, "mcr-live")
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatal(err)
	}
	h := &liveHost{
		build: paths[liveBinaryEnv], smoke: paths[liveSmokeEnv],
		bin: filepath.Join(scratch, "mcremote"), config: filepath.Join(scratch, "config.yaml"),
		work: filepath.Join(scratch, "work"), release: os.Getenv(liveReleaseEnv),
	}
	if h.release == "" {
		h.release = liveRelease
	}
	data := filepath.Join(scratch, "data")
	for _, d := range []string{h.work, data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	placeBinary(t, h.build, h.bin)
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	cfg := fmt.Sprintf("listen:\n  host: 127.0.0.1\n  port: %d\ntls:\n  enabled: false\ndata_dir: %s\n"+
		"auth:\n  require_client_key: false\nproviders:\n  fake:\n    enabled: false\n"+
		"  grok:\n    enabled: true\n    bin: %s\n    always_approve: true\n"+
		"  opencode:\n    enabled: false\n  codex:\n    enabled: false\n  kilo:\n    enabled: false\n",
		livePort, q(data), q(paths[liveAgentEnv]))
	if err := os.WriteFile(h.config, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Whatever failed, leave no service behind.
		_ = exec.Command(h.build, "--config", h.config, "setup-service", "--remove", "--unit-name", liveUnit).Run()
	})
	return h
}

// mc runs binary with the scratch config and args, fails the test on a
// non-zero exit, and returns stdout.
func (h *liveHost) mc(t *testing.T, binary string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, append([]string{"--config", h.config}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", filepath.Base(binary), strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	t.Logf("%s %s:\n%s%s", filepath.Base(binary), strings.Join(args, " "), stdout.String(), stderr.String())
	return stdout.String()
}

// backend is the update's own backend for the scratch service, as `update`
// builds it: the library's adapter (MADR 0212 P5).
func (h *liveHost) backend(t *testing.T) Backend {
	t.Helper()
	b, err := NewBackend("mcremote", liveUnit)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(*libservice.Supervised); !ok {
		t.Fatalf("the backend is %T, not the supervised adapter", b)
	}
	return b
}

// pid is the daemon's process, as the backend's state reports it.
func (h *liveHost) pid(t *testing.T) int {
	t.Helper()
	st, err := h.backend(t).(*libservice.Supervised).Status(context.Background())
	if err != nil || !st.Up || st.PID <= 0 {
		t.Fatalf("the service's state: %+v, %v; want it up with a PID", st, err)
	}
	return st.PID
}

// answers reports whether the daemon answers /healthz.
func (h *liveHost) answers() bool {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", livePort))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode == http.StatusOK && bytes.Contains(body, []byte(`"ok":true`))
}

func (h *liveHost) healthy(t *testing.T) {
	t.Helper()
	if !poll(60*time.Second, h.answers) {
		t.Fatal("the daemon did not answer /healthz within 60 s")
	}
}

// runs checks that the daemon's own executable, read through /proc, is the
// file want, byte for byte.
func (h *liveHost) runs(t *testing.T, want string) {
	t.Helper()
	if got, w := exeDigest(t, h.pid(t)), fileDigest(t, want); got != w {
		t.Fatalf("the daemon runs %s; want %s, %s", got, filepath.Base(want), w)
	}
}

// updated checks an update's result, then that a new daemon process answers,
// and runs a build that is not the one under test: the release's.
func (h *liveHost) updated(t *testing.T, doc resultLine, before int) {
	t.Helper()
	r := doc.Result
	if !r.Applied || !r.ReplacedBeforeStop || !r.ServiceStarted || r.RolledBack {
		t.Fatalf("result %+v: want applied, replaced before the stop, started, not rolled back", r)
	}
	h.healthy(t)
	after := h.pid(t)
	if after == before {
		t.Fatalf("the daemon is still process %d", before)
	}
	if exeDigest(t, after) == fileDigest(t, h.build) {
		t.Fatalf("the daemon, process %d, still runs the build under test", after)
	}
	if exeDigest(t, after) != fileDigest(t, h.bin) {
		t.Fatalf("the daemon, process %d, does not run %s", after, h.bin)
	}
}

// prompt has the daemon's agent, the probe agent, run text's command in a new
// session, and returns what the session said.
func (h *liveHost) prompt(t *testing.T, text string) string {
	t.Helper()
	out := h.mc(t, h.bin, "pair", "create", "--name", "live")
	token := ""
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Token:"); ok {
			token = strings.TrimSpace(v)
		}
	}
	if token == "" {
		t.Fatalf("pair create printed no token:\n%s", out)
	}
	cmd := exec.Command(h.smoke, "-host", fmt.Sprintf("127.0.0.1:%d", livePort), "-plaintext", "-token", token,
		"-provider", "grok", "-name", "live", "-cwd", h.work, "-prompt", text, "-wait", "60s")
	said, err := cmd.CombinedOutput()
	t.Logf("smoke-protocol: %v\n%s", err, said)
	// The update stops the daemon, so the session can end with an error.
	return string(said)
}

// resultLine is the update's last --json line, and the part of a handoff
// result the test reads.
type resultLine struct {
	Result struct {
		Applied            bool `json:"applied"`
		ReplacedBeforeStop bool `json:"replaced_before_stop"`
		ServiceStarted     bool `json:"service_started"`
		RolledBack         bool `json:"rolled_back"`
	} `json:"result"`
}

func lastResult(t *testing.T, out string) resultLine {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var doc resultLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &doc); err != nil {
		t.Fatalf("the last --json line: %v\n%s", err, out)
	}
	return doc
}

// placeBinary copies src to dst as a new file, renamed into place, as an
// update does: writing over a running binary would change the process's.
func placeBinary(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		t.Fatal(err)
	}
}

func exeDigest(t *testing.T, pid int) string {
	t.Helper()
	return fileDigest(t, filepath.Join("/proc", strconv.Itoa(pid), "exe"))
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// poll checks cond every half second until it holds or d passes.
func poll(d time.Duration, cond func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return cond()
}
