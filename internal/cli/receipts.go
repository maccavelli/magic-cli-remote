package cli

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/daemon"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
	"github.com/spf13/cobra"
)

const receiptsExample = `  # List every device with a receipts chain
  mcremote receipts list

  # Verify one device's chain end to end
  mcremote receipts verify --device dev_abc123

  # Show what one specific decision actually attested to
  mcremote receipts show --device dev_abc123 --permission per_a1b2`

func newReceiptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "receipts",
		Short: "Inspect and verify signed permission-decision receipts",
		Long: "Signed receipts (MADR 0077) are opt-in, device-signed, hash-chained\n" +
			"records of permission decisions matching receipts.allow_patterns. Each\n" +
			"line in <data_dir>/receipts/<device_id>.jsonl is a JWS compact string\n" +
			"wrapping an in-toto-style Statement; see docs/receipts.md for the wire\n" +
			"shape and the predicateType registry.",
		Example: receiptsExample,
	}
	cmd.AddCommand(newReceiptsListCmd())
	cmd.AddCommand(newReceiptsVerifyCmd())
	cmd.AddCommand(newReceiptsShowCmd())
	return cmd
}

// openReceiptStoresFromFlags resolves --data-dir (falling back to config)
// into the receipt store and the auth store PublicKeyFor reads from —
// mirrors openStoreFromFlags' role for `pair`.
func openReceiptStoresFromFlags(cmd *cobra.Command) (*receipt.Store, *auth.Store, config.Config, error) {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return nil, nil, config.Config{}, err
	}
	rs, err := receipt.NewStore(cfg.DataDir)
	if err != nil {
		return nil, nil, config.Config{}, err
	}
	as, err := auth.OpenStore(filepath.Join(cfg.DataDir, "devices.json"))
	if err != nil {
		return nil, nil, config.Config{}, err
	}
	return rs, as, cfg, nil
}

// devicePublicKey resolves the verification key for deviceID: the auth
// store's live record first (authoritative while the device is enrolled),
// else the key archived beside the chain at receipt time — which is what
// keeps a revoked/pruned device's history auditable after its Device record
// (the only other holder of the key) is deleted.
func devicePublicKey(rs *receipt.Store, as *auth.Store, deviceID string) (*ecdsa.PublicKey, error) {
	pub, authErr := as.PublicKeyFor(deviceID)
	if authErr == nil {
		return pub, nil
	}
	archived, archErr := rs.ArchivedKey(deviceID)
	if archErr == nil {
		return archived, nil
	}
	// Report the auth-store error — it names the actionable condition
	// ("device not found", "no persisted key yet") — with the archive miss
	// as context.
	return nil, fmt.Errorf("%w (and no archived key beside the chain: %v)", authErr, archErr)
}

// daemonPublicKeyFromFlags resolves the same ECDSA key P7 signs
// receipt-unavailable markers with (internal/daemon.EnsureCerts(cfg),
// mirroring how `mcremote pair`'s pairFingerprint already reuses it), so
// `verify`/`show` can check a marker entry against the real key that
// produced it — not a guess.
func daemonPublicKeyFromFlags(cfg config.Config) (*ecdsa.PublicKey, error) {
	b, err := daemon.EnsureCerts(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve daemon signing key: %w", err)
	}
	daemonKey, ok := b.Certificate.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("daemon TLS key is %T, not ECDSA", b.Certificate.PrivateKey)
	}
	return &daemonKey.PublicKey, nil
}

func newReceiptsListCmd() *cobra.Command {
	var device string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List devices with a receipts chain",
		RunE: func(cmd *cobra.Command, args []string) error {
			rs, as, cfg, err := openReceiptStoresFromFlags(cmd)
			if err != nil {
				return err
			}
			daemonPub, daemonErr := daemonPublicKeyFromFlags(cfg)

			ids, err := rs.DeviceIDs()
			if err != nil {
				return err
			}
			if device != "" {
				found := false
				for _, id := range ids {
					if id == device {
						found = true
						break
					}
				}
				if !found {
					fmt.Fprintf(cmd.OutOrStdout(), "No receipts for device %s.\n", device)
					return nil
				}
				ids = []string{device}
			}
			if len(ids) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No devices have a receipts chain.")
				return nil
			}

			out := cmd.OutOrStdout()
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "DEVICE\tENTRIES\tFIRST\tLAST\tCHAIN")
			for _, id := range ids {
				lines, err := rs.Lines(id)
				if err != nil {
					return err
				}
				first, last := summarizeDecidedAt(lines)
				chain := "-"
				if devicePub, perr := devicePublicKey(rs, as, id); perr == nil && daemonErr == nil {
					broken, verr := rs.Verify(id, devicePub, daemonPub)
					switch {
					case verr != nil:
						chain = "error"
					case broken == -1:
						chain = "intact"
					default:
						chain = fmt.Sprintf("BROKEN@%d", broken)
					}
				} else {
					chain = "unavailable"
				}
				fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", id, len(lines), first, last, chain)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&device, "device", "", "only list this device")
	cmd.Flags().String("data-dir", "", "data directory (overrides config)")
	return cmd
}

func newReceiptsVerifyCmd() *cobra.Command {
	var device string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify one device's receipt chain end to end",
		Long: "Walks the device's chain from the first entry, confirming each line's\n" +
			"JWS signature (against the device's enrolled key for permission-decision\n" +
			"entries, the daemon's own key for receipt-unavailable markers) and that\n" +
			"its chain link matches the SHA-256 of the line above it. Exits non-zero\n" +
			"on any break, so this is safe to use in an audit script.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if device == "" {
				return fmt.Errorf("--device is required")
			}
			rs, as, cfg, err := openReceiptStoresFromFlags(cmd)
			if err != nil {
				return err
			}
			devicePub, err := devicePublicKey(rs, as, device)
			if err != nil {
				return fmt.Errorf("device %s: %w", device, err)
			}
			daemonPub, err := daemonPublicKeyFromFlags(cfg)
			if err != nil {
				return err
			}
			broken, err := rs.Verify(device, devicePub, daemonPub)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if broken == -1 {
				fmt.Fprintf(out, "OK: device %s chain intact.\n", device)
				return nil
			}
			fmt.Fprintf(out, "BROKEN: device %s chain broken at line %d.\n", device, broken)
			return fmt.Errorf("chain broken at line %d", broken)
		},
	}
	cmd.Flags().StringVar(&device, "device", "", "device id to verify (required)")
	cmd.Flags().String("data-dir", "", "data directory (overrides config)")
	return cmd
}

func newReceiptsShowCmd() *cobra.Command {
	var device, permissionID string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Pretty-print what one receipt actually attests to",
		Long: "Decodes one Statement (human-readable, not raw JWS) for a specific\n" +
			"permission decision — \"what exactly did this receipt attest to.\"\n" +
			"Verifies the signature when the signing key is available; falls back to\n" +
			"an unverified decode (clearly labeled) otherwise, rather than refusing\n" +
			"to show anything at all.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if device == "" || permissionID == "" {
				return fmt.Errorf("--device and --permission are required")
			}
			rs, as, cfg, err := openReceiptStoresFromFlags(cmd)
			if err != nil {
				return err
			}
			lines, err := rs.Lines(device)
			if err != nil {
				return err
			}
			var match string
			for _, line := range lines {
				payload, perr := receipt.DecodePayloadUnverified(line)
				if perr != nil {
					continue
				}
				var probe struct {
					Subject []struct {
						Name string `json:"name"`
					} `json:"subject"`
				}
				if json.Unmarshal(payload, &probe) != nil {
					continue
				}
				for _, subj := range probe.Subject {
					if subj.Name != "" && subjectNamesPermission(subj.Name, permissionID) {
						match = line
					}
				}
			}
			if match == "" {
				return fmt.Errorf("no receipt found for device %s permission %s", device, permissionID)
			}

			out := cmd.OutOrStdout()
			verified, payload := showVerify(match, device, rs, as, cfg)
			var stmt receipt.Statement
			if err := json.Unmarshal(payload, &stmt); err != nil {
				return fmt.Errorf("decode statement: %w", err)
			}
			pretty, err := json.MarshalIndent(&stmt, "", "  ")
			if err != nil {
				return err
			}
			if verified {
				fmt.Fprintln(out, "Signature: VERIFIED")
			} else {
				fmt.Fprintln(out, "Signature: NOT VERIFIED (signing key unavailable — content shown as-is, do not treat as authoritative)")
			}
			fmt.Fprintln(out, string(pretty))
			return nil
		},
	}
	cmd.Flags().StringVar(&device, "device", "", "device id (required)")
	cmd.Flags().StringVar(&permissionID, "permission", "", "permission id (required)")
	cmd.Flags().String("data-dir", "", "data directory (overrides config)")
	return cmd
}

// subjectNamesPermission reports whether a Statement's subject name (e.g.
// "session:<id>/permission:<id>" for a decision, "permission:<id>" for an
// unavailable marker) names permissionID — both shapes end the same way.
func subjectNamesPermission(subjectName, permissionID string) bool {
	suffix := "permission:" + permissionID
	if len(subjectName) < len(suffix) {
		return false
	}
	return subjectName[len(subjectName)-len(suffix):] == suffix
}

// showVerify attempts a real signature check (predicateType selects device
// vs. daemon key, mirroring Store.Verify's own selection), returning
// whether it succeeded and the payload either way — DecodePayloadUnverified
// as the fallback so `show` degrades gracefully instead of refusing to
// display anything.
func showVerify(compact, device string, rs *receipt.Store, as *auth.Store, cfg config.Config) (bool, []byte) {
	payload, err := receipt.DecodePayloadUnverified(compact)
	if err != nil {
		return false, []byte("{}")
	}
	var probe struct {
		PredicateType string `json:"predicateType"`
	}
	if json.Unmarshal(payload, &probe) != nil {
		return false, payload
	}
	var pub *ecdsa.PublicKey
	switch probe.PredicateType {
	case receipt.PredicateTypePermissionDecision:
		pub, err = devicePublicKey(rs, as, device)
	case receipt.PredicateTypeReceiptUnavailable:
		pub, err = daemonPublicKeyFromFlags(cfg)
	default:
		return false, payload
	}
	if err != nil || pub == nil {
		return false, payload
	}
	verified, verr := receipt.VerifyES256Compact(pub, compact)
	if verr != nil {
		return false, payload
	}
	return true, verified
}

// summarizeDecidedAt returns the earliest/latest decided_at among lines that
// carry one (permission-decision entries only — a receipt-unavailable
// marker has no decided_at) — "-" when none do.
func summarizeDecidedAt(lines []string) (first, last string) {
	first, last = "-", "-"
	for _, line := range lines {
		payload, err := receipt.DecodePayloadUnverified(line)
		if err != nil {
			continue
		}
		var probe struct {
			PredicateType string `json:"predicateType"`
			Predicate     struct {
				DecidedAt string `json:"decided_at"`
			} `json:"predicate"`
		}
		if json.Unmarshal(payload, &probe) != nil {
			continue
		}
		if probe.PredicateType != receipt.PredicateTypePermissionDecision || probe.Predicate.DecidedAt == "" {
			continue
		}
		if first == "-" {
			first = probe.Predicate.DecidedAt
		}
		last = probe.Predicate.DecidedAt
	}
	return first, last
}
