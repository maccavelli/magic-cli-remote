package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/signal"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/debugserve"
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
	root.AddCommand(newPathsCmd(&cfgFile))
	root.AddCommand(newUpdateCmd())
	return root
}

func newPathsCmd(cfgFile *string) *cobra.Command {
	var asJSON bool
	var dataDir string
	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Print resolved XDG path layout (no mutation)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := Load(LoadOptions{ConfigFile: *cfgFile, Flags: cmd.Flags()})
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("data-dir") {
				cfg.DataDir = dataDir
				if err := cfg.RecomputePaths(); err != nil {
					return err
				}
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"product":             "mcrelay",
					"config_file":         cfg.ConfigFile,
					"config_dir":          cfg.Paths.ConfigDir,
					"data_dir":            cfg.DataDir,
					"state_dir":           cfg.Paths.StateDir,
					"cache_dir":           cfg.Paths.CacheDir,
					"runtime_dir":         cfg.Paths.RuntimeDir,
					"admin_socket":        cfg.Paths.AdminSocket,
					"engine_registry_dir": cfg.Paths.EngineRegistryDir,
					"instance_key":        cfg.Paths.InstanceKey,
					"diagnostics":         cfg.Diagnostics,
				})
			}
			_, _ = fmt.Fprintf(out, "product:            mcrelay\n")
			if cfg.ConfigFile != "" {
				_, _ = fmt.Fprintf(out, "config_file:        %s\n", cfg.ConfigFile)
			}
			_, _ = fmt.Fprintf(out, "config_dir:         %s\n", cfg.Paths.ConfigDir)
			_, _ = fmt.Fprintf(out, "data_dir:           %s\n", cfg.DataDir)
			_, _ = fmt.Fprintf(out, "state_dir:          %s\n", cfg.Paths.StateDir)
			_, _ = fmt.Fprintf(out, "cache_dir:          %s\n", cfg.Paths.CacheDir)
			_, _ = fmt.Fprintf(out, "runtime_dir:        %s\n", cfg.Paths.RuntimeDir)
			_, _ = fmt.Fprintf(out, "instance_key:       %s\n", cfg.Paths.InstanceKey)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "data directory (overrides config)")
	return cmd
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
	// Flags bound through bindRelayFlags land in FileConfig via Load; their
	// local variables exist only to register the flag (0115 F15).
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
		allowPlaintext          bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the mcrelay public edge",
		Long: `Start the join-plane listener (register / join / opaque splice).

Host allowlist is required: configure hosts in YAML, MCRELAY_HOSTS, and/or --allow.

TLS modes:
  letsencrypt — ACME via --tls-acme-challenge http-01 (default; port 80) or dns-01 (Route 53)
  files       — operator PEMs (--tls-cert / --tls-key)
  off         — plaintext (loopback only unless --allow-plaintext)

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
			// One flag mechanism (0115 F15): every flag that bindRelayFlags
			// binds reaches FileConfig through Load. Only the runtime-scoped
			// flags below are applied by hand: trusted-proxy is a StringArray
			// (deliberately post-Load), and log level/format come from the
			// persistent flag set.
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
			fc.TLS.LetsEncrypt.Domains = expandStringList(fc.TLS.LetsEncrypt.Domains)
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

			ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
			defer stop()

			srvCfg := fc.ToServerConfig()
			if allowPlaintext {
				srvCfg.AllowPlaintext = true
			}
			cleanup, err := ApplyTLS(ctx, fc, &srvCfg, log)
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}

			srv := New(srvCfg, log)
			// No-op in release builds; `make debug` + MC_DEBUG_ADDR only
			// (0068 P6, goroutine-leak triage — docs/ops-mcrelay.md).
			debugserve.Start(ctx, log)
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
	fs.BoolVar(&allowPlaintext, "allow-plaintext", false, "permit a non-loopback listen with tls.mode=off (0091 D5; tests/lab only)")
	_ = cfgFile
	return cmd
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
	refresh    bool
	jsonOut    bool
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
	fs.BoolVar(&f.refresh, "refresh", false,
		"re-render the installed unit/plist from this binary's template, preserving the options baked into it; rewrites only what this binary wrote, and reloads the systemd user manager")
	fs.BoolVar(&f.jsonOut, "json", false, "with --refresh, print the result as one JSON object")
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
live in the config file (0600) or --env (service file becomes 0600).

--refresh re-renders an already-installed definition from this binary's
template, keeping the options baked into it, backs the old one up as
<unit>.prev, and reloads the systemd user manager. It rewrites only a
definition this binary wrote and can reproduce.`,
		Example: `  mcrelay setup-service --force
  mcrelay setup-service --service-config ~/.config/mcrelay/config.yaml --force
  mcrelay setup-service --refresh
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

	if f.refresh {
		if f.remove {
			return fmt.Errorf("--refresh and --remove are mutually exclusive")
		}
		res, err := service.RefreshUnit(
			service.Options{Product: "mcrelay", UnitName: f.unitName},
			service.RefreshOptions{PrintOnly: f.printOnly},
		)
		if err != nil {
			return err
		}
		if f.printOnly && res.Body != "" {
			fmt.Fprint(out, res.Body)
			return nil
		}
		return service.PrintRefreshResult(out, res, "mcrelay", f.jsonOut)
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
	// 0099 F4b: the written config binds loopback, because a public bind with
	// no TLS is refused at startup and used to yield a crash-looping unit.
	fmt.Fprintln(out, "Listen:       127.0.0.1:8443 (loopback) — the shipped default, so the")
	fmt.Fprintln(out, "      service starts as provisioned. To expose it publicly set")
	fmt.Fprintln(out, "      listen.host and tls.mode=letsencrypt|files together; a public")
	fmt.Fprintln(out, "      bind without TLS is refused at startup.")
	fmt.Fprintln(out)
	// mcrelay is a public edge. What binding 443 (TLS) or 80 (ACME HTTP-01)
	// costs differs by platform: a privilege on Unix, a reservation collision
	// on Windows (MADR 0116 F15).
	switch res.Scope {
	case "windows-task":
		fmt.Fprintln(out, "Public ports: Windows has no privileged-port restriction, so binding 443")
		fmt.Fprintln(out, "      or 80 needs no elevation. Two things do bite:")
		fmt.Fprintln(out, "        * excluded ranges — check with:")
		fmt.Fprintln(out, "            netsh int ipv4 show excludedportrange protocol=tcp")
		fmt.Fprintln(out, "          Hyper-V, WinNAT and Docker Desktop claim wide dynamic ranges.")
		fmt.Fprintln(out, "        * Windows Defender Firewall prompts on the first inbound bind;")
		fmt.Fprintln(out, "          allow it for the private and/or public profile as appropriate.")
		fmt.Fprintln(out, "      Note the task starts at logon, not at boot (MADR 0116 D12).")
	case "launchd-agent":
		fmt.Fprintln(out, "Public ports: binding 443 (TLS) or 80 (ACME HTTP-01) needs elevated")
		fmt.Fprintln(out, "      privileges a user service cannot grant. If you bind a port < 1024:")
		fmt.Fprintln(out, "      use a privileged front proxy/port-forward, or run behind a load balancer.")
		fmt.Fprintln(out, "      (Not needed for the default 8443.)")
	default:
		fmt.Fprintln(out, "Public ports: binding 443 (TLS) or 80 (ACME HTTP-01) needs elevated")
		fmt.Fprintln(out, "      privileges a user service cannot grant. If you bind a port < 1024:")
		fmt.Fprintf(out, "        sudo setcap 'cap_net_bind_service=+ep' %s\n", res.Binary)
		fmt.Fprintln(out, "      (Not needed for the default 8443, or behind a proxy/port-forward.)")
	}
	return nil
}
