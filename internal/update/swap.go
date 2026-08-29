package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SwapOpts controls service restart around a binary swap.
type SwapOpts struct {
	// Product is mcremote or mcrelay (service name).
	Product string
	// RestartService when true stops/starts the user unit around the swap.
	RestartService bool
	// WasActive is the pre-swap IsActive result; when RestartService and
	// WasActive (or heal-enabled-down), Start is called after success.
	WasActive bool
	// HealStart when true starts the unit after swap even if it was down
	// but managed (install-binary.sh want_up parity for enabled-but-stopped).
	HealStart bool
	// Service injects IsActive/Stop/Start. Nil uses no service cycle beyond
	// the RestartService flag being false.
	Service ServiceControl
	// Refresher reconciles the service definition after the swap and before
	// the start, so a release that fixes the unit can deliver it. Nil skips
	// the step entirely (MADR 0100).
	Refresher UnitRefresher
	// CodesignIdentity when set re-signs the staged binary (0069 / 0065).
	CodesignIdentity string
	// Log for progress lines (optional).
	Log func(string)
	// Sleep is a test seam for settle delays.
	Sleep func(time.Duration)
}

// SwapAndRestart renames staged → dest with .prev backup and optional service
// cycle (MADR 0065 F1). On failure after stop, restores .prev and restarts.
func SwapAndRestart(staged, dest string, opts SwapOpts) (err error) {
	log := opts.Log
	if log == nil {
		log = func(string) {}
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
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
	} else if opts.Product != "" {
		log("note: MC_CODESIGN_IDENTITY unset — macOS FDA grants may need re-approval after update (see docs/ops-macos-tcc.md)")
	}

	svc := opts.Service
	prev := dest + ".prev"
	// A leftover .prev that cannot be removed is fatal, not ignorable. On
	// Windows a file still open by another process refuses deletion, and the
	// rename at dest->prev below would then fail with the swap half-done
	// (MADR 0116 F9). Checked here, before the service stop, so the failure is
	// free of side effects.
	if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale backup %s: %w", prev, err)
	}

	// Declared before the rollback defer so it can undo a definition rewrite.
	var refreshed UnitRefresh

	wantUp := opts.WasActive || opts.HealStart
	stopped := false
	if opts.RestartService && svc != nil {
		active, aerr := svc.IsActive(opts.Product)
		if aerr == nil && active {
			wantUp = true
			opts.WasActive = true
		}
		if wantUp || active {
			log("stopping " + opts.Product + " service")
			if serr := svc.Stop(opts.Product); serr != nil {
				// Soft: unit may already be down (install-binary || true).
				log("stop: " + serr.Error() + " (continuing)")
			}
			stopped = true
			// No settle sleep. Stop now waits for the job to actually leave the
			// launchd domain (MADR 0125 D1), so the 300ms that used to stand in
			// for that wait is both unnecessary and — as error 37 proved on a
			// real Mac — insufficient. A constant cannot be right here (0125 C1).
		}
	}

	defer func() {
		if err == nil {
			return
		}
		if _, st := os.Stat(prev); st == nil {
			_ = os.Remove(dest)
			if rerr := os.Rename(prev, dest); rerr != nil {
				log("restore .prev failed: " + rerr.Error())
			} else {
				log("restored previous binary from .prev")
			}
		}
		if refreshed.Changed && opts.Refresher != nil {
			if rerr := opts.Refresher.RestoreUnit(opts.Product, refreshed); rerr != nil {
				log("restore previous service definition failed: " + rerr.Error())
			} else {
				log("restored previous service definition from " + refreshed.BackupPath)
			}
		}
		if stopped && wantUp && svc != nil {
			// The reported harm (MADR 0125 F4): this used to log and move on,
			// so an update that failed left the daemon down while the process
			// exited as though the rollback had worked. The two outcomes are
			// materially different to a user and must read differently.
			if serr := svc.Start(opts.Product); serr != nil {
				log("ROLLED BACK BUT NOT RUNNING: the previous binary is restored, " +
					"but " + opts.Product + " could not be started: " + serr.Error())
				log("start it with: " + opts.Product + " setup-service --force")
				err = fmt.Errorf("%w (rolled back; %s is NOT running: %v)",
					err, opts.Product, serr)
				return
			}
			log("rolled back; " + opts.Product + " is running again")
		}
	}()

	// dest->prev then staged->dest, deliberately: a RUNNING .exe on Windows
	// cannot be deleted or written, but it CAN be renamed. Turning this into a
	// delete-then-copy would break self-update there (MADR 0116 F9).
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, prev); err != nil {
			return fmt.Errorf("rename dest→prev: %w", err)
		}
	}
	if err := os.Rename(staged, dest); err != nil {
		return fmt.Errorf("rename staged→dest: %w", err)
	}
	// Inert on Windows: the execute bit is not a Windows concept, the .exe
	// extension is. Chmod there would only toggle the read-only attribute.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dest, 0o755)
	}

	// Refresh between the swap and the start: the new definition only exists
	// once the new binary is in place, and it must be what starts. Failures
	// here are reported and stepped over -- the binary swap is what `update`
	// promises, and a definition that could not be reconciled is not worth
	// rolling that back for (MADR 0100 D3).
	if opts.Refresher != nil {
		r, rerr := opts.Refresher.RefreshUnit(opts.Product, dest)
		if rerr != nil {
			log("service definition refresh failed: " + rerr.Error() + " (continuing)")
		} else {
			refreshed = r
			if r.Output != "" {
				log(r.Output)
			}
		}
	}

	if stopped && wantUp && svc != nil {
		log("starting " + opts.Product + " service")
		if err := svc.Start(opts.Product); err != nil {
			return fmt.Errorf("start service: %w", err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			ok, _ := svc.IsActive(opts.Product)
			if ok {
				log(opts.Product + " is active")
				_ = os.Remove(prev) // success: drop backup like install-binary cleanup
				return nil
			}
			sleep(50 * time.Millisecond)
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
