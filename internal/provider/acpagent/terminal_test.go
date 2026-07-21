package acpagent

import (
	"strings"
	"testing"
)

func TestBuildTerminalCmd_withArgs(t *testing.T) {
	cmd := buildTerminalCmd("/bin/echo", []string{"hi"})
	// Path may be resolved through PATH lookup; only its basename is stable.
	if !strings.HasSuffix(cmd.Path, "echo") {
		t.Fatalf("path=%q", cmd.Path)
	}
	if len(cmd.Args) < 2 || cmd.Args[len(cmd.Args)-1] != "hi" {
		t.Fatalf("args=%v", cmd.Args)
	}
}

func TestBuildTerminalCmd_shellLine(t *testing.T) {
	line := "/bin/bash -lc 'echo test'"
	cmd := buildTerminalCmd(line, nil)
	// Must not use the whole line as argv0.
	if cmd.Args[0] == line {
		t.Fatalf("argv0 is full line: %v", cmd.Args)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "-lc") {
		t.Fatalf("expected bash -lc wrapper, got %v", cmd.Args)
	}
	if !strings.Contains(joined, line) {
		t.Fatalf("expected original line in -lc arg, got %v", cmd.Args)
	}
}

func TestBuildTerminalCmd_singleBinary(t *testing.T) {
	cmd := buildTerminalCmd("true", nil)
	if len(cmd.Args) != 1 {
		t.Fatalf("args=%v", cmd.Args)
	}
}
