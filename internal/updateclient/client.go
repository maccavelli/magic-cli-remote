// Package updateclient adapts go-selfupdate-lib's selfupdate package to
// mcremote and mcrelay.
//
// It is the only place this repository knows about GitHub releases, version
// comparison, download, verification, or executable replacement: that logic
// lives once in github.com/maccavelli/go-selfupdate-lib/selfupdate (MADR 0005,
// 0054). What stays here is product-specific: the embedded release spec, the
// composition of the library's service backend, reconciler and codesign
// signer, and the frozen v0.15.3 selector fixture that proves the published
// v0.16.0 bridge.
package updateclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/cli"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// Repository is the single GitHub repository both products are published from.
var Repository = selfupdate.Repository{Owner: "maccavelli", Name: "magic-cli-remote"}

// OperationTimeout bounds one whole update: discovery, download, install and
// any managed recovery.
const OperationTimeout = 15 * time.Minute

// Options describe one product's updater.
type Options struct {
	// Product is the executable basename, "mcremote" or "mcrelay".
	Product string
	// Identity is the running binary's build identity, for the User-Agent.
	Identity buildinfo.Info
	// Managed enables service lifecycle handling. mcremote and mcrelay both
	// set it; a product with no service definition leaves it false and can
	// never accidentally start a daemon.
	Managed bool
	// CodesignIdentity optionally re-signs the staged binary on macOS.
	CodesignIdentity string
	// HTTPClient overrides the default client in tests.
	HTTPClient *http.Client
	// APIBaseURL overrides the GitHub API origin in tests.
	APIBaseURL string
	// TargetPath overrides the executable the update replaces, in tests. Its
	// directory becomes an allowed root.
	TargetPath string
	// Health is the product's health wiring for its service backend: the
	// probe, and on Windows the shutdown request (MADR 0054 D5).
	Health service.HealthOptions
}

// New builds the updater for one product. Every security-relevant component is
// composed explicitly here rather than hidden behind a convenience
// constructor, so this file is the one place to audit what the update trusts.
// The reporter and the confirmer are the command's: cli.Run supplies them on
// the canonical streams.
func New(opts Options) (*selfupdate.Updater, error) {
	if opts.Product == "" {
		return nil, fmt.Errorf("updateclient: product is required")
	}
	spec, err := releaseSpec()
	if err != nil {
		return nil, err
	}
	if _, err := spec.Product(opts.Product); err != nil {
		return nil, fmt.Errorf("updateclient: %w", err)
	}
	selector, err := spec.AssetSelector()
	if err != nil {
		return nil, fmt.Errorf("updateclient: asset selector: %w", err)
	}
	unpacker, err := spec.Unpacker()
	if err != nil {
		return nil, fmt.Errorf("updateclient: unpacker: %w", err)
	}
	limits := selfupdate.DefaultLimits()

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: OperationTimeout}
	}
	source, err := newSource(opts, client, limits)
	if err != nil {
		return nil, err
	}

	var install selfupdate.InstallOptions
	if opts.TargetPath != "" {
		install.TargetPolicy = selfupdate.TargetPolicy{
			ExecutablePath: opts.TargetPath,
			AllowedRoots:   []string{filepath.Dir(opts.TargetPath)},
		}
	}
	standalone, err := selfupdate.NewStandaloneInstaller(install)
	if err != nil {
		return nil, fmt.Errorf("updateclient: installer: %w", err)
	}

	var installer selfupdate.Installer = standalone
	if opts.Managed {
		if installer, err = managedInstaller(opts.Product, standalone, opts.Health); err != nil {
			return nil, err
		}
	}

	transformer, err := newTransformer(opts.Product, opts.CodesignIdentity)
	if err != nil {
		return nil, err
	}

	return selfupdate.New(selfupdate.Config{
		Source:      source,
		Versions:    selfupdate.NewStrictVersionPolicy(),
		Assets:      selector,
		Unpacker:    unpacker,
		Transformer: transformer,
		Installer:   installer,
		Reporter:    selfupdate.DiscardReporter(),
		Confirmer:   selfupdate.NonInteractiveConfirmer(),
		Limits:      limits,
	})
}

func newSource(opts Options, client *http.Client, limits selfupdate.Limits) (selfupdate.ReleaseSource, error) {
	gh := selfupdate.GitHubOptions{
		Repository: Repository,
		Client:     client,
		UserAgent:  selfupdate.UserAgent(opts.Product, opts.Identity.Current()),
		Limits:     limits,
	}
	if opts.APIBaseURL != "" {
		u, err := parseBase(opts.APIBaseURL)
		if err != nil {
			return nil, err
		}
		gh.APIBaseURL = u
	}
	source, err := selfupdate.NewGitHubSource(gh)
	if err != nil {
		return nil, fmt.Errorf("updateclient: github source: %w", err)
	}
	return source, nil
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("updateclient: api base url: %w", err)
	}
	return u, nil
}

// The command's seams are variables for tests; production never changes them.
var (
	// commandIdentity is the running build's identity.
	commandIdentity = buildinfo.Identity
	// commandInteractive is whether an input that is not the process's own
	// stdin can answer a prompt. It cannot; a test sets it to prompt.
	commandInteractive = false
	// newCommandUpdater builds the command's updater.
	newCommandUpdater = func(product string, id buildinfo.Info, health service.HealthOptions) (*selfupdate.Updater, error) {
		return New(Options{
			Product:          product,
			Identity:         id,
			Managed:          true,
			CodesignIdentity: strings.TrimSpace(os.Getenv("MC_CODESIGN_IDENTITY")),
			Health:           health,
		})
	}
)

// ExitError is the exit status of an update command that did not succeed.
// The library has already reported the outcome on the command's streams, so
// main exits with Code and prints nothing more (MADR 0054 PLAN S3,
// Deviation 1).
type ExitError struct {
	// Code is 10 when --check found an update, and 1 for any failure.
	Code int
}

func (e *ExitError) Error() string { return fmt.Sprintf("update: exit status %d", e.Code) }

// NewCommand builds the `update` subcommand both products share. It lives
// here, not in either product's package, so mcremote and mcrelay cannot drift
// apart again; only the product differs between them.
//
// It is go-selfupdate-lib's canonical command bound to cobra (MADR 0054 D8,
// the library's migration guide §5 "With cobra"): the library's flags, its
// request from buildinfo, and cli.Run and cli.Exit, which own the streams,
// the signals, the run timeout and the exit status. Progress, prompts and
// the outcome go to stderr; stdout carries only --json's JSON Lines.
//
// health is the product's own: each product knows how to reach its daemon,
// and this package imports neither product's configuration.
func NewCommand(product string, health service.HealthOptions) *cobra.Command {
	var flags cli.Flags
	cmd := &cobra.Command{
		Use:   "update",
		Args:  cobra.NoArgs,
		Short: "Download and install a GitHub release for " + product,
		Long: `Check GitHub releases for ` + product + `, verify the release checksum,
and replace this executable, restarting the user service when one is installed
and enabled.

Progress, prompts and the outcome are written to stderr. Stdout is empty
unless --json is given, which writes JSON Lines ending in one result object.

Exit status: 0 up to date, declined or installed; 10 an update is available
(with --check); 1 any error. Set GH_TOKEN or GITHUB_TOKEN to raise API rate
limits. Set MC_CODESIGN_IDENTITY on macOS to re-sign the verified binary
before install, which preserves TCC grants (MADR 0069).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			o := cli.StdioOptions()
			o.Stdout, o.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
			if in := cmd.InOrStdin(); in != os.Stdin {
				// A reader that is not the terminal cannot answer a prompt.
				o.Stdin, o.Interactive = in, commandInteractive
			}
			o.JSON = flags.JSON
			o.Timeout = OperationTimeout

			id := commandIdentity()
			req, err := flags.Request(product, id)
			if err != nil {
				return exit(o, selfupdate.Result{}, err)
			}
			u, err := newCommandUpdater(product, id, health)
			if err != nil {
				return exit(o, selfupdate.Result{}, err)
			}
			// The caller's context, never context.Background: Ctrl-C during a
			// download must actually cancel it (MADR 0005 F2). cli.Run adds the
			// signals and the timeout.
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			res, err := cli.Run(ctx, u, req, o)
			return exit(o, res, err)
		},
	}
	flags.Bind(cmd.Flags())
	return cmd
}

// exit reports a run's outcome through cli.Exit, and returns its status as
// an *ExitError, or nil for 0.
func exit(o cli.Options, res selfupdate.Result, err error) error {
	if code := cli.Exit(o.Stderr, res, err); code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}
