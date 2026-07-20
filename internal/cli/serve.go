package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the mcremote daemon",
		Long: `Run the mcremote WebSocket control plane in the foreground.

For a managed background process on Linux, prefer:
  mcremote setup-service --force
`,
		Example: serveExample,
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
			}
			if cmd.Flags().Changed("tls") {
				// Legacy switch: --tls=false forces off, --tls=true re-resolves
				// the mode (letsencrypt when a domain + email are configured).
				cfg.TLS = cfg.TLS.WithEnabled(enableTLS)
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

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger.Info("starting mcremote",
				"version", version,
				"listen", cfg.Addr(),
				"scheme", cfg.TLS.Scheme(),
				"data_dir", cfg.DataDir,
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
