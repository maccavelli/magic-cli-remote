//go:build windows

package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// safeBatchArg matches the characters an argument may contain when it must
// pass through cmd.exe. The rejected set is deliberately broad: & | < > ^ %
// ! " ( ) are all cmd.exe metacharacters, and % and ! survive quoting under
// delayed expansion, so quoting is NOT treated as a substitute for rejection
// (MADR 0116 D11).
const safeBatchChars = `ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 ._:\/@=+,'-`

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

// command builds the *exec.Cmd, routing a batch shim through cmd.exe /c.
func command(ctx context.Context, r Resolved, args ...string) (*exec.Cmd, error) {
	if r.Kind == KindBatch {
		for _, a := range args {
			if bad, ok := unsafeBatchChar(a); ok {
				return nil, fmt.Errorf(
					"%w: %q contains %q — point the provider's `bin` at the real executable "+
						"instead of the %s shim", ErrUnsafeBatchArgs, a, bad, filepath.Ext(r.Path))
			}
		}
		full := append([]string{"/c", r.Path}, args...)
		if n := commandLineLen(comspec(), full); n > maxCommandLine {
			return nil, fmt.Errorf("%w: %d characters", ErrCommandLineTooLong, n)
		}
		return exec.CommandContext(ctx, comspec(), full...), nil
	}
	if n := commandLineLen(r.Path, args); n > maxCommandLine {
		return nil, fmt.Errorf("%w: %d characters", ErrCommandLineTooLong, n)
	}
	return exec.CommandContext(ctx, r.Path, args...), nil
}

// unsafeBatchChar reports the first character cmd.exe would reinterpret.
func unsafeBatchChar(arg string) (string, bool) {
	for _, r := range arg {
		if !strings.ContainsRune(safeBatchChars, r) {
			return string(r), true
		}
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
