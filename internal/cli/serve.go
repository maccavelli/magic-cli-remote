package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/daemon"
	"github.com/maccavelli/magic-cli-remote/internal/logging"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var listenHost string
	var listenPort int
	var dataDir string
	var enableTLS bool
	var relayURL, relayHostID, relaySecret string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the mcremote daemon",
		Long: `Run the mcremote WebSocket control plane in the foreground.

For a managed background process on Linux, prefer:
  mcremote setup-service --force
`,
		Example: serveExample,
		// serve takes no positional args; reject them so a stray word
		// (a mistyped flag or subcommand) fails loudly instead of being ignored.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.LoadOptions{
				ConfigFile: cfgFile,
				Flags:      cmd.Flags(),
			})
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("listen-host") {
				cfg.Listen.Host = listenHost
			}
			if cmd.Flags().Changed("listen-port") {
				cfg.Listen.Port = listenPort
			}
			if cmd.Flags().Changed("data-dir") {
				cfg.DataDir = dataDir
				if err := cfg.RecomputePaths(); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("tls") {
				// Legacy switch: --tls=false forces off, --tls=true re-resolves
				// the mode (letsencrypt when a domain + email are configured).
				cfg.TLS = cfg.TLS.WithEnabled(enableTLS)
			}
			if cmd.Flags().Changed("relay-url") {
				cfg.Relay.URL = relayURL
			}
			if cmd.Flags().Changed("relay-host-id") {
				cfg.Relay.HostID = relayHostID
			}
			if cmd.Flags().Changed("relay-secret") {
				cfg.Relay.Secret = relaySecret
			}
			if logLevel != "" {
				cfg.Log.Level = logLevel
			}
			if logFormat != "" {
				cfg.Log.Format = logFormat
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			cfg.TLS = cfg.TLS.Normalized()

			logger := logging.Setup(logging.Options{
				Level:  cfg.Log.Level,
				Format: cfg.Log.Format,
			})

			ctx, stop := signal.NotifyContext(cmd.Context(), shutdownSignals()...)
			defer stop()
			// After the first signal starts graceful shutdown, restore default
			// signal handling so a second Ctrl-C force-quits instead of being
			// swallowed while the 10s drain runs.
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("signal restorer panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
					}
				}()
				<-ctx.Done()
				stop()
			}()

			memLimit, memLimitSource := applyMemoryLimit()

			logger.Info("starting mcremote",
				"version", version,
				"listen", cfg.Addr(),
				"scheme", cfg.TLS.Scheme(),
				"data_dir", cfg.DataDir,
				"mem_limit_bytes", memLimit,
				"mem_limit_source", memLimitSource,
			)

			if err := daemon.Run(ctx, daemon.Options{
				Config:  cfg,
				Version: version,
				Log:     logger,
			}); err != nil {
				return fmt.Errorf("daemon: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&listenHost, "listen-host", "", "listen host (overrides config)")
	cmd.Flags().IntVar(&listenPort, "listen-port", 0, "listen port (overrides config)")
	cmd.Flags().StringVar(&relayURL, "relay-url", "", "mcrelay base URL (e.g. wss://relay.example.com); env MCREMOTE_RELAY_URL")
	cmd.Flags().StringVar(&relayHostID, "relay-host-id", "", "public host id for mcrelay registration; env MCREMOTE_RELAY_HOST_ID")
	cmd.Flags().StringVar(&relaySecret, "relay-secret", "", "mcrelay registration secret; env MCREMOTE_RELAY_SECRET")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory (overrides config)")
	cmd.Flags().BoolVar(&enableTLS, "tls", true,
		"serve HTTPS/WSS; --tls=false for plaintext (same as --tls-mode off)")
	cmd.Flags().String("tls-mode", "",
		"letsencrypt|selfsigned|off (default: letsencrypt when a domain + email are set, else selfsigned)")
	cmd.Flags().StringSlice("tls-domain", nil,
		"DNS name(s) to request from the ACME CA; the first is advertised to phones")
	cmd.Flags().String("tls-email", "", "ACME account email (required for letsencrypt)")
	cmd.Flags().String("tls-acme-directory", "", "ACME directory URL (default: Let's Encrypt production)")
	cmd.Flags().Bool("tls-acme-staging", false, "use the Let's Encrypt staging CA (untrusted certs, high rate limits)")
	cmd.Flags().String("tls-route53-zone-id", "", "Route 53 hosted zone ID for the DNS-01 challenge")
	cmd.Flags().String("tls-route53-region", "", "AWS region for the Route 53 API")
	cmd.Flags().String("tls-route53-profile", "", "AWS shared-config profile for the Route 53 API")
	return cmd
}

// defaultMemoryLimit is the soft heap ceiling the daemon runs under.
//
// Sized against what the transcript budget can hold: 384 MiB of global
// retention (internal/session.globalBudgetBytes) plus the daemon's own ~51 MB
// working set, with room for the paging and persistence copies on top. The
// host this was measured on has 32 GB and runs a single kilo engine at 1,450 MB,
// so the daemon is not the process competing for that memory — the limit exists
// so a retention bug degrades into GC pressure rather than an OOM on somebody's
// laptop (MADR 0138, amendment step 4).
const defaultMemoryLimit = 1 << 30

// applyMemoryLimit sets the soft heap ceiling unless the operator already set
// GOMEMLIMIT, whose value wins. It reports the effective limit and where it
// came from, so the number is in the log rather than only in this file.
func applyMemoryLimit() (int64, string) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		// The runtime already parsed and applied it at startup. Read it back
		// rather than re-parsing the string ourselves.
		return debug.SetMemoryLimit(-1), "GOMEMLIMIT"
	}
	debug.SetMemoryLimit(defaultMemoryLimit)
	return defaultMemoryLimit, "default"
}
