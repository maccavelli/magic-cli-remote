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

// Termination bounds. killTimeout is how long the process group gets to die;
// killDrainTimeout additionally bounds a Wait that raced a Kill.
const (
	killTimeout      = 3 * time.Second
	killDrainTimeout = 5 * time.Second
)

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
	// err is written by the waiter goroutine before it closes exited, so any
	// read after <-exited is ordered behind that write.
	err  error
	once sync.Once

	// mu guards cancelled, which records that a Kill initiated termination
	// before the child finished on its own.
	mu        sync.Mutex
	cancelled bool

	// termOnce fixes the terminal result the first time any observer asks, so
	// Wait and Kill callers cannot disagree about how the flow ended.
	termOnce sync.Once
	term     error
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

	// The child writes into a pipe this function owns rather than one from
	// cmd.StdoutPipe(). Wait closes a StdoutPipe as soon as it reaps the
	// child, and os/exec documents that calling Wait before every read has
	// finished is incorrect. A CLI that prints its code and exits promptly
	// therefore raced the scanner: the read end closed mid-scan, the scan
	// came back FlowUnknown, and a perfectly good device flow was reported as
	// "did not start a device flow". Owning the pipe decouples the scan from
	// the reap, so the two can no longer race.
	pr, pw, err := os.Pipe()
	if err != nil {
		return Classification{}, nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return Classification{}, nil, fmt.Errorf("start %s: %w", bin, err)
	}
	// The child must hold the only write end, or the reader never sees EOF.
	_ = pw.Close()

	f := &CLIFlow{cmd: cmd, exited: make(chan struct{})}
	scanned := make(chan Classification, 1)
	scanDone := make(chan struct{})
	go func() {
		scanned <- scanForCode(pr)
		close(scanDone)
		// Drain the rest so the child never blocks writing into a full pipe.
		_, _ = io.Copy(io.Discard, pr)
		_ = pr.Close()
	}()
	go func() {
		f.err = cmd.Wait()
		close(f.exited)
		// A grandchild that outlived the child can hold the write end open
		// indefinitely. Closing the read end once the scan has finished
		// unblocks that drain without ever truncating the scan.
		<-scanDone
		_ = pr.Close()
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
//
// Wait and Kill are safe to call in any order, concurrently, and repeatedly.
// Every observer receives the same terminal result: a cancelled flow always
// reports ErrFlowCancelled, and a flow that finished on its own always reports
// its real outcome even if Kill arrives afterwards (MADR 0074 D27).
func (f *CLIFlow) Wait(ctx context.Context) error {
	select {
	case <-f.exited:
		return f.terminal()
	case <-ctx.Done():
		f.Kill()
		// Kill drains the process group, but never block a caller forever on a
		// child that refuses to die.
		select {
		case <-f.exited:
		case <-time.After(killDrainTimeout):
		}
		return f.terminal()
	}
}

// Kill terminates the flow's whole process group. Killing a flow that already
// finished is a no-op and must not rewrite its outcome.
func (f *CLIFlow) Kill() {
	select {
	case <-f.exited:
	default:
		f.mu.Lock()
		f.cancelled = true
		f.mu.Unlock()
	}
	f.once.Do(func() {
		if f.cmd == nil || f.cmd.Process == nil {
			return
		}
		procutil.TerminateProcessGroup(f.cmd.Process, f.exited, killTimeout)
	})
}

// terminal computes the flow's single terminal result exactly once.
func (f *CLIFlow) terminal() error {
	f.termOnce.Do(func() {
		f.mu.Lock()
		cancelled := f.cancelled
		f.mu.Unlock()
		switch {
		case cancelled:
			f.term = ErrFlowCancelled
		case f.err != nil:
			f.term = fmt.Errorf("device sign-in failed: %w", f.err)
		}
	})
	return f.term
}
