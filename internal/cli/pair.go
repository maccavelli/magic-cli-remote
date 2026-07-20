package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/daemon"
	"github.com/maccavelli/magic-cli-remote/internal/pairuri"
	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

func newPairCmd() *cobra.Command {
	var (
		name     string
		showQR   bool
		pairHost string
		ttlStr   string
	)

	createFn := func(cmd *cobra.Command, args []string) error {
		store, cfg, err := openStoreFromFlags(cmd)
		if err != nil {
			return err
		}
		dev, token, err := store.Create(name)
		if err != nil {
			return err
		}

		host := resolvePairHost(pairHost, cfg)
		fp, err := pairFingerprint(cfg)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Device: %s (id=%s)\n", dev.Name, dev.ID)
		fmt.Fprintf(out, "Token:  %s\n", token)
		fmt.Fprintf(out, "Host:   %s\n", host)
		fmt.Fprintf(out, "WS URL: %s://%s/v1/ws\n", cfg.TLS.Scheme(), host)
		printFingerprint(out, "Cert:   ", fp, cfg.TLS.ResolvedMode())

		uri, err := pairuri.Encode(pairuri.Payload{
			Host:        cfg.TLS.Scheme() + "://" + host,
			Token:       token,
			Fingerprint: fp,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Pair:   %s\n", uri)
		fmt.Fprintln(out, "Store this token; it will not be shown again.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Tip: prefer `mcremote pair code` for a 5-minute short code (no long token to copy).")
		fmt.Fprintln(out, "On the phone: Scan QR, or paste Host + Token.")

		printPairQR(cmd, out, showQR, uri)
		return nil
	}

	codeFn := func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigFromFlags(cmd)
		if err != nil {
			return err
		}
		codes, err := auth.OpenPairCodeStore(filepath.Join(cfg.DataDir, "pair_codes.json"))
		if err != nil {
			return err
		}
		ttl := auth.DefaultPairCodeTTL
		if strings.TrimSpace(ttlStr) != "" {
			d, err := time.ParseDuration(ttlStr)
			if err != nil {
				return fmt.Errorf("invalid --ttl: %w", err)
			}
			ttl = d
		}
		info, err := codes.Create(name, ttl)
		if err != nil {
			return err
		}

		host := resolvePairHost(pairHost, cfg)
		fp, err := pairFingerprint(cfg)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		remain := time.Until(info.ExpiresAt).Round(time.Second)
		if remain < 0 {
			remain = 0
		}
		fmt.Fprintf(out, "Device name: %s\n", info.Name)
		fmt.Fprintf(out, "Pair code:   %s\n", info.Display)
		fmt.Fprintf(out, "Expires in:  %s (at %s)\n", remain, info.ExpiresAt.Local().Format(time.Kitchen))
		fmt.Fprintf(out, "Host:        %s\n", host)
		fmt.Fprintf(out, "WS URL:      %s://%s/v1/ws\n", cfg.TLS.Scheme(), host)
		printFingerprint(out, "Cert:        ", fp, cfg.TLS.ResolvedMode())

		uri, err := pairuri.Encode(pairuri.Payload{
			Host:        cfg.TLS.Scheme() + "://" + host,
			Code:        info.Code,
			Fingerprint: fp,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Pair URI:    %s\n", uri)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "On the phone: Magic CLI Remote → Enter code (or Scan QR).")
		fmt.Fprintln(out, "One-time use. Daemon must share this data dir to accept the claim.")

		printPairQR(cmd, out, showQR, uri)
		return nil
	}

	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Manage device pairing (tokens + short codes)",
		Long: `Create short pair codes (recommended), long-lived device tokens, list, or revoke devices.

Preferred onboarding: mcremote pair code --name phone --qr
Then on the phone app: Enter code or Scan QR.

Running "mcremote pair" with no subcommand is the same as "mcremote pair code".`,
		Example: pairExample,
		RunE:    codeFn, // default to short code — safer than minting long tokens
	}
	cmd.Flags().StringVar(&name, "name", "device", "device label")
	cmd.Flags().BoolVar(&showQR, "qr", false, "print ASCII QR; default on when stdout is a TTY")
	cmd.Flags().StringVar(&pairHost, "host", "", "host:port the phone should dial")
	cmd.Flags().StringVar(&ttlStr, "ttl", "5m", "pair code lifetime (e.g. 5m)")
	cmd.Flags().String("data-dir", "", "data directory (overrides config)")

	createCmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a long-lived device token (shown once); prefer `pair code`",
		Long:    "Issue a durable mcr_… device token (shown once). Prefer short codes via `pair code` for phone onboarding.",
		Example: pairCreateExample,
		RunE:    createFn,
	}
	createCmd.Flags().StringVar(&name, "name", "device", "device label")
	createCmd.Flags().BoolVar(&showQR, "qr", false, "print ASCII QR (default on when stdout is a TTY)")
	createCmd.Flags().StringVar(&pairHost, "host", "", "host:port the phone should dial")
	createCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	codeCmd := &cobra.Command{
		Use:     "code",
		Short:   "Create an 8-char pair code (5 min, one-shot) for phone entry",
		Long:    "Create a one-shot 8-character pair code (default TTL 5m). Phone: Enter code or Scan QR. Claims via pair.claim over the WebSocket.",
		Example: pairCodeExample,
		RunE:    codeFn,
	}
	codeCmd.Flags().StringVar(&name, "name", "device", "device label")
	codeCmd.Flags().BoolVar(&showQR, "qr", false, "print ASCII QR (default on when stdout is a TTY)")
	codeCmd.Flags().StringVar(&pairHost, "host", "", "host:port the phone should dial")
	codeCmd.Flags().StringVar(&ttlStr, "ttl", "5m", "pair code lifetime")
	codeCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "List paired devices (metadata only)",
		Example: pairListExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openStoreFromFlags(cmd)
			if err != nil {
				return err
			}
			devices, err := store.List()
			if err != nil {
				return err
			}
			if len(devices) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No devices paired.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tCREATED\tLAST_USED")
			for _, d := range devices {
				last := "-"
				if d.LastUsedAt != nil {
					last = d.LastUsedAt.Format("2006-01-02T15:04:05Z")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					d.ID, d.Name, d.CreatedAt.Format("2006-01-02T15:04:05Z"), last)
			}
			return w.Flush()
		},
	}
	listCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	revokeCmd := &cobra.Command{
		Use:     "revoke <device-id-or-name>",
		Short:   "Revoke a paired device",
		Example: pairRevokeExample,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openStoreFromFlags(cmd)
			if err != nil {
				return err
			}
			dev, err := store.Revoke(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked device %s (%s)\n", dev.Name, dev.ID)
			return nil
		},
	}
	revokeCmd.Flags().String("data-dir", "", "data directory (overrides config)")

	cmd.AddCommand(codeCmd, createCmd, listCmd, revokeCmd)
	return cmd
}

func printPairQR(cmd *cobra.Command, out interface {
	Write([]byte) (int, error)
}, showQR bool, uri string) {
	printQR := showQR
	if !cmd.Flags().Changed("qr") {
		if f, ok := out.(*os.File); ok {
			if st, err := f.Stat(); err == nil {
				printQR = (st.Mode() & os.ModeCharDevice) != 0
			}
		}
	}
	if printQR {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Scan with the Magic CLI Remote app:")
		qrterminal.GenerateWithConfig(uri, qrterminal.Config{
			Level:      qrterminal.M,
			Writer:     out,
			HalfBlocks: true,
			QuietZone:  2,
		})
	} else {
		fmt.Fprintln(out, "(re-run with --qr to print a terminal QR code)")
	}
}

// pairFingerprint returns the base64url SHA-256 of the certificate the daemon
// will serve — but only in self-signed mode, where the phone has no other way
// to establish trust. A Let's Encrypt certificate chains to a public root and
// is renewed every ~60 days, so pinning it would be both unnecessary and
// actively harmful: the pin would break at the first renewal. Empty string
// means "do not emit fp= in the QR".
//
// Doing this here (rather than only in the daemon) means `mcremote pair` run
// before the first `serve` still advertises the right fingerprint: both paths
// converge on the same persisted files under the data dir.
func pairFingerprint(cfg config.Config) (string, error) {
	if !cfg.TLS.Pinned() {
		return "", nil
	}
	b, err := daemon.EnsureCerts(cfg)
	if err != nil {
		return "", err
	}
	return b.FingerprintBase64(), nil
}

// printFingerprint writes the trust line for the resolved TLS mode. label
// carries its own padding so it lines up with the surrounding block, which
// differs between subcommands.
func printFingerprint(out interface{ Write([]byte) (int, error) }, label, fp string, mode string) {
	switch {
	case fp != "":
		fmt.Fprintf(out, "%ssha256:%s (pinned by the app)\n", label, fp)
	case mode == config.TLSModeLetsEncrypt:
		fmt.Fprintf(out, "%spublicly trusted (Let's Encrypt) — no pin needed\n", label)
	default:
		fmt.Fprintf(out, "%sdisabled — the device token will travel in cleartext.\n",
			strings.Replace(label, "Cert:", "TLS: ", 1))
	}
}

// resolvePairHost picks the host:port printed into the pair QR.
//
// In letsencrypt mode this MUST be the certificate's DNS name: the CA will
// never issue for an IP address, and the mesh addresses this daemon usually
// binds (100.64.0.0/10 CGNAT) are not names at all — advertising one would
// guarantee a hostname-verification failure on the phone. In self-signed mode
// the fingerprint is pinned, the name is irrelevant, and the mesh IP is the
// most useful thing to dial.
func resolvePairHost(pairHost string, cfg config.Config) string {
	port := cfg.Listen.Port
	if host := strings.TrimSpace(pairHost); host != "" {
		return withPort(host, port)
	}
	if cfg.TLS.ResolvedMode() == config.TLSModeLetsEncrypt {
		if d := cfg.TLS.LetsEncrypt.PrimaryDomain(); d != "" {
			return withPort(d, port)
		}
	}
	return detectAdvertiseHost(port)
}

func withPort(host string, port int) string {
	if !strings.Contains(host, ":") {
		return fmt.Sprintf("%s:%d", host, port)
	}
	return host
}

func loadConfigFromFlags(cmd *cobra.Command) (config.Config, error) {
	cfg, err := config.Load(config.LoadOptions{
		ConfigFile: cfgFile,
		Flags:      cmd.Flags(),
	})
	if err != nil {
		return config.Config{}, err
	}
	if f := cmd.Flags().Lookup("data-dir"); f != nil && cmd.Flags().Changed("data-dir") {
		cfg.DataDir = f.Value.String()
	}
	if cmd.Parent() != nil {
		if f := cmd.Parent().Flags().Lookup("data-dir"); f != nil && cmd.Parent().Flags().Changed("data-dir") {
			cfg.DataDir = f.Value.String()
		}
	}
	return cfg, nil
}

func openStoreFromFlags(cmd *cobra.Command) (*auth.Store, config.Config, error) {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return nil, config.Config{}, err
	}
	store, err := auth.OpenStore(filepath.Join(cfg.DataDir, "devices.json"))
	if err != nil {
		return nil, config.Config{}, err
	}
	return store, cfg, nil
}

// detectAdvertiseHost picks the address printed into the pair QR.
// Preference: Tailscale IPv4 → MCREMOTE_PAIR_HOST → localhost.
func detectAdvertiseHost(port int) string {
	if v := strings.TrimSpace(os.Getenv("MCREMOTE_PAIR_HOST")); v != "" {
		if !strings.Contains(v, ":") {
			return fmt.Sprintf("%s:%d", v, port)
		}
		return v
	}
	if ip := tailscaleIPv4(); ip != "" {
		return fmt.Sprintf("%s:%d", ip, port)
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func tailscaleIPv4() string {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return ""
	}
	if i := strings.IndexByte(ip, '\n'); i >= 0 {
		ip = strings.TrimSpace(ip[:i])
	}
	return ip
}
