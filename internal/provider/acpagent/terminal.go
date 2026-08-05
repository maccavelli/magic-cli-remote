package acpagent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

type terminalHost struct {
	mu   sync.Mutex
	byID map[string]*terminalProc
	// closed is set by CloseAll (session teardown). Terminal children run in
	// their own process groups, so one created after teardown would be
	// unkillable through any API — Create must refuse instead.
	closed bool
}

// killIfRunning signals p's process group unless the child has already been
// reaped (done closed) — past that point the PID may be recycled and the
// signal would hit an unrelated process group.
func killIfRunning(p *terminalProc) {
	if p.cmd.Process == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	_ = procutil.KillProcessGroup(p.cmd.Process)
}

type terminalProc struct {
	id       string
	cmd      *exec.Cmd
	buf      *limitedBuffer
	done     chan struct{}
	exit     acp.WaitForTerminalExitResponse
	waitOnce sync.Once
}

func newTerminalHost() *terminalHost {
	return &terminalHost{byID: make(map[string]*terminalProc)}
}

func (h *terminalHost) Create(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	_ = ctx
	id := uuid.NewString()

	// Grok often packs a full shell line into Command with empty Args, e.g.
	//   Command: `/bin/bash -lc 'ls -la'`
	// exec.Command would treat that entire string as argv[0] and fail with
	// "no such file or directory". Route whole-line commands through bash -lc.
	cmd := buildTerminalCmd(params.Command, params.Args)
	if params.Cwd != nil && *params.Cwd != "" {
		cmd.Dir = *params.Cwd
	}
	if len(params.Env) > 0 {
		env := os.Environ()
		for _, e := range params.Env {
			env = append(env, e.Name+"="+e.Value)
		}
		cmd.Env = env
	}

	limit := 1024 * 1024
	if params.OutputByteLimit != nil && *params.OutputByteLimit > 0 {
		limit = *params.OutputByteLimit
	}
	// Clamp the agent-supplied limit: OutputByteLimit is attacker-influenced (a
	// prompt-injected agent could request gigabytes), and limitedBuffer retains
	// up to `limit` bytes per terminal.
	if limit > maxTerminalBuffer {
		limit = maxTerminalBuffer
	}
	buf := newLimitedBuffer(limit)
	cmd.Stdout = buf
	cmd.Stderr = buf
	procutil.SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		// The daemon runs agent terminals itself, under its own OS/TCC
		// identity (MADR 0069 F6 #1/#6): attribute a denial to the daemon,
		// not the command.
		if agenterr.IsPermission(err) {
			return acp.CreateTerminalResponse{}, fmt.Errorf(
				"start command: %w — the mcremote daemon lacks OS permission "+
					"here (macOS: see docs/ops-macos-tcc.md)", err)
		}
		return acp.CreateTerminalResponse{}, fmt.Errorf("start command: %w", err)
	}

	p := &terminalProc{
		id:   id,
		cmd:  cmd,
		buf:  buf,
		done: make(chan struct{}),
	}
	go p.reap()

	h.mu.Lock()
	if h.closed {
		// Session torn down while we were starting: kill the orphan now, it
		// would otherwise outlive the session unkillably (own process group).
		h.mu.Unlock()
		if cmd.Process != nil {
			_ = procutil.KillProcessGroup(cmd.Process)
		}
		return acp.CreateTerminalResponse{}, fmt.Errorf("session closed")
	}
	h.byID[id] = p
	h.mu.Unlock()

	return acp.CreateTerminalResponse{TerminalId: id}, nil
}

func (h *terminalHost) Output(_ context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	p, err := h.get(params.TerminalId)
	if err != nil {
		return acp.TerminalOutputResponse{}, err
	}
	out, truncated := p.buf.Snapshot()
	resp := acp.TerminalOutputResponse{Output: out, Truncated: truncated}
	select {
	case <-p.done:
		// Fill exit status if available.
		es := &acp.TerminalExitStatus{}
		if p.exit.ExitCode != nil {
			es.ExitCode = p.exit.ExitCode
		}
		if p.exit.Signal != nil {
			es.Signal = p.exit.Signal
		}
		resp.ExitStatus = es
	default:
	}
	if resp.Output == "" {
		// Some validators dislike empty strings; keep a single space when still running with no output.
		resp.Output = " "
	}
	return resp, nil
}

func (h *terminalHost) WaitForExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	p, err := h.get(params.TerminalId)
	if err != nil {
		return acp.WaitForTerminalExitResponse{}, err
	}
	select {
	case <-ctx.Done():
		return acp.WaitForTerminalExitResponse{}, ctx.Err()
	case <-p.done:
		return p.exit, nil
	}
}

func (h *terminalHost) Kill(_ context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	p, err := h.get(params.TerminalId)
	if err != nil {
		return acp.KillTerminalResponse{}, err
	}
	killIfRunning(p)
	return acp.KillTerminalResponse{}, nil
}

func (h *terminalHost) Release(_ context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	h.mu.Lock()
	p, ok := h.byID[params.TerminalId]
	if ok {
		delete(h.byID, params.TerminalId)
	}
	h.mu.Unlock()
	if ok {
		killIfRunning(p)
	}
	return acp.ReleaseTerminalResponse{}, nil
}

func (h *terminalHost) CloseAll() {
	h.mu.Lock()
	h.closed = true
	procs := make([]*terminalProc, 0, len(h.byID))
	for id, p := range h.byID {
		procs = append(procs, p)
		delete(h.byID, id)
	}
	h.mu.Unlock()
	for _, p := range procs {
		killIfRunning(p)
	}
}

func (h *terminalHost) get(id string) (*terminalProc, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.byID[id]
	if !ok {
		return nil, fmt.Errorf("unknown terminal %q", id)
	}
	return p, nil
}

func (p *terminalProc) reap() {
	err := p.cmd.Wait()
	p.waitOnce.Do(func() {
		exit := acp.WaitForTerminalExitResponse{}
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				if status, ok := ee.Sys().(syscall.WaitStatus); ok {
					if status.Exited() {
						code := status.ExitStatus()
						exit.ExitCode = &code
					} else if status.Signaled() {
						sig := status.Signal().String()
						exit.Signal = &sig
					}
				}
			}
		} else {
			code := 0
			exit.ExitCode = &code
		}
		p.exit = exit
		close(p.done)
	})
}

// maxTerminalBuffer caps the per-terminal retained output, bounding the memory
// an agent-supplied OutputByteLimit can pin regardless of the requested value.
const maxTerminalBuffer = 16 * 1024 * 1024

// limitedBuffer retains the last N bytes of output.
type limitedBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	if limit <= 0 {
		limit = 1024 * 1024
	}
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, _ := b.buf.Write(p)
	// Compact only once the buffer overshoots 2× the limit: compacting on
	// every write past the cap is an O(limit) memmove per chunk (a chatty
	// command turns 100MB of output into gigabytes of copying). Amortized this
	// way each byte is moved at most once per `limit` bytes written.
	if b.buf.Len() > 2*b.limit {
		// Drop from the front at a byte boundary (may split UTF-8; acceptable for tool output).
		extra := b.buf.Len() - b.limit
		data := b.buf.Bytes()
		b.buf.Reset()
		_, _ = b.buf.Write(data[extra:])
		b.truncated = true
	} else if b.buf.Len() > b.limit {
		b.truncated = true
	}
	return n, nil
}

func (b *limitedBuffer) Snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Trim lazily on read: Write defers compaction until 2× the limit, but the
	// agent's OutputByteLimit contract is on what we return.
	data := b.buf.Bytes()
	if len(data) > b.limit {
		data = data[len(data)-b.limit:]
	}
	return string(data), b.truncated
}

var _ io.Writer = (*limitedBuffer)(nil)

// buildTerminalCmd constructs an *exec.Cmd that matches how agents send terminals.
func buildTerminalCmd(command string, args []string) *exec.Cmd {
	command = strings.TrimSpace(command)
	if len(args) > 0 {
		return exec.Command(command, args...)
	}
	if command == "" {
		return exec.Command("/bin/bash", "-lc", "true")
	}
	// Single token with no shell metacharacters (e.g. /usr/bin/env or ls) —
	// run directly. Anything shell-ish ("a|b", "x;y", globs, $vars) must go
	// through the shell even without whitespace.
	if !strings.ContainsAny(command, " \t;|&<>$`\\\"'*?[](){}~#\n") {
		return exec.Command(command)
	}
	// Full shell line: prefer bash -lc so quoting and builtins work.
	return exec.Command(shellPath(), "-lc", command)
}

// shellPath resolves bash for shell-line terminals, tolerating hosts where it
// is not at /bin/bash (NixOS, minimal images).
func shellPath() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}
