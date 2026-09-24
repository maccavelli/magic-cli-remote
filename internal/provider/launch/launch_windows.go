//go:build windows

package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// maxCommandLineBatch is cmd.exe's own line limit, four times smaller than
// [maxCommandLineNative]. Declared here, not in launch.go, because only
// Windows uses it (staticcheck U1000 on linux and darwin, MADR 0169 D20).
// It is the one that applies to a shim, because cmd.exe parses that line before
// anything else sees it — so checking a batch invocation against the
// CreateProcessW number, as this package did until MADR 0159 D25, checks it
// against a ceiling it can pass while still being too long (0159 F37).
const maxCommandLineBatch = 8191

// batchFlags are the cmd.exe switches every shim invocation carries, in front
// of /c. Each one removes a way the interpreter could change what runs
// (MADR 0159 D23):
//
//	/d      skip AutoRun. HKCU\Software\Microsoft\Command Processor\AutoRun runs
//	        before the given command on every cmd.exe start, and HKCU is
//	        user-writable, so without /d a persistence entry there would execute
//	        on every provider launch (0159 F34).
//	/s      make outer-quote handling deterministic: the first and last quote of
//	        the line are stripped and everything between is taken literally,
//	        instead of cmd.exe applying its "is this a whole command?" heuristics.
//	/v:off  delayed expansion off, so `!VAR!` is inert (measured: probe 13).
var batchFlags = []string{"/d", "/s", "/v:off", "/c"}

// resolve finds bin on PATH and classifies it.
//
// exec.LookPath already honours PATHEXT (os/exec/lp_windows.go), so an
// npm-installed CLI resolves to its .cmd shim rather than the extensionless
// shell script beside it.
func resolve(bin string) (Resolved, error) {
	p, err := exec.LookPath(bin)
	if err != nil {
		return Resolved{}, err
	}
	kind := KindNative
	switch strings.ToLower(filepath.Ext(p)) {
	case ".bat", ".cmd":
		kind = KindBatch
	}
	return Resolved{Path: p, Kind: kind}, nil
}

// command builds the *exec.Cmd, routing a batch shim through a cmd.exe whose
// behaviour is pinned and whose command line this package writes itself.
func command(ctx context.Context, r Resolved, args ...string) (*exec.Cmd, error) {
	if r.Kind != KindBatch {
		if n := commandLineLen(r.Path, args); n > maxCommandLineNative {
			return nil, fmt.Errorf("%w: %d characters, CreateProcessW accepts %d",
				ErrCommandLineTooLong, n, maxCommandLineNative)
		}
		return procutil.Command(ctx, r.Path, args...), nil
	}

	// The shim path is quoted into the same line as the arguments, so it is
	// subject to the same rule. A path is not attacker-chosen as often as an
	// argument, but `%` in one would expand just the same.
	if bad, ok := unrepresentableForBatch(r.Path); ok {
		return nil, fmt.Errorf("%w: the shim path %q contains %s", ErrUnsafeBatchArgs, r.Path, bad)
	}
	for _, a := range args {
		if bad, ok := unrepresentableForBatch(a); ok {
			return nil, fmt.Errorf(
				"%w: %q contains %s — point the provider's `bin` at the real executable "+
					"instead of the %s shim", ErrUnsafeBatchArgs, a, bad, filepath.Ext(r.Path))
		}
	}

	line := batchCommandLine(comspec(), r.Path, args)
	if n := len(line) + 1; n > maxCommandLineBatch {
		return nil, fmt.Errorf("%w: %d characters, cmd.exe accepts %d",
			ErrCommandLineTooLong, n, maxCommandLineBatch)
	}

	// Args stays the logical argv, for logs and tests; CmdLine is what Windows
	// actually receives, because os/exec's own escaping is wrong for cmd.exe.
	cmd := procutil.Command(ctx, comspec(), append(append([]string{}, batchFlags...),
		append([]string{r.Path}, args...)...)...)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = line
	return cmd, nil
}

// batchCommandLine assembles the exact line Windows receives:
//
//	cmd.exe /d /s /v:off /c ""<shim>" "<arg>" ..."
//
// The outer pair is what /s strips; each element carries its own pair, which is
// what makes & | < > ^ ( ) and spaces literal (measured: probe 13).
func batchCommandLine(interpreter, shim string, args []string) string {
	var b strings.Builder
	b.WriteString(interpreter)
	for _, f := range batchFlags {
		b.WriteString(" " + f)
	}
	b.WriteString(` ""`)
	b.WriteString(shim)
	b.WriteString(`"`)
	for _, a := range args {
		b.WriteString(` "` + a + `"`)
	}
	b.WriteString(`"`)
	return b.String()
}

// unrepresentableForBatch reports the first part of arg that cmd.exe would act
// on even inside quotes, and therefore cannot be passed at all.
//
// This is a refusal of four shapes, not an allowlist of permitted characters.
// The allowlist it replaces (MADR 0116 D11) was simultaneously too strict and
// too blunt: it refused `(` and `)`, so an ordinary `C:\Program Files (x86)\…`
// argument could not be passed, while offering no protection at all because
// nothing called it (0159 F29/F31).
//
// Rust's standard library takes the same approach for the same reason — it
// returns an error rather than guessing an escaping — after CVE-2024-24576.
func unrepresentableForBatch(arg string) (string, bool) {
	for _, r := range arg {
		switch r {
		case '"':
			// Ends the quoted run early; there is no escape cmd.exe honours.
			return `a double quote`, true
		case '%':
			// %VAR% expands inside double quotes too (measured: probes 12, 13).
			return `a percent sign`, true
		case '\r', '\n':
			// Cannot appear in a command line at all.
			return `a line break`, true
		}
	}
	// A trailing backslash meets the closing quote as \", which the shim's own
	// consumer — node, python, whatever the .cmd execs — unescapes back into a
	// literal quote, breaking out of the quoted run one layer down. cmd.exe and
	// CommandLineToArgvW disagree about backslashes, and no single spelling
	// satisfies both, so this is refused rather than guessed at.
	if strings.HasSuffix(arg, `\`) {
		return `a trailing backslash`, true
	}
	return "", false
}

// comspec returns the command interpreter, honouring %COMSPEC%.
func comspec() string {
	if v := os.Getenv("COMSPEC"); v != "" {
		return v
	}
	return "cmd.exe"
}

// commandLineLen approximates the assembled command line length.
func commandLineLen(path string, args []string) int {
	n := len(path) + 1
	for _, a := range args {
		n += len(a) + 3 // separator plus worst-case quoting
	}
	return n
}

// isExecutableFile reports whether path has an extension Windows will execute.
//
// Mode()&0o111 is always zero here: Go derives the mode from file attributes,
// so every regular file reports 0666 and the POSIX test rejects a perfectly
// good .exe. That is what made `mcremote setup-service` refuse its own binary
// on Windows (MADR 0116 F23c).
func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	for _, e := range pathExts() {
		if ext == e {
			return true
		}
	}
	return false
}

// pathExts returns the lowercased PATHEXT list, with the documented default
// when the variable is unset.
func pathExts() []string {
	v := os.Getenv("PATHEXT")
	if v == "" {
		v = ".COM;.EXE;.BAT;.CMD"
	}
	out := make([]string, 0, 8)
	for _, e := range strings.Split(strings.ToLower(v), ";") {
		if e == "" {
			continue
		}
		if e[0] != '.' {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}
