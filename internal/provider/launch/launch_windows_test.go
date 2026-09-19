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
	return writeShimBody(t, name, "@echo off\r\n")
}

// writeShimBody creates a batch shim with a body of the caller's choosing, for
// the tests that need to see what the shim actually received.
func writeShimBody(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
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

// TestCommandRoutesBatchThroughAPinnedCmd proves a shim is launched through
// cmd.exe with AutoRun, quote heuristics and delayed expansion all disabled, and
// that the line Windows receives is the one this package built — not os/exec's
// escaping of it (MADR 0159 D23).
func TestCommandRoutesBatchThroughAPinnedCmd(t *testing.T) {
	path := writeShim(t, "mcfakeengine.cmd")
	r := Resolved{Path: path, Kind: KindBatch}
	cmd, err := Command(context.Background(), r, "--model", "grok-4")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") {
		t.Errorf("cmd.Path = %q, want cmd.exe", cmd.Path)
	}

	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CmdLine == "" {
		t.Fatal("SysProcAttr.CmdLine is empty: os/exec would then escape the line by " +
			"CommandLineToArgvW's rules, which are not cmd.exe's (golang/go#68313)")
	}
	line := cmd.SysProcAttr.CmdLine

	// A22. /d is the flag whose absence changes nothing observable on a host with
	// no AutoRun entry — which is every host until it is the one that matters — so
	// it is asserted here rather than left to behaviour.
	for _, flag := range batchFlags {
		if !strings.Contains(line, " "+flag+" ") {
			t.Errorf("CmdLine %q is missing %s", line, flag)
		}
	}
	if want := `""` + path + `" "--model" "grok-4""`; !strings.Contains(line, want) {
		t.Errorf("CmdLine = %q, want it to contain %q", line, want)
	}

	// Args stays the logical argv, so logs and tests can still read it.
	want := []string{cmd.Path, "/d", "/s", "/v:off", "/c", path, "--model", "grok-4"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

// TestCommandRefusesOnlyWhatQuotingCannotFix is the security half of
// MADR 0159 D23, and it deliberately REPLACES the older assertion that every
// cmd.exe metacharacter is refused.
//
// Weakening a security assertion needs a reason, so here it is: the old
// allowlist protected nothing, because nothing ever called this function
// (0159 F29), and it refused `(` and `)`, so an ordinary
// `C:\Program Files (x86)\…` argument could not be passed at all (0159 F31).
// What replaces it is narrower but actually reached, and every character no
// longer refused is proven inert against a real shim by
// TestBatchArgumentsReachTheShimLiterally. Refusing a character is not
// protection; what arrives at the shim is.
func TestCommandRefusesOnlyWhatQuotingCannotFix(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}

	refused := map[string]string{
		"double quote":       `value"more`,
		"percent sign":       "%PATH%",
		"carriage return":    "value\rmore",
		"line feed":          "value\nmore",
		"trailing backslash": `C:\dir\`,
	}
	for name, arg := range refused {
		t.Run("refused_"+name, func(t *testing.T) {
			_, err := Command(context.Background(), r, "--prompt", arg)
			if !errors.Is(err, ErrUnsafeBatchArgs) {
				t.Fatalf("arg %q gave err = %v, want ErrUnsafeBatchArgs", arg, err)
			}
		})
	}

	// Everything cmd.exe acts on when unquoted, which quoting makes literal.
	accepted := []string{
		"a&calc", "a|b", "a<in", "a>out", "a^b", "a!PATH!", "(x86)",
		`C:\Program Files (x86)\tool\x`, "two words", "a;b", "a,b", "*.go", "a?b",
	}
	for _, arg := range accepted {
		t.Run("accepted_"+arg, func(t *testing.T) {
			if _, err := Command(context.Background(), r, "--prompt", arg); err != nil {
				t.Fatalf("arg %q rejected: %v", arg, err)
			}
		})
	}
}

// TestCommandTellsTheOperatorWhatToDo keeps the actionable half of the old
// error: a refusal names the remedy, not only the problem.
func TestCommandTellsTheOperatorWhatToDo(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}
	_, err := Command(context.Background(), r, "%PATH%")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "bin") {
		t.Errorf("error %q should tell the operator to set the provider's bin", err)
	}
	if !strings.Contains(err.Error(), "percent") {
		t.Errorf("error %q should name what it refused", err)
	}
}

// TestBatchArgumentsReachTheShimLiterally is acceptance criteria A20 and A21,
// and the test that would have caught MADR 0159 F30.
//
// It runs a real shim. Before D23, `a&calc` passed to an npm shim reached
// cmd.exe as a command separator and started Calculator (probe 12, measured on
// this host). Here the shim must report the argument verbatim, which it can only
// do if cmd.exe treated it as text.
func TestBatchArgumentsReachTheShimLiterally(t *testing.T) {
	// %* is the raw argument tail as cmd.exe passes it on, which is exactly what
	// an npm shim forwards to node.
	shim := writeShimBody(t, "mcechoargs.cmd", "@echo off\r\necho ARGS=[%*]\r\n")
	r := Resolved{Path: shim, Kind: KindBatch}

	for _, arg := range []string{
		"a&calc",
		"a|b",
		"a>nul",
		"a^b",
		"a!PATH!",
		`C:\Program Files (x86)\tool\x`,
		"two words",
	} {
		t.Run(arg, func(t *testing.T) {
			cmd, err := Command(context.Background(), r, arg)
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("running the shim failed: %v\n%s", err, out)
			}
			got := strings.TrimSpace(string(out))
			if want := `ARGS=["` + arg + `"]`; got != want {
				t.Errorf("the shim received %s, want %s — cmd.exe acted on the "+
					"argument instead of passing it through", got, want)
			}
		})
	}
}

// TestCommandAcceptsOrdinaryBatchArgs proves the rule is not so tight that
// normal provider argv is refused.
func TestCommandAcceptsOrdinaryBatchArgs(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}
	args := []string{"--model", "grok-4", "--cwd", `C:\Users\dev\project`, "--flag=a,b"}
	if _, err := Command(context.Background(), r, args...); err != nil {
		t.Fatalf("ordinary args rejected: %v", err)
	}
}

// TestCommandNativeSkipsBatchRules proves a real .exe is not subjected to the
// cmd.exe rules — only shims are, because only shims get an interpreter.
func TestCommandNativeSkipsBatchRules(t *testing.T) {
	r := Resolved{Path: `C:\bin\engine.exe`, Kind: KindNative}
	cmd, err := Command(context.Background(), r, "--prompt", "a & b", "%PATH%", `ends\`)
	if err != nil {
		t.Fatalf("native command rejected: %v", err)
	}
	if cmd.Path != r.Path {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, r.Path)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		t.Errorf("native command should let os/exec build the line, got CmdLine %q",
			cmd.SysProcAttr.CmdLine)
	}
}

// TestCommandRejectsOverlongBatchCommandLine pins cmd.exe's own 8191 ceiling on
// the shim path, not CreateProcessW's 32767: the interpreter parses the line
// first, so its limit is the one that applies (MADR 0159 D25/F37).
func TestCommandRejectsOverlongBatchCommandLine(t *testing.T) {
	r := Resolved{Path: `C:\shims\engine.cmd`, Kind: KindBatch}
	_, err := Command(context.Background(), r, strings.Repeat("a", maxCommandLineBatch))
	if !errors.Is(err, ErrCommandLineTooLong) {
		t.Fatalf("err = %v, want ErrCommandLineTooLong", err)
	}

	// The same length is fine for a native image, which has four times the room.
	native := Resolved{Path: `C:\bin\engine.exe`, Kind: KindNative}
	if _, err := Command(context.Background(), native,
		strings.Repeat("a", maxCommandLineBatch)); err != nil {
		t.Errorf("native command of batch-limit length rejected: %v", err)
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
