package provider

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// LogStderrTail warns with the engine stderr tail a provider is about to
// embed in a phone-visible start error (MADR 0069 D7). Before this, the
// same lines were logged only at Debug — invisible at the default level —
// so the phone could display text the operator could not grep. No-op on an
// empty tail; the tail is already bounded (lineRing, 20 lines).
func LogStderrTail(log *slog.Logger, bin, tail string) {
	if log == nil || tail == "" {
		return
	}
	log.Warn("engine stderr tail (phone-visible)",
		slog.String("bin", bin),
		slog.String("stderr", tail),
	)
}

// CWDPermissionError is an OS permission denial on the session working
// directory. It unwraps to the underlying errno (fs.ErrPermission) and
// carries the path so the daemon layer can compose platform-aware guidance
// without providers knowing the platform (0069 D4.5).
type CWDPermissionError struct {
	Path string
	Err  error
}

func (e *CWDPermissionError) Error() string {
	return fmt.Sprintf("cwd %q: %v", e.Path, e.Err)
}

func (e *CWDPermissionError) Unwrap() error { return e.Err }

// ResolveSessionCWD picks the absolute working directory for a new session:
// explicit cwd, else the provider's default_cwd, else the daemon user's
// home. Under a service manager the daemon's process cwd is an accident of
// the unit file, so empty always means home — never os.Getwd(). 0069 P1
// made this uniform across providers (codex and acphttp previously fell
// back to Getwd with no validation at all).
//
// Validation preserves the errno (0069 D4): a TCC/OS-policy denial must
// surface as fs.ErrPermission — previously any stat failure was reported
// as "not a directory", sending operators hunting for a typo instead of a
// privacy grant. statFn is injectable for tests; nil means os.Stat.
func ResolveSessionCWD(optsCWD, defaultCWD string, statFn func(string) (os.FileInfo, error)) (string, error) {
	if statFn == nil {
		statFn = os.Stat
	}
	cwd := optsCWD
	if cwd == "" {
		cwd = defaultCWD
	}
	if cwd == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir for session cwd: %w", err)
		}
		cwd = home
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	st, err := statFn(cwd)
	switch {
	case err != nil && errors.Is(err, fs.ErrPermission):
		// OS permission policy denied the stat itself (macOS TCC denies
		// like this for protected folders). The typed error keeps the errno
		// in the chain (permission_denied mapping) and carries the path so
		// the WS layer can compose platform-aware guidance (0069 D4.5) —
		// providers stay GOOS-free by design.
		return "", &CWDPermissionError{Path: cwd, Err: err}
	case err != nil && errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("cwd %q does not exist: %w", cwd, err)
	case err != nil:
		return "", fmt.Errorf("cwd %q: %w", cwd, err)
	case !st.IsDir():
		return "", fmt.Errorf("cwd %q is not a directory", cwd)
	}
	return cwd, nil
}
