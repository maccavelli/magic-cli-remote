package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Exit codes for auth-recovery (MADR 0074 P19 step 7). They are distinct so an
// operator script can tell "you asked for something impossible" from "the
// machine could not do it".
const (
	authRecoveryExitUsage  = 2
	authRecoveryExitFailed = 3
)

// exitCodeError carries a specific process exit code to the root command.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

// ExitCode reports the process exit code an error requests, or 1.
func ExitCode(err error) int {
	var ec *exitCodeError
	if errors.As(err, &ec) {
		return ec.code
	}
	if err != nil {
		return 1
	}
	return 0
}

const authRecoveryExample = `  # Show credential backup state for every provider
  mcremote auth-recovery status

  # Show one provider
  mcremote auth-recovery status codex

  # Resolve a preserved ambiguous state
  mcremote auth-recovery choose codex previous`

func newAuthRecoveryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth-recovery",
		Short: "Inspect and resolve provider credential backup state",
		Long: "Credential transactions (MADR 0074 D21-D26) keep a bounded CURRENT and\n" +
			"PREVIOUS generation per provider so an interrupted login or an external\n" +
			"writer cannot leave the host signed out with no way back.\n\n" +
			"When durable evidence is ambiguous the coordinator preserves every file\n" +
			"and waits for a decision rather than guessing. These commands show that\n" +
			"state and apply the decision. They print provider, public state, and\n" +
			"timestamps only — never a path, fingerprint, or credential.",
		Example: authRecoveryExample,
	}
	cmd.AddCommand(newAuthRecoveryStatusCmd())
	cmd.AddCommand(newAuthRecoveryChooseCmd())
	return cmd
}

// coordinatorsFromFlags builds the same adapters and data directory `serve`
// would, so a locally run command serializes with a running daemon through the
// same coordinator and native locks rather than working on a different view.
func coordinatorsFromFlags(cmd *cobra.Command) (map[string]*providerauth.Coordinator, error) {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return nil, err
	}
	out := map[string]*providerauth.Coordinator{}
	adapters := map[string]providerauth.Adapter{
		"codex": codex.NewCredentialAdapter("codex"),
		"grok":  grok.NewCredentialAdapter("grok"),
	}
	for id, ad := range adapters {
		c, err := providerauth.NewCoordinator(cfg.DataDir, ad, providerauth.CoordinatorOptions{})
		if err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, nil
}

func resolveOne(cmd *cobra.Command, provider string) (*providerauth.Coordinator, error) {
	coords, err := coordinatorsFromFlags(cmd)
	if err != nil {
		return nil, err
	}
	c, ok := coords[provider]
	if !ok {
		return nil, &exitCodeError{
			code: authRecoveryExitUsage,
			err:  fmt.Errorf("unknown provider %q (known: codex, grok)", provider),
		}
	}
	return c, nil
}

func newAuthRecoveryStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [provider]",
		Short: "Show credential backup state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			coords, err := coordinatorsFromFlags(cmd)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(coords))
			for id := range coords {
				ids = append(ids, id)
			}
			sort.Strings(ids)

			if len(args) == 1 {
				if _, ok := coords[args[0]]; !ok {
					return &exitCodeError{
						code: authRecoveryExitUsage,
						err:  fmt.Errorf("unknown provider %q (known: %v)", args[0], ids),
					}
				}
				ids = []string{args[0]}
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			for _, id := range ids {
				st, err := coords[id].Status(ctx)
				if err != nil {
					return &exitCodeError{code: authRecoveryExitFailed, err: err}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-6s  %-18s  recovery_available=%v\n",
					id, st.BackupState, st.RecoveryAvailable)
			}
			return nil
		},
	}
}

func newAuthRecoveryChooseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "choose <provider> <live|current|previous|logged-out>",
		Short: "Resolve a preserved ambiguous credential state",
		Long: "live       validate and adopt the credential currently on disk\n" +
			"current    republish the retained CURRENT generation\n" +
			"previous   promote PREVIOUS, retaining the displaced CURRENT\n" +
			"logged-out record the tombstone, then remove the credential and all\n" +
			"           retained generations",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			choice := providerauth.RecoveryChoice(args[1])
			switch choice {
			case providerauth.ChooseLive, providerauth.ChooseCurrent,
				providerauth.ChoosePrevious, providerauth.ChooseLoggedOut:
			default:
				return &exitCodeError{
					code: authRecoveryExitUsage,
					err:  fmt.Errorf("unknown choice %q (want live, current, previous, or logged-out)", args[1]),
				}
			}
			c, err := resolveOne(cmd, args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if err := c.ResolveRecovery(ctx, choice); err != nil {
				return &exitCodeError{code: authRecoveryExitFailed, err: err}
			}
			st, err := c.Status(ctx)
			if err != nil {
				return &exitCodeError{code: authRecoveryExitFailed, err: err}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s resolved: %s (recovery_available=%v)\n",
				args[0], st.BackupState, st.RecoveryAvailable)
			return nil
		},
	}
}
