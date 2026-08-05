// Package tcc detects macOS Full Disk Access denials for the daemon's own
// identity (MADR 0069 D5). There is no API to query or request TCC state;
// the sanctioned pattern is probing a known-protected path and classifying
// EPERM-despite-existence as "no grant". The probe is a lower-bound signal:
// success on one protected folder does not guarantee every protected
// location (iCloud, external volumes) — the runbook says so.
package tcc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// ProbeResult classifies the daemon's protected-folder access.
type ProbeResult int

const (
	// NotApplicable means the host is not macOS.
	NotApplicable ProbeResult = iota
	// Granted means the probe path stat succeeded — FDA (or a per-folder
	// grant) covers the daemon for at least this location.
	Granted
	// Denied means EPERM on a folder every macOS account has — the TCC
	// shape. Sessions under Documents/Desktop/Downloads will fail.
	Denied
	// Unknown means the probe path is missing or errored in a non-TCC way.
	Unknown
)

// Probe checks ~/Downloads — present on every macOS account and
// TCC-protected — under the current process identity. Cheap (one stat);
// callers may re-run per invocation, results are deliberately not cached
// (grants change under the daemon at any time).
func Probe() ProbeResult {
	return probeWith(runtime.GOOS, os.UserHomeDir, os.Stat)
}

func probeWith(goos string, homeFn func() (string, error), statFn func(string) (os.FileInfo, error)) ProbeResult {
	if goos != "darwin" {
		return NotApplicable
	}
	home, err := homeFn()
	if err != nil || home == "" {
		return Unknown
	}
	_, err = statFn(filepath.Join(home, "Downloads"))
	switch {
	case err == nil:
		return Granted
	case errors.Is(err, fs.ErrPermission):
		return Denied
	default:
		return Unknown
	}
}
