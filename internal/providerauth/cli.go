package providerauth

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// ansiPattern strips SGR and cursor escapes. Both grok and codex colour their
// device-flow output, so the URL and code arrive wrapped in escapes that a
// naive parser would fold into the values.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// StripANSI removes terminal escapes from a line of CLI output.
func StripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// urlPattern finds the verification link in CLI output.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// CLIFlow is a device flow driven by a spawned CLI.
//
// Exit is published as a closed channel plus a stored error rather than a
// value on a channel: both Wait and Kill need to observe it, and a one-shot
// channel receive lets whichever runs first consume the result and strand the
// other. That is not hypothetical — it deadlocked Wait after Kill until a test
// caught it.
type CLIFlow struct {
	cmd    *exec.Cmd
	exited chan struct{}
	err    error
	once   sync.Once
}

// StartCLIDeviceFlow spawns bin with args and scans its output for a
// verification URL and user code.
//
// The process keeps running after this returns: the CLI is what polls the
// vendor and writes the credential. Wait blocks on its exit.
//
// scanTimeout bounds only the wait for the code to appear, not the flow.
// extraEnv is KEY=VAL overlays applied over the inherited environment (PATH
// replacements included). Empty leaves cmd.Env nil so the child inherits
// (Codex). Grok passes a stub-open PATH so webbrowser::open cannot launch a
// host browser (MADR 0107 D6).
func StartCLIDeviceFlow(
	ctx context.Context,
	bin string,
	args []string,
	scanTimeout time.Duration,
	extraEnv []string,
) (cls Classification, flow *CLIFlow, err error) {
	cmd := exec.Command(bin, args...) //nolint:gosec // bin comes from provider config
	// The npm-shim agents (codex) exec a vendored binary as a grandchild that
	// survives a plain Kill of the shim — observed 2026-08-10, where a
	// cancelled probe left the real process running. Grouping is what makes
	// cancellation actually work.
	procutil.SetProcessGroup(cmd)
	procutil.SetDeathSignal(cmd)
	applyExtraEnv(cmd, extraEnv)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Classification{}, nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return Classification{}, nil, fmt.Errorf("start %s: %w", bin, err)
	}

	f := &CLIFlow{cmd: cmd, exited: make(chan struct{})}
	scanned := make(chan Classification, 1)
	go func() {
		scanned <- scanForCode(stdout)
		// Drain the rest so the child never blocks writing into a full pipe.
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() {
		f.err = cmd.Wait()
		close(f.exited)
	}()

	select {
	case cls = <-scanned:
	case <-time.After(scanTimeout):
		f.Kill()
		return Classification{}, nil, fmt.Errorf("%s printed no device code within %s", bin, scanTimeout)
	case <-ctx.Done():
		f.Kill()
		return Classification{}, nil, ctx.Err()
	}

	if cls.Kind != FlowDevice {
		f.Kill()
		return Classification{}, nil, fmt.Errorf("%s did not start a device flow", bin)
	}
	return cls, f, nil
}

func applyExtraEnv(cmd *exec.Cmd, extra []string) {
	if cmd == nil || len(extra) == 0 {
		return
	}
	env := os.Environ()
	for _, kv := range extra {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		env = replaceEnv(env, key, kv)
	}
	cmd.Env = env
}

func replaceEnv(env []string, key, kv string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			if !found {
				next = append(next, kv)
				found = true
			}
			continue
		}
		next = append(next, e)
	}
	if !found {
		next = append(next, kv)
	}
	return next
}

// scanForCode reads output until it has both a URL and a code, or the stream
// ends. Both agents print the URL first and the code a couple of lines later,
// so the two are accumulated independently rather than expected on one line.
func scanForCode(r io.Reader) Classification {
	var uri, code string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 8<<10), 64<<10)
	for sc.Scan() {
		line := StripANSI(sc.Text())
		if uri == "" {
			if m := urlPattern.FindString(line); m != "" {
				uri = strings.TrimRight(m, ".,)")
			}
		}
		if code == "" {
			if m := codePattern.FindStringSubmatch(line); len(m) == 2 {
				code = m[1]
			} else if m := bareCodePattern.FindStringSubmatch(line); len(m) == 2 {
				code = m[1]
			}
		}
		if uri != "" && code != "" {
			return Classification{Kind: FlowDevice, VerificationURI: uri, UserCode: code}
		}
	}
	if uri != "" && code != "" {
		return Classification{Kind: FlowDevice, VerificationURI: uri, UserCode: code}
	}
	return Classification{Kind: FlowUnknown, VerificationURI: uri, UserCode: code}
}

// Wait blocks until the CLI exits, ctx ends, or the flow is killed.
func (f *CLIFlow) Wait(ctx context.Context) error {
	select {
	case <-f.exited:
		if f.err != nil {
			return fmt.Errorf("device sign-in failed: %w", f.err)
		}
		return nil
	case <-ctx.Done():
		f.Kill()
		return ctx.Err()
	}
}

// Kill terminates the flow's whole process group.
func (f *CLIFlow) Kill() {
	f.once.Do(func() {
		if f.cmd == nil || f.cmd.Process == nil {
			return
		}
		procutil.TerminateProcessGroup(f.cmd.Process, f.exited, 3*time.Second)
	})
}
