package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// SwapOpts controls service restart around a binary swap.
type SwapOpts struct {
	// Product is mcremote or mcrelay (service name).
	Product string
	// RestartService when true stops/starts the user unit around the swap.
	RestartService bool
	// WasActive is the pre-swap IsActive result; when RestartService and
	// WasActive, Start is called after success (or after restore).
	WasActive bool
	// CodesignIdentity when set re-signs the staged binary (0069 / 0065).
	CodesignIdentity string
	// Logger for progress lines (optional).
	Log func(string)
}

// SwapAndRestart renames staged → dest with .prev backup and optional service
// cycle (MADR 0065 F1). On failure after stop, restores .prev.
func SwapAndRestart(staged, dest string, opts SwapOpts) (err error) {
	log := opts.Log
	if log == nil {
		log = func(string) {}
	}
	if staged == "" || dest == "" {
		return fmt.Errorf("staged and dest paths required")
	}
	if _, err := os.Stat(staged); err != nil {
		return fmt.Errorf("staged binary: %w", err)
	}

	if id := strings.TrimSpace(opts.CodesignIdentity); id != "" {
		ident := "com.magiccliremote." + opts.Product
		// #nosec G204 — identity and paths are operator-controlled.
		cmd := exec.Command("codesign", "-f", "-s", id, "-i", ident, staged)
		if out, cerr := cmd.CombinedOutput(); cerr != nil {
			return fmt.Errorf("codesign staged binary: %w (%s)", cerr, strings.TrimSpace(string(out)))
		}
		log("re-signed staged binary with MC_CODESIGN_IDENTITY")
	}

	prev := dest + ".prev"
	_ = os.Remove(prev)

	stopped := false
	if opts.RestartService {
		active, aerr := service.IsActive(opts.Product)
		if aerr == nil && active {
			opts.WasActive = true
		}
		if opts.WasActive {
			log("stopping " + opts.Product + " service")
			if serr := service.Stop(opts.Product); serr != nil {
				log("stop: " + serr.Error() + " (continuing)")
			}
			stopped = true
			// Brief settle for ETXTBSY.
			time.Sleep(300 * time.Millisecond)
		}
	}

	defer func() {
		if err == nil {
			return
		}
		// Restore .prev if we moved dest away.
		if _, st := os.Stat(prev); st == nil {
			_ = os.Remove(dest)
			if rerr := os.Rename(prev, dest); rerr != nil {
				log("restore .prev failed: " + rerr.Error())
			} else {
				log("restored previous binary from .prev")
			}
		}
		if stopped && opts.WasActive {
			if serr := service.Start(opts.Product); serr != nil {
				log("restart after failure: " + serr.Error())
			}
		}
	}()

	// Move live → .prev if present.
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, prev); err != nil {
			return fmt.Errorf("rename dest→prev: %w", err)
		}
	}
	if err := os.Rename(staged, dest); err != nil {
		return fmt.Errorf("rename staged→dest: %w", err)
	}
	// Final name without .staging suffix if dest was the real path already.
	_ = os.Chmod(dest, 0o755)

	if stopped && opts.WasActive {
		log("starting " + opts.Product + " service")
		if err := service.Start(opts.Product); err != nil {
			return fmt.Errorf("start service: %w", err)
		}
		// Wait briefly for up.
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			ok, _ := service.IsActive(opts.Product)
			if ok {
				log(opts.Product + " is active")
				return nil
			}
			time.Sleep(400 * time.Millisecond)
		}
		return fmt.Errorf("%s did not become active after swap", opts.Product)
	}
	log("binary installed at " + dest + " (restart the service yourself if needed)")
	return nil
}

// ExecutableDir returns the directory of the current process binary.
func ExecutableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}
