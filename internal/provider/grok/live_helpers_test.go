//go:build live_grok

package grok_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// startACP launches grok 1.0.3 the way the daemon does (MADR 0081 Phase D):
// --no-auto-update --permission-mode default agent --no-leader stdio.
// Callers send extra JSON-RPC after initialize + session/new via send/waitID.
type acpProc struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	lines []string
	byID  map[int]map[string]any
}

func startACP(t *testing.T, extraNewMeta map[string]any) *acpProc {
	t.Helper()
	bin, err := exec.LookPath("grok")
	if err != nil {
		t.Skip("grok not in PATH")
	}
	cmd := exec.Command(bin, "--no-auto-update", "--permission-mode", "default", "agent", "--no-leader", "stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start grok: %v", err)
	}
	p := &acpProc{cmd: cmd, stdin: stdin, byID: map[int]map[string]any{}}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			p.mu.Lock()
			p.lines = append(p.lines, line)
			var msg map[string]any
			if json.Unmarshal([]byte(line), &msg) == nil {
				if id, ok := jsonNumber(msg["id"]); ok {
					p.byID[id] = msg
				}
			}
			p.mu.Unlock()
		}
	}()
	p.send(t, 1, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
		"clientInfo": map[string]any{"name": "mcremote-0081", "version": "0.0.1"},
	})
	if err := p.waitID(t, 1, 20*time.Second); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	p.sendNote(t, "notifications/initialized", map[string]any{})
	newParams := map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}
	if extraNewMeta != nil {
		newParams["_meta"] = extraNewMeta
	}
	p.send(t, 2, "session/new", newParams)
	if err := p.waitID(t, 2, 25*time.Second); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	return p
}

func (p *acpProc) send(t *testing.T, id int, method string, params any) {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}
}

func (p *acpProc) sendNote(t *testing.T, method string, params any) {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}
}

func (p *acpProc) waitID(t *testing.T, id int, d time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		msg, ok := p.byID[id]
		p.mu.Unlock()
		if ok {
			if errObj, has := msg["error"]; has {
				return fmt.Errorf("rpc %d error: %v", id, errObj)
			}
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for id %d", id)
}

func (p *acpProc) result(id int) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byID[id]
}

func (p *acpProc) stdoutContains(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, line := range p.lines {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

func jsonNumber(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}
