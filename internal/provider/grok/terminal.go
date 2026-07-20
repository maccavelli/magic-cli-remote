package grok

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
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

type terminalHost struct {
	mu   sync.Mutex
	byID map[string]*terminalProc
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
	buf := newLimitedBuffer(limit)
	cmd.Stdout = buf
	cmd.Stderr = buf
	procutil.SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
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
	if p.cmd.Process != nil {
		_ = procutil.KillProcessGroup(p.cmd.Process)
	}
	return acp.KillTerminalResponse{}, nil
}

func (h *terminalHost) Release(_ context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	h.mu.Lock()
	p, ok := h.byID[params.TerminalId]
	if ok {
		delete(h.byID, params.TerminalId)
	}
	h.mu.Unlock()
	if ok && p.cmd.Process != nil {
		_ = procutil.KillProcessGroup(p.cmd.Process)
	}
	return acp.ReleaseTerminalResponse{}, nil
}

func (h *terminalHost) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.byID {
		if p.cmd.Process != nil {
			_ = procutil.KillProcessGroup(p.cmd.Process)
		}
		delete(h.byID, id)
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
	if b.buf.Len() > b.limit {
		// Drop from the front at a byte boundary (may split UTF-8; acceptable for tool output).
		extra := b.buf.Len() - b.limit
		data := b.buf.Bytes()
		b.buf.Reset()
		_, _ = b.buf.Write(data[extra:])
		b.truncated = true
	}
	return n, nil
}

func (b *limitedBuffer) Snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String(), b.truncated
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
	// Single token path (e.g. /usr/bin/env or ls) — run directly.
	if !strings.ContainsAny(command, " \t") {
		return exec.Command(command)
	}
	// Full shell line: prefer bash -lc so quoting and builtins work.
	return exec.Command("/bin/bash", "-lc", command)
}
