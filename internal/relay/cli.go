package relay

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/logging"
	"github.com/spf13/cobra"
)

// Set via Execute / main.
var (
	cliVersion = "dev"
	cliCommit  = "none"
	cliDate    = "unknown"
)

// Execute runs the mcrelay command tree.
func Execute(version, commit, date string) error {
	if version != "" {
		cliVersion = version
	}
	if commit != "" {
		cliCommit = commit
	}
	if date != "" {
		cliDate = date
	}
	return newRootCmd().Execute()
}

// VersionString returns the human-readable mcrelay version line.
func VersionString() string {
	return fmt.Sprintf("mcrelay %s (%s) %s", cliVersion, cliCommit, cliDate)
}

func newRootCmd() *cobra.Command {
	var (
		cfgFile   string
		logLevel  string
		logFormat string
		setupSvc  bool
		setupFl   setupServiceFlags
	)

	root := &cobra.Command{
		Use:   "mcrelay",
		Short: "Outbound join-plane relay for mcremote (MADR 0015)",
		Long: `mcrelay is a public-edge join router: hosts register outbound, phones join
by host_id, and mcrelay splices opaque WebSocket frames. It does not authenticate
devices, run agents, or see protocol-v1 plaintext when used with end-to-end TLS
to mcremote (inner hop).

Configuration: flags > MCRELAY_* env > config.yaml > defaults.
Default config: ` + ConfigPathHint() + `
Default data:   ` + DataDirHint() + `

All long flags use a double dash (--help, --config, --listen-host, …).
Short -h is help only. See docs/config-mcrelay.md.`,
		SilenceUsage:     true,
		SilenceErrors:    true,
		Version:          VersionString(),
		TraverseChildren: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			if setupSvc {
				return runSetupService(cmd, setupFl, cfgFile, logLevel, logFormat)
			}
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: $XDG_CONFIG_HOME/mcrelay/config.yaml or MCRELAY_CONFIG)")
	root.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level (debug|info|warn|error); env MCRELAY_LOG_LEVEL")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "", "log format (text|json); env MCRELAY_LOG_FORMAT")

	root.Flags().BoolVar(&setupSvc, "setup-service", false, "install and start the background service (Linux: systemd --user; macOS: launchd agent; same as setup-service subcommand)")
	bindSetupServiceFlags(root, &setupFl)

	root.AddCommand(newServeCmd(&cfgFile, &logLevel, &logFormat))
	root.AddCommand(newSetupServiceCmd(&cfgFile, &logLevel, &logFormat))
	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), VersionString())
			return err
		},
	}
}

func newServeCmd(cfgFile, logLevel, logFormat *string) *cobra.Command {
	var (
		listenHost              string
		listenPort              int
		dataDir                 string
		tlsMode                 string
		tlsCert                 string
		tlsKey                  string
		tlsDomains              []string
		tlsEmail                string
		tlsACMEDir              string
		tlsStaging              bool
		tlsHTTPPort             int
		tlsChallenge            string
		tlsR53Zone              string
		tlsR53Region            string
		tlsR53Profile           string
		allows                  []string
		allowLegacyTunnelSecret bool
		trustedProxies          []string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the mcrelay public edge",
		Long: `Start the join-plane listener (register / join / opaque splice).

Host allowlist is required: configure hosts in YAML, MCRELAY_HOSTS, and/or --allow.

TLS modes:
  letsencrypt — ACME via --tls-acme-challenge http-01 (default; port 80) or dns-01 (Route 53)
  files       — operator PEMs (--tls-cert / --tls-key)
  off         — plaintext (local tests only; logs a warning)

Empty tls.mode auto-selects: domains+email → letsencrypt; cert files → files; else off.`,
		Example: `  mcrelay serve --config ~/.config/mcrelay/config.yaml
  mcrelay serve --tls-mode letsencrypt --tls-acme-challenge http-01 \
    --tls-domain relay.example.com --tls-email ops@example.com \
    --listen-port 443 --allow 'devbox-1:your-long-registration-secret'
  mcrelay serve --tls-mode letsencrypt --tls-acme-challenge dns-01 \
    --tls-domain relay.example.com --tls-email ops@example.com \
    --tls-route53-zone-id Z0123456789ABCDEFGHIJ \
    --allow 'devbox-1:your-long-registration-secret'
  mcrelay serve --tls-cert /etc/ssl/relay.crt --tls-key /etc/ssl/relay.key \
    --allow 'devbox-1:your-long-registration-secret'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fc, err := Load(LoadOptions{
				ConfigFile: *cfgFile,
				Flags:      cmd.Flags(),
				AllowExtra: allows,
			})
			if err != nil {
				return err
			}
			// CLI overrides that share names with setup-service flags.
			if cmd.Flags().Changed("listen-host") && listenHost != "" {
				fc.Listen.Host = listenHost
			}
			if cmd.Flags().Changed("listen-port") && listenPort > 0 {
				fc.Listen.Port = listenPort
			}
			if cmd.Flags().Changed("data-dir") && dataDir != "" {
				fc.DataDir = dataDir
			}
			if cmd.Flags().Changed("tls-mode") && tlsMode != "" {
				fc.TLS.Mode = tlsMode
			}
			if cmd.Flags().Changed("tls-cert") {
				fc.TLS.CertFile = tlsCert
			}
			if cmd.Flags().Changed("tls-key") {
				fc.TLS.KeyFile = tlsKey
			}
			if cmd.Flags().Changed("tls-domain") && len(tlsDomains) > 0 {
				fc.TLS.LetsEncrypt.Domains = tlsDomains
			}
			if cmd.Flags().Changed("tls-email") {
				fc.TLS.LetsEncrypt.Email = tlsEmail
			}
			if cmd.Flags().Changed("tls-acme-directory") {
				fc.TLS.LetsEncrypt.DirectoryURL = tlsACMEDir
			}
			if cmd.Flags().Changed("tls-acme-staging") {
				fc.TLS.LetsEncrypt.Staging = tlsStaging
			}
			if cmd.Flags().Changed("tls-acme-http-port") {
				fc.TLS.LetsEncrypt.HTTPPort = tlsHTTPPort
			}
			if cmd.Flags().Changed("tls-acme-challenge") && tlsChallenge != "" {
				fc.TLS.LetsEncrypt.Challenge = tlsChallenge
			}
			if cmd.Flags().Changed("tls-route53-zone-id") {
				fc.TLS.LetsEncrypt.Route53.HostedZoneID = tlsR53Zone
			}
			if cmd.Flags().Changed("tls-route53-region") {
				fc.TLS.LetsEncrypt.Route53.Region = tlsR53Region
			}
			if cmd.Flags().Changed("tls-route53-profile") {
				fc.TLS.LetsEncrypt.Route53.Profile = tlsR53Profile
			}
			if cmd.Flags().Changed("allow-legacy-tunnel-secret") {
				fc.AllowLegacyTunnelSecret = allowLegacyTunnelSecret
			}
			if cmd.Flags().Changed("trusted-proxy") && len(trustedProxies) > 0 {
				fc.TrustedProxies = expandStringList(trustedProxies)
			}
			if *logLevel != "" {
				fc.Log.Level = *logLevel
			}
			if *logFormat != "" {
				fc.Log.Format = *logFormat
			}
			// Env MCRELAY_TLS_DOMAINS=a,b may arrive as a single string via AutomaticEnv.
			fc.TLS.LetsEncrypt.Domains = expandDomainList(fc.TLS.LetsEncrypt.Domains)
			fc.TrustedProxies = expandStringList(fc.TrustedProxies)

			if err := fc.Validate(); err != nil {
				return err
			}
			fc.TLS = fc.TLS.Normalized()

			if err := EnsureDataDir(fc.DataDir); err != nil {
				return fmt.Errorf("data_dir: %w", err)
			}

			log := logging.Setup(logging.Options{
				Level:  fc.Log.Level,
				Format: fc.Log.Format,
			})

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srvCfg := fc.ToServerConfig()
			cleanup, err := ApplyTLS(ctx, fc, &srvCfg, log)
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}

			srv := New(srvCfg, log)
			log.Info("mcrelay starting",
				slog.String("version", cliVersion),
				slog.String("listen", srvCfg.ListenAddr),
				slog.String("tls", fc.TLS.Mode),
				slog.Int("hosts_allowed", len(srvCfg.Allow)),
				slog.String("data_dir", fc.DataDir),
			)
			if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
				return err
			}
			return nil
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&listenHost, "listen-host", "", "listen host (default: config / 0.0.0.0)")
	fs.IntVar(&listenPort, "listen-port", 0, "listen port (default: config / 8443; use 443 for public LE)")
	fs.StringVar(&dataDir, "data-dir", "", "data directory (default: XDG mcrelay data home)")
	fs.StringVar(&tlsMode, "tls-mode", "", "tls mode: letsencrypt|files|off (empty = auto)")
	fs.StringVar(&tlsCert, "tls-cert", "", "TLS certificate PEM path (files mode)")
	fs.StringVar(&tlsKey, "tls-key", "", "TLS private key PEM path (files mode)")
	fs.StringSliceVar(&tlsDomains, "tls-domain", nil, "ACME domain (repeatable); env MCRELAY_TLS_DOMAINS")
	fs.StringVar(&tlsEmail, "tls-email", "", "ACME account email; env MCRELAY_TLS_EMAIL")
	fs.StringVar(&tlsACMEDir, "tls-acme-directory", "", "ACME directory URL (empty = production LE)")
	fs.BoolVar(&tlsStaging, "tls-acme-staging", false, "use Let's Encrypt staging CA")
	fs.IntVar(&tlsHTTPPort, "tls-acme-http-port", 0, "HTTP-01 challenge port (0 = 80; ignored for dns-01)")
	fs.StringVar(&tlsChallenge, "tls-acme-challenge", "", "ACME challenge: http-01 (default) or dns-01; env MCRELAY_TLS_ACME_CHALLENGE")
	fs.StringVar(&tlsR53Zone, "tls-route53-zone-id", "", "Route 53 hosted zone id for DNS-01; env MCRELAY_TLS_ROUTE53_HOSTED_ZONE_ID")
	fs.StringVar(&tlsR53Region, "tls-route53-region", "", "AWS region for Route 53 DNS-01; env MCRELAY_TLS_ROUTE53_REGION")
	fs.StringVar(&tlsR53Profile, "tls-route53-profile", "", "AWS profile for Route 53 DNS-01; env MCRELAY_TLS_ROUTE53_PROFILE")
	fs.StringArrayVar(&allows, "allow", nil, "allowed host registration host_id:secret (repeatable; merges with config)")
	fs.BoolVar(&allowLegacyTunnelSecret, "allow-legacy-tunnel-secret", false, "allow registration secret on /v1/tunnel (default false; MCRELAY_ALLOW_LEGACY_TUNNEL_SECRET)")
	fs.StringArrayVar(&trustedProxies, "trusted-proxy", nil, "trusted reverse-proxy CIDR or IP for XFF (repeatable; MCRELAY_TRUSTED_PROXIES)")
	_ = cfgFile
	return cmd
}

// expandDomainList splits comma-separated domain entries (env / single flag).
func expandDomainList(in []string) []string {
	var out []string
	for _, d := range in {
		for _, p := range strings.Split(d, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// setupServiceFlags mirrors mcremote setup-service.
type setupServiceFlags struct {
	unitName   string
	binary     string
	configPath string
	dataDir    string
	listenHost string
	listenPort int
	workingDir string
	printOnly  bool
	force      bool
	noEnable   bool
	noStart    bool
	noLinger   bool
	remove     bool
	envPairs   []string
}

func bindSetupServiceFlags(cmd *cobra.Command, f *setupServiceFlags) {
	fs := cmd.Flags()
	if fs.Lookup("unit-name") != nil {
		return
	}
	fs.StringVar(&f.unitName, "unit-name", "mcrelay", "service name (Linux unit without .service; macOS maps to a launchd Label)")
	fs.StringVar(&f.binary, "binary", "", "serve binary path (default: ~/.local/bin/mcrelay if present, else this executable)")
	fs.StringVar(&f.configPath, "service-config", "", "config file path written into the service (falls back to --config)")
	if fs.Lookup("data-dir") == nil {
		fs.StringVar(&f.dataDir, "data-dir", "", "data directory passed to serve")
	}
	if fs.Lookup("listen-host") == nil {
		fs.StringVar(&f.listenHost, "listen-host", "", "listen host baked into the service (default: follow config)")
	}
	if fs.Lookup("listen-port") == nil {
		fs.IntVar(&f.listenPort, "listen-port", 0, "listen port baked into the service (default: follow config)")
	}
	fs.StringVar(&f.workingDir, "working-directory", "", "working directory for the service (default: $HOME)")
	fs.BoolVar(&f.printOnly, "print-only", false, "print the unit/plist to stdout; do not install")
	fs.BoolVar(&f.force, "force", false, "overwrite an existing unit/plist file")
	fs.BoolVar(&f.noEnable, "no-enable", false, "do not enable the service (systemctl --user enable / launchctl enable)")
	fs.BoolVar(&f.noStart, "no-start", false, "do not start/restart the service")
	fs.BoolVar(&f.noLinger, "no-linger", false, "Linux: skip loginctl enable-linger. macOS: no effect (LaunchAgents are session-bound)")
	fs.BoolVar(&f.remove, "remove", false, "stop, disable, and delete the service definition (inverse of setup)")
	fs.StringArrayVar(&f.envPairs, "env", nil, "extra environment entries (KEY=VALUE); repeatable")
}

func newSetupServiceCmd(cfgFile, logLevel, logFormat *string) *cobra.Command {
	var f setupServiceFlags
	cmd := &cobra.Command{
		Use:   "setup-service",
		Short: "Install mcrelay as a background service and start it",
		Long: `Install mcrelay as a managed background service (definition only — no binary copy).

Linux: write ~/.config/systemd/user/mcrelay.service, enable/start, optional linger.
macOS: write ~/Library/LaunchAgents/com.magiccliremote.mcrelay.plist (LaunchAgent;
session-bound, no sudo).

Install the binary first (make install / make install-relay). Secrets should
live in the config file (0600) or --env (service file becomes 0600).`,
		Example: `  mcrelay setup-service --force
  mcrelay setup-service --service-config ~/.config/mcrelay/config.yaml --force
  mcrelay setup-service --print-only
  mcrelay setup-service --remove`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupService(cmd, f, *cfgFile, *logLevel, *logFormat)
		},
	}
	bindSetupServiceFlags(cmd, &f)
	return cmd
}

func runSetupService(cmd *cobra.Command, f setupServiceFlags, cfgFile, logLevel, logFormat string) error {
	out := cmd.OutOrStdout()
	cfg := f.configPath
	if cfg == "" {
		cfg = cfgFile
	}
	opts := service.Options{
		Product:          "mcrelay",
		Description:      "mcrelay outbound join-plane relay for mcremote",
		UnitName:         f.unitName,
		Binary:           f.binary,
		ConfigPath:       cfg,
		DataDir:          f.dataDir,
		ListenHost:       f.listenHost,
		ListenPort:       f.listenPort,
		LogLevel:         logLevel,
		LogFormat:        logFormat,
		WorkingDirectory: f.workingDir,
		ExtraEnviron:     f.envPairs,
		PrintOnly:        f.printOnly,
		Force:            f.force,
		NoEnable:         f.noEnable,
		NoStart:          f.noStart,
		NoLinger:         f.noLinger,
	}

	if f.remove {
		res, err := service.Remove(opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Removed:           %s\n", res.UnitPath)
		fmt.Fprintln(out, "The relay was stopped and the service disabled.")
		return nil
	}

	res, err := service.Setup(opts)
	if err != nil {
		if res.UnitPath != "" && !f.printOnly {
			fmt.Fprintf(out, "Service file:      %s\n", res.UnitPath)
		}
		return err
	}
	if f.printOnly {
		fmt.Fprint(out, res.UnitBody)
		return nil
	}
	if res.Unchanged {
		fmt.Fprintln(out, "Service definition unchanged (already installed).")
	}
	service.PrintSetupResult(out, res, f.noLinger, "mcrelay")
	fmt.Fprintln(out)
	// mcrelay is a public edge: binding 443 (TLS) or 80 (ACME HTTP-01) needs
	// privileges a user service cannot grant.
	fmt.Fprintln(out, "Public ports: binding 443 (TLS) or 80 (ACME HTTP-01) needs elevated")
	fmt.Fprintln(out, "      privileges a user service cannot grant. If you bind a port < 1024:")
	if res.Scope == "launchd-agent" {
		fmt.Fprintln(out, "      use a privileged front proxy/port-forward, or run behind a load balancer.")
		fmt.Fprintln(out, "      (Not needed for the default 8443.)")
	} else {
		fmt.Fprintf(out, "        sudo setcap 'cap_net_bind_service=+ep' %s\n", res.Binary)
		fmt.Fprintln(out, "      (Not needed for the default 8443, or behind a proxy/port-forward.)")
	}
	return nil
}
