package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"time"
)

// setup-service --from-installer (MADR 0054 amendment S7, H1; PLAN S7a). The
// release's generated installer runs it after every install, with arguments
// fixed in the spec, so one command has to do what install.sh's branches did
// (scripts/install.sh:652-772): set the service up on a first install; on a
// later one, refresh a definition this install owns, keeping the options baked
// into it, and cycle it onto the new binary; and leave another install's
// definition alone. Plain setup-service refuses a definition that differs,
// which every baked option and every template change makes it do.

// The plans --from-installer can make.
const (
	// HookSetup is a first install: what plain setup-service does.
	HookSetup = "setup"
	// HookUpgrade refreshes and cycles a definition this install owns.
	HookUpgrade = "upgrade"
	// HookForeign is a definition that runs another folder's binary, left
	// alone.
	HookForeign = "foreign"
	// HookNone is mcrelay with no service: nothing to do.
	HookNone = "none"
)

// What an upgrade did with the service after the refresh, by update's rule
// (MADR 0054 PLAN S5f).
const (
	CycleRestarted   = "restarted"
	CycleStarted     = "started"
	CycleLeftStopped = "left stopped"
)

// HookPlan is what --from-installer does for one product.
type HookPlan struct {
	Product string
	Action  string
	// Kind and Name are the service manager and definition name looked in.
	Kind Kind
	Name string
	// Item is the definition found, for HookUpgrade and HookForeign.
	Item UninstallItem
	// Health is the product's health wiring, as Options carries it (MADR
	// 0207 D40).
	Health HealthOptions
}

// HookResult is what an upgrade did.
type HookResult struct {
	Refresh RefreshResult
	Cycle   string
	// Health is the verified start's outcome, when the service was started.
	Health   string
	Warnings []string
}

// PlanInstallerHook decides what --from-installer does for opts.Product. The
// order is install.sh's: a definition another install owns is left even with
// opts.Force (scripts/install.sh:666, before :672). opts.Force, which
// MCREMOTE_FORCE_SERVICE=1 sets, makes a definition this install owns a full
// setup --force, as the installer's --force-service did; it sets up no relay
// that was never asked for.
func PlanInstallerHook(opts Options) (HookPlan, error) {
	product := opts.Product
	if product == "" {
		product = "mcremote"
	}
	plan := HookPlan{Product: product, Action: HookSetup, Health: opts.Health}
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	kind, err := ResolveBackend(product)
	if err != nil {
		return plan, err
	}
	name := opts.UnitName
	if name == "" {
		if name, err = ResolveUnitName(product); err != nil {
			return plan, err
		}
	}
	if osName == "windows" {
		// The task is always named after the product (MADR 0159 D13).
		name = taskNameFor(product)
	}
	plan.Kind, plan.Name = kind, name
	item, found := findDefinition(kind, product, name)
	if !found {
		if product == "mcrelay" {
			// The relay's first service comes from MCREMOTE_WITH_RELAY_SERVICE
			// on mcremote's, or from mcrelay setup-service (MADR 0054 S5-1).
			plan.Action = HookNone
		}
		return plan, nil
	}
	plan.Item = item
	dir, err := uninstallExeDir()
	if err != nil {
		return plan, fmt.Errorf("resolve this binary's folder: %w", err)
	}
	if hookOwns(osName, product, item.Program, dir) {
		// A task still in install.ps1's folder is moved even with Force: a
		// plain setup --force would leave the old folders, and their older
		// binaries, on the User PATH.
		if !opts.Force || (osName == "windows" && inOldLayout(product, item.Program)) {
			plan.Action = HookUpgrade
		}
		return plan, nil
	}
	plan.Action = HookForeign
	if plan.Item.Reason == "" {
		plan.Item.Reason = "runs " + item.Program + ", not a program from " + dir
	}
	return plan, nil
}

// hookOwns reports whether a definition running program belongs to the
// install in dir: --uninstall's ownership rule. On Windows, install.ps1's
// per-product folder belongs to an install in the new folder too, because the
// refresh moves it there (MADR 0054 D9).
func hookOwns(osName, product, program, dir string) bool {
	if program == "" {
		return false
	}
	if sameDir(filepath.Dir(program), dir) {
		return true
	}
	return osName == "windows" && inOldLayout(product, program) && samePath(dir, newWindowsDir())
}

// hookTimeout bounds the cycle's service-manager calls.
var hookTimeout = 5 * time.Minute

// UpgradeFromInstaller refreshes the definition plan found, then cycles the
// service onto the new binary as update does: a running service is restarted,
// a stopped one is started only when it is enabled, and each start is
// confirmed by the backend that made it, with the product's probe (MADR 0054
// PLAN S5c, S5f; MADR 0207 D4, D40).
func UpgradeFromInstaller(plan HookPlan) (HookResult, error) {
	var res HookResult
	rr, err := RefreshUnit(Options{Product: plan.Product, UnitName: plan.Name}, RefreshOptions{})
	if err != nil {
		return res, fmt.Errorf("refresh the %s service: %w", plan.Product, err)
	}
	res.Refresh = rr
	b, err := setupBackend(plan.Kind, plan.Product, plan.Name, plan.Health)
	if err != nil {
		return res, fmt.Errorf("reach the %s service manager: %w", plan.Product, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	running, err := b.Running(ctx, plan.Product)
	if err != nil {
		return res, fmt.Errorf("read whether the %s service is running: %w", plan.Product, err)
	}
	if running {
		if err := b.Stop(ctx, plan.Product); err != nil {
			return res, fmt.Errorf("stop the %s service: %w", plan.Product, err)
		}
		res.Cycle = CycleRestarted
	} else {
		enabled, err := b.Enabled(ctx, plan.Product)
		if err != nil {
			return res, fmt.Errorf("read whether the %s service is enabled: %w", plan.Product, err)
		}
		if !enabled {
			// The operator disabled it: an install does not undo that.
			res.Cycle = CycleLeftStopped
			return res, nil
		}
		res.Cycle = CycleStarted
	}
	if err := b.Start(ctx, plan.Product); err != nil {
		return res, fmt.Errorf("start the %s service: %w", plan.Product, err)
	}
	vr, err := verifyStart(plan.Kind, hookStartResult(plan, b))
	res.Health = vr.Health
	res.Warnings = append(res.Warnings, vr.Warnings...)
	if err != nil {
		return res, err
	}
	if plan.Kind == KindWindowsTask {
		// The move kept the old copy the daemon was running; the restart
		// freed it.
		res.Warnings = append(res.Warnings, cleanOldFolders()...)
	}
	return res, nil
}

// hookStartResult is the Result the verified start reads: the backend b that
// stopped and started the service, which it waits on.
func hookStartResult(plan HookPlan, b Backend) Result {
	r := Result{UnitName: plan.Name, UnitPath: plan.Item.Path, Label: plan.Name, Scope: string(plan.Kind),
		started: &startedBy{b: b, product: plan.Product}}
	if plan.Kind == KindLaunchdAgent {
		if label, err := LaunchdLabel(plan.Product, plan.Name); err == nil {
			r.Label = label
		}
	}
	return r
}

// CheckFromInstaller refuses --from-installer with a flag that picks a mode
// the installer never runs. With --uninstall it is the installer's
// before_uninstall hook (UninstallFromInstaller; MADR 0054 amendment S9).
func CheckFromInstaller(refresh, remove, printOnly bool) error {
	if refresh || remove || printOnly {
		return fmt.Errorf("--from-installer cannot be combined with --refresh, --remove or --print-only")
	}
	return nil
}

// RunInstallerHook is --from-installer up to a first install. It applies the
// installer's variables, plans, and for anything but a first install does it
// and prints what it did. It reports whether it handled the command: a first
// install is the caller's, run as plain setup-service runs it.
func RunInstallerHook(out io.Writer, opts Options) (bool, error) {
	opts, err := ApplyInstallerEnv(opts)
	if err != nil {
		return true, err
	}
	plan, err := PlanInstallerHook(opts)
	if err != nil {
		return true, err
	}
	if plan.Action == HookSetup {
		return false, nil
	}
	var res HookResult
	if plan.Action == HookUpgrade {
		res, err = UpgradeFromInstaller(plan)
	}
	PrintInstallerHook(out, plan, res)
	// After whatever the hook did, as install.sh's advisories followed its
	// service step (MADR 0054 amendment S7d, A1 and A2).
	PrintInstallerAdvisories(out, plan.Product)
	return true, err
}

// PrintInstallerHook writes what --from-installer did when it was not a
// first install, which prints as plain setup-service does.
func PrintInstallerHook(out io.Writer, plan HookPlan, res HookResult) {
	switch plan.Action {
	case HookNone:
		fmt.Fprintf(out, "no %s service is set up, so there is nothing to refresh\n", plan.Product)
		fmt.Fprintf(out, "set one up with: %s setup-service, or %s=1 on mcremote's\n", plan.Product, EnvWithRelayService)
	case HookForeign:
		fmt.Fprintf(out, "left alone: %s %s service %s — %s\n", plan.Product, plan.Item.Kind, plan.Item.Path, plan.Item.Reason)
	case HookUpgrade:
		if res.Refresh.Verdict != "" {
			_ = PrintRefreshResult(out, res.Refresh, plan.Product, false)
		}
		switch res.Cycle {
		case CycleRestarted:
			fmt.Fprintf(out, "%s service restarted on the new binary\n", plan.Product)
		case CycleStarted:
			fmt.Fprintf(out, "%s service started on the new binary (it was stopped and is enabled)\n", plan.Product)
		case CycleLeftStopped:
			fmt.Fprintf(out, "%s service left stopped: it is not enabled\n", plan.Product)
		}
		printHealth(out, Result{Health: res.Health, Warnings: res.Warnings})
	}
}
