//go:build windows

package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeShim creates a batch shim on PATH, the shape npm installs.
func writeShim(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

// TestResolveClassifiesBatchShim proves PATHEXT resolution finds the .cmd and
// that it is classified as needing cmd.exe (MADR 0116 F10).
func TestResolveClassifiesBatchShim(t *testing.T) {
	writeShim(t, "mcfakeengine.cmd")
	r, err := Resolve("mcfakeengine")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Kind != KindBatch {
		t.Errorf("Kind = %v, want batch", r.Kind)
	}
	if strings.ToLower(filepath.Ext(r.Path)) != ".cmd" {
		t.Errorf("resolved %q, want the .cmd shim", r.Path)
	}
}

// TestCommandRoutesBatchThroughCmd proves a shim is launched via cmd.exe /c,
// which CreateProcessW requires for a batch file.
func TestCommandRoutesBatchThroughCmd(t *testing.T) {
	path := writeShim(t, "mcfakeengine.cmd")
	r := Resolved{Path: path, Kind: KindBatch}
	cmd, err := Command(context.Background(), r, "--model", "grok-4")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") {
		t.Errorf("cmd.Path = %q, want cmd.exe", cmd.Path)
	}
	want := []string{cmd.Path, "/c", path, "--model", "grok-4"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

// TestCommandRejectsCmdMetacharacters is the security half of MADR 0116 D11:
// every character cmd.exe would reinterpret must be refused, not quoted.
func TestCommandRejectsCmdMetacharacters(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}
	for _, bad := range []string{"&", "|", "<", ">", "^", "%", `"`, "(", ")", "!"} {
		t.Run("meta_"+bad, func(t *testing.T) {
			_, err := Command(context.Background(), r, "--prompt", "value"+bad+"more")
			if !errors.Is(err, ErrUnsafeBatchArgs) {
				t.Fatalf("arg containing %q gave err = %v, want ErrUnsafeBatchArgs", bad, err)
			}
			if !strings.Contains(err.Error(), "bin") {
				t.Errorf("error %q should tell the operator to set the provider's bin", err)
			}
		})
	}
}

// TestCommandAcceptsOrdinaryBatchArgs proves the allowlist is not so tight
// that normal provider argv is refused.
func TestCommandAcceptsOrdinaryBatchArgs(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}
	args := []string{"--model", "grok-4", "--cwd", `C:\Users\dev\project`, "--flag=a,b"}
	if _, err := Command(context.Background(), r, args...); err != nil {
		t.Fatalf("ordinary args rejected: %v", err)
	}
}

// TestCommandNativeSkipsBatchRules proves a real .exe is not subjected to the
// cmd.exe allowlist — only shims are.
func TestCommandNativeSkipsBatchRules(t *testing.T) {
	r := Resolved{Path: `C:\bin\engine.exe`, Kind: KindNative}
	cmd, err := Command(context.Background(), r, "--prompt", "a & b")
	if err != nil {
		t.Fatalf("native command rejected: %v", err)
	}
	if cmd.Path != r.Path {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, r.Path)
	}
}

// TestCommandRejectsOverlongBatchCommandLine pins the 32767 ceiling on the
// cmd.exe path too, where the /c and shim path add to the total.
func TestCommandRejectsOverlongBatchCommandLine(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}
	_, err := Command(context.Background(), r, strings.Repeat("a", maxCommandLine))
	if !errors.Is(err, ErrCommandLineTooLong) {
		t.Fatalf("err = %v, want ErrCommandLineTooLong", err)
	}
}

// TestComspecHonoured proves %COMSPEC% is used when set.
func TestComspecHonoured(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	if got := comspec(); got != `C:\Windows\System32\cmd.exe` {
		t.Errorf("comspec() = %q", got)
	}
	t.Setenv("COMSPEC", "")
	if got := comspec(); got != "cmd.exe" {
		t.Errorf("comspec() fallback = %q, want cmd.exe", got)
	}
}
