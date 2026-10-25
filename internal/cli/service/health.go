package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The health wiring of a managed update (MADR 0054 D5, A1, A2, A4, and
// amendment S8). The product supplies the check, because only it knows how to
// reach its daemon; this package supplies where the daemon is, by reading the
// options setup baked into the installed definition.

// ServeArgs are the options a service definition passes `serve` that decide
// where the daemon answers. An empty field was not baked in, and the daemon
// takes it from its configuration.
type ServeArgs struct {
	// ConfigPath is --config and DataDir is --data-dir.
	ConfigPath string
	DataDir    string
	// ListenHost and ListenPort are --listen-host and --listen-port.
	ListenHost string
	ListenPort int
}

// ServeProbe checks once whether the daemon run with args is ready.
type ServeProbe func(ctx context.Context, args ServeArgs) (libservice.Health, error)

// HealthOptions are a product's health wiring for its service backend.
type HealthOptions struct {
	// Probe, when set, is each backend's Options.Probe: an application check
	// the backend runs once the service manager reports a new instance.
	Probe ServeProbe
	// RequestStop, when set, asks the daemon run with args to shut down. It
	// is the Task Scheduler backend's Options.RequestStop, sent before the
	// task is ended so the daemon can drain.
	RequestStop func(ctx context.Context, args ServeArgs) error
	// StopGrace bounds the wait after RequestStop. It must be shorter than
	// the backend's poll timeout, 60 seconds.
	StopGrace time.Duration
}

// installedServeArgs reads product's installed definition. An error means it
// could not be read; a non-empty reason means it was read but its serve
// arguments could not be recovered, as for a definition edited by hand. It is
// a variable for tests.
var installedServeArgs = readServeArgs

// The folders setup writes a systemd user unit and a LaunchAgent into. They
// are variables for tests, which run on every OS.
var (
	systemdUserDir = func() (string, error) {
		return filepath.Join(xdgConfigHome(), "systemd", "user"), nil
	}
	launchAgentsDir = func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		return filepath.Join(home, "Library", "LaunchAgents"), nil
	}
)

// readServeArgs recovers the serve arguments from product's definition, with
// the refresh's own parsers, so the probe looks where the service runs.
func readServeArgs(product, unitName string) (ServeArgs, string, error) {
	if unitName == "" {
		var err error
		if unitName, err = ResolveUnitName(product); err != nil {
			return ServeArgs{}, "", err
		}
	}
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	var (
		opts Options
		why  string
		ok   bool
	)
	switch osName {
	case "linux":
		dir, err := systemdUserDir()
		if err != nil {
			return ServeArgs{}, "", err
		}
		body, err := os.ReadFile(filepath.Join(dir, unitName+".service"))
		if err != nil {
			return ServeArgs{}, "", fmt.Errorf("read the %s unit: %w", product, err)
		}
		var rec recovery
		rec, why, ok = recoverOptions(product, unitName, string(body))
		opts = rec.Opts
	case "darwin":
		label, err := LaunchdLabel(product, unitName)
		if err != nil {
			return ServeArgs{}, "", err
		}
		dir, err := launchAgentsDir()
		if err != nil {
			return ServeArgs{}, "", err
		}
		body, err := os.ReadFile(filepath.Join(dir, label+".plist"))
		if err != nil {
			return ServeArgs{}, "", fmt.Errorf("read the %s LaunchAgent: %w", product, err)
		}
		var rec recovery
		rec, why, ok = recoverPlistOptions(product, unitName, label, string(body))
		opts = rec.Opts
	case "windows":
		name := taskNameFor(product)
		existing, err := taskDefinitionOf(name)
		if err != nil {
			return ServeArgs{}, "", err
		}
		opts, why, ok = recoverTaskOptions(product, existing)
	default:
		return ServeArgs{}, "", fmt.Errorf("service health unsupported on %s", osName)
	}
	if !ok {
		return ServeArgs{}, why, nil
	}
	return ServeArgs{
		ConfigPath: opts.ConfigPath,
		DataDir:    opts.DataDir,
		ListenHost: opts.ListenHost,
		ListenPort: opts.ListenPort,
	}, "", nil
}

// backendProbe is h.Probe for product's backend. It reads the definition on
// each check, so it follows the one the update has just refreshed. A
// definition whose arguments cannot be recovered gets no application check:
// probing the defaults instead could fail every update of a service that does
// not run with them (MADR 0054 amendment S8, S1).
func (h HealthOptions) backendProbe(product, unitName string) libservice.HealthProbe {
	if h.Probe == nil {
		return nil
	}
	return func(ctx context.Context) (libservice.Health, error) {
		args, why, err := installedServeArgs(product, unitName)
		if err != nil {
			return libservice.Health{}, err
		}
		if why != "" {
			return libservice.Health{Ready: true, Detail: "no application check: the " + product + " definition " + why}, nil
		}
		return h.Probe(ctx, args)
	}
}

// backendRequestStop is h.RequestStop for the Task Scheduler backend, which
// passes the running instance's process ID; the request goes to the daemon's
// own socket instead. An error ends the grace at once, and the task is ended
// as it was before the request existed.
func (h HealthOptions) backendRequestStop(product, unitName string) func(context.Context, uint32) error {
	if h.RequestStop == nil {
		return nil
	}
	return func(ctx context.Context, _ uint32) error {
		args, why, err := installedServeArgs(product, unitName)
		if err != nil {
			return err
		}
		if why != "" {
			return fmt.Errorf("the %s task's serve arguments cannot be read back: the definition %s", product, why)
		}
		return h.RequestStop(ctx, args)
	}
}
