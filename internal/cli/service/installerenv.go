package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// The installer's per-run options, as environment variables setup-service
// reads (MADR 0054 PLAN S5d, gap G3). The library's installer runs
// setup-service from an after_install hook whose arguments are fixed in the
// release spec, so options an operator chose per install travel in the
// environment. They are install.sh's --no-linger, --force-service and
// --with-relay-service (scripts/install.sh:1039-1041).
const (
	// EnvNoLinger is --no-linger.
	EnvNoLinger = "MCREMOTE_NO_LINGER"
	// EnvForceService is --force: rewrite a definition with other content.
	EnvForceService = "MCREMOTE_FORCE_SERVICE"
	// EnvWithRelayService makes mcremote's setup-service also set up mcrelay
	// (MADR 0054 amendment S5-1).
	EnvWithRelayService = "MCREMOTE_WITH_RELAY_SERVICE"
)

// EnvFlag reads one of the variables above: "1" turns it on, and unset or
// "0" leaves it off. Any other value is refused, naming the variable, rather
// than guessed at.
func EnvFlag(name string) (bool, error) {
	v, ok := os.LookupEnv(name)
	switch {
	case !ok, v == "0":
		return false, nil
	case v == "1":
		return true, nil
	}
	return false, fmt.Errorf("%s=%q: set it to 1 to turn the option on, or 0 or unset to leave it off", name, v)
}

// ApplyInstallerEnv turns on opts.NoLinger and opts.Force when their
// variables say so. A flag already on stays on.
func ApplyInstallerEnv(opts Options) (Options, error) {
	noLinger, err := EnvFlag(EnvNoLinger)
	if err != nil {
		return opts, err
	}
	force, err := EnvFlag(EnvForceService)
	if err != nil {
		return opts, err
	}
	opts.NoLinger = opts.NoLinger || noLinger
	opts.Force = opts.Force || force
	return opts, nil
}

// SetupRelayBeside sets up mcrelay after mcremote's setup succeeded, from the
// mcrelay binary in the same folder as the mcremote one the service runs, as
// install.sh did with --with-relay-service (:706-718). It takes mcremote's
// enable, start, linger and force choices; mcrelay's own options are its
// defaults, as `mcrelay setup-service` with no flags would use. relay is the
// relay's health wiring, so its start is confirmed with its own /healthz probe,
// as `mcrelay setup-service` confirms it (MADR 0207 D40). When the relay fails,
// mcremote is left as it is, and the error says so.
func SetupRelayBeside(remote Result, base Options, relay HealthOptions) (Result, error) {
	exe := "mcrelay"
	if runtime.GOOS == "windows" || installOS == "windows" {
		exe += ".exe"
	}
	bin := filepath.Join(filepath.Dir(remote.Binary), exe)
	if !isExecutableFile(bin) {
		return Result{}, fmt.Errorf("%s=1, but there is no mcrelay beside mcremote at %s; mcremote is set up, mcrelay is not", EnvWithRelayService, bin)
	}
	res, err := Setup(Options{
		Product:  "mcrelay",
		Binary:   bin,
		UnitDir:  base.UnitDir,
		Force:    base.Force,
		NoEnable: base.NoEnable,
		NoStart:  base.NoStart,
		NoLinger: base.NoLinger,
		Health:   relay,
	})
	if err != nil {
		return res, fmt.Errorf("mcremote is set up; mcrelay is not: %w", err)
	}
	return res, nil
}
