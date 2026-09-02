package updateclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maccavelli/mcplib/selfupdate"
)

// Repository is the single GitHub repository both products are published from.
var Repository = selfupdate.Repository{Owner: "maccavelli", Name: "magic-cli-remote"}

// Platforms is this product's frozen release matrix. A platform outside this
// set is rejected before any network call; the set is never interpolated from
// runtime values.
var Platforms = []selfupdate.Platform{
	{OS: "linux", Arch: "amd64"},
	{OS: "linux", Arch: "arm64"},
	{OS: "darwin", Arch: "arm64"},
	{OS: "windows", Arch: "amd64"},
}

// OperationTimeout bounds one whole update: discovery, download, install and
// any managed recovery.
const OperationTimeout = 15 * time.Minute

// Options describe one product's update command.
type Options struct {
	// Product is the executable basename, "mcremote" or "mcrelay".
	Product string
	// RawVersion and RawBuildKind are the linker-stamped identity.
	RawVersion   string
	RawBuildKind string
	// Out receives human progress; Err receives prompts and diagnostics.
	Out io.Writer
	Err io.Writer
	// In is the confirmation source. A nil value disables interactive
	// confirmation, which makes apply without --yes fail rather than hang.
	In *os.File
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
}

// New builds the updater for one product. Every security-relevant component is
// composed explicitly here rather than hidden behind a convenience
// constructor, so this file is the one place to audit what the update trusts.
func New(opts Options) (*selfupdate.Updater, error) {
	if opts.Product == "" {
		return nil, fmt.Errorf("updateclient: product is required")
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

	selector, err := selfupdate.NewExactAssetSelector(Platforms)
	if err != nil {
		return nil, fmt.Errorf("updateclient: asset selector: %w", err)
	}

	standalone, err := selfupdate.NewStandaloneInstaller(selfupdate.InstallOptions{})
	if err != nil {
		return nil, fmt.Errorf("updateclient: installer: %w", err)
	}

	var installer selfupdate.Installer = standalone
	if opts.Managed {
		managed, err := selfupdate.NewManagedInstaller(standalone, &Lifecycle{}, &Reconciler{})
		if err != nil {
			return nil, fmt.Errorf("updateclient: managed installer: %w", err)
		}
		installer = managed
	}

	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	errw := opts.Err
	if errw == nil {
		errw = io.Discard
	}

	return selfupdate.New(selfupdate.Config{
		Source:      source,
		Versions:    selfupdate.NewStrictVersionPolicy(),
		Assets:      selector,
		Transformer: newCodesignTransformer(opts.CodesignIdentity),
		Installer:   installer,
		Reporter:    selfupdate.NewTextReporter(out),
		Confirmer:   selfupdate.NewTerminalConfirmer(opts.In, errw),
		Limits:      limits,
	})
}

func newSource(opts Options, client *http.Client, limits selfupdate.Limits) (selfupdate.ReleaseSource, error) {
	gh := selfupdate.GitHubOptions{
		Repository: Repository,
		Client:     client,
		UserAgent:  opts.Product + "/" + opts.RawVersion,
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

// Request builds the shared request from this product's stamped identity and
// the command's flags. The legacy BASE.N normalization happens here and
// nowhere else.
func (o Options) Request(targetVersion string, check, force, yes bool) selfupdate.Request {
	version, kind := NormalizeInstalled(o.RawVersion, o.RawBuildKind)
	return selfupdate.Request{
		Product:        o.Product,
		CurrentVersion: version,
		CurrentBuild:   kind,
		TargetVersion:  targetVersion,
		CheckOnly:      check,
		Force:          force,
		Yes:            yes,
	}
}

// Context derives the bounded operation context from the caller's context. It
// never replaces the caller's context with a background one, so Ctrl-C during
// an update actually cancels it.
func Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, OperationTimeout)
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("updateclient: api base url: %w", err)
	}
	return u, nil
}

// NewCommand builds the `update` subcommand both products share. It lives
// here, not in either product's package, so mcremote and mcrelay cannot drift
// apart again; only the stamped identity differs between them.
//
// The command owns flag parsing and stream selection and nothing else.
// Discovery, verification, staging, replacement, rollback and exit semantics
// all live in mcplib/selfupdate.
func NewCommand(product string, identity func() (rawVersion, rawBuildKind string)) *cobra.Command {
	var (
		check, yes, force bool
		targetVersion     string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Args:  cobra.NoArgs,
		Short: "Download and install a GitHub release for " + product,
		Long: `Check GitHub releases for ` + product + `, verify the release checksum,
and replace this executable, restarting the user service when one is installed.

Exit codes: 0 = up to date or declined, 10 = --check found an actionable
target, 1 = error. Set GH_TOKEN or GITHUB_TOKEN to raise API rate limits. Set
MC_CODESIGN_IDENTITY on macOS to re-sign the verified binary before install,
which preserves TCC grants (MADR 0069).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rawVersion, rawBuildKind := identity()
			opts := Options{
				Product:          product,
				RawVersion:       rawVersion,
				RawBuildKind:     rawBuildKind,
				Out:              cmd.OutOrStdout(),
				Err:              cmd.ErrOrStderr(),
				In:               stdinFile(cmd),
				Managed:          true,
				CodesignIdentity: strings.TrimSpace(os.Getenv("MC_CODESIGN_IDENTITY")),
			}
			updater, err := New(opts)
			if err != nil {
				return err
			}
			// The caller's context, never context.Background: Ctrl-C during a
			// download must actually cancel it (MADR 0005 F2).
			ctx, cancel := Context(cmd.Context())
			defer cancel()
			_, err = updater.Run(ctx, opts.Request(targetVersion, check, force, yes))
			return err
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report only; exit 0 up to date, 10 actionable, 1 error")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the selected operation without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "replace a local build, or reinstall the selected version")
	cmd.Flags().StringVar(&targetVersion, "version", "", "install this exact release tag (vX.Y.Z); a lower tag is an explicit rollback")
	return cmd
}

// stdinFile returns the real terminal when the command reads it, and nil
// otherwise. A nil confirmer source makes a non-interactive apply without
// --yes fail with an actionable error instead of hanging or silently
// replacing the binary.
func stdinFile(cmd *cobra.Command) *os.File {
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		return f
	}
	return nil
}
