package tailnet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestDetectIPv4Success(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	execCommand = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "TAILSCALE_OUTPUT=100.64.0.1"}
		return cmd
	}

	ip := detectIPv4()
	if ip != "100.64.0.1" {
		t.Errorf("got %q, want 100.64.0.1", ip)
	}
}

func TestDetectIPv4MultiLine(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	execCommand = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "TAILSCALE_OUTPUT=100.64.0.1\n100.64.0.2"}
		return cmd
	}

	ip := detectIPv4()
	if ip != "100.64.0.1" {
		t.Errorf("got %q, want 100.64.0.1", ip)
	}
}

func TestDetectIPv4Error(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	execCommand = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "TAILSCALE_ERROR=1"}
		return cmd
	}

	ip := detectIPv4()
	if ip != "" {
		t.Errorf("got %q, want empty", ip)
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("TAILSCALE_ERROR") == "1" {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "%s", os.Getenv("TAILSCALE_OUTPUT"))
	os.Exit(0)
}
