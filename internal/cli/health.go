package cli

import (
	"context"
	"fmt"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// shutdownGrace is how long an update on Windows waits for the daemon to exit
// after asking it to, before ending the task. The daemon's own drain is
// bounded at about 12 seconds (internal/daemon gracefulDrain); the backend's
// poll timeout, 60 seconds, must stay longer (MADR 0054 A2).
const shutdownGrace = 30 * time.Second

// updateHealth is mcremote's health wiring for update: the probe pings the
// daemon's admin socket, and on Windows the update asks the daemon to shut
// down before ending its task (MADR 0054 A1, A2; amendment S8).
func updateHealth() service.HealthOptions {
	return service.HealthOptions{
		Probe:       adminPingProbe,
		RequestStop: adminShutdownRequest,
		StopGrace:   shutdownGrace,
	}
}

// loadServiceConfig loads the configuration of the daemon run with args: the
// file its --config names, or the default one, with its --data-dir, as serve
// loads them. It is a variable for tests.
var loadServiceConfig = func(args service.ServeArgs) (config.Config, error) {
	cfg, err := config.Load(config.LoadOptions{ConfigFile: args.ConfigPath})
	if err != nil {
		return config.Config{}, err
	}
	if args.DataDir != "" {
		abs, err := config.AbsDataDir(args.DataDir)
		if err != nil {
			return config.Config{}, err
		}
		cfg.DataDir = abs
		if err := cfg.RecomputePaths(); err != nil {
			return config.Config{}, err
		}
	}
	return cfg, nil
}

// adminPingProbe is ready when the daemon's admin socket answers ping with
// OK. A call that fails is an error, which the backend keeps and reports if
// the wait times out; an answer that is not OK is an observation, not ready.
func adminPingProbe(_ context.Context, args service.ServeArgs) (libservice.Health, error) {
	cfg, err := loadServiceConfig(args)
	if err != nil {
		return libservice.Health{}, fmt.Errorf("load the service's configuration: %w", err)
	}
	resp, err := admin.Call(cfg.Paths.AdminSocket, admin.Request{Op: admin.OpPing})
	if err != nil && resp.Error == "" {
		return libservice.Health{}, err
	}
	h := libservice.Health{Ready: resp.OK, Detail: "admin ping on " + cfg.Paths.AdminSocket}
	if !resp.OK {
		h.Detail += ": " + resp.Error
	}
	return h, nil
}

// adminShutdownRequest asks the daemon run with args to shut down, over its
// admin socket.
func adminShutdownRequest(_ context.Context, args service.ServeArgs) error {
	cfg, err := loadServiceConfig(args)
	if err != nil {
		return fmt.Errorf("load the service's configuration: %w", err)
	}
	return admin.RequestShutdown(cfg.Paths.AdminSocket)
}
