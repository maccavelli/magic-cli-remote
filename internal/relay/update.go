package relay

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/update"
)

func newUpdateCmd() *cobra.Command {
	var check, yes, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest GitHub release for mcrelay",
		Long: `Check GitHub releases for a newer mcrelay binary, verify SHA256SUMS,
and swap it into place (optionally restarting the user service). Exit codes
with --check: 0 = up to date, 10 = update available, 1 = error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelayUpdate(cmd, check, yes, force)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report only; exit 0 up-to-date, 10 available, 1 error")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "allow updating a dev-suffixed local build")
	return cmd
}

func runRelayUpdate(cmd *cobra.Command, check, yes, force bool) error {
	const product = "mcrelay"
	localVer := strings.TrimPrefix(cliVersion, "v")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	out := cmd.OutOrStdout()

	rel, err := update.Latest(ctx, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "latest release: %s (base %s)\n", rel.Tag, rel.Base)
	fmt.Fprintf(out, "local version:  %s\n", localVer)

	_, _, _, localDev, _ := update.ParseBase(localVer)
	newer, err := update.NewerBase(rel.Base, localVer)
	if err != nil {
		return err
	}
	if !newer {
		fmt.Fprintln(out, "already up to date")
		return nil
	}
	if localDev && !force {
		return fmt.Errorf("local build %q looks like a dev suffix; pass --force to update anyway", localVer)
	}
	if check {
		fmt.Fprintf(out, "update available: %s → %s\n", localVer, rel.Base)
		os.Exit(10)
	}
	if !yes {
		fmt.Fprintf(out, "update %s → %s? [y/N] ", localVer, rel.Base)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) != "y" && strings.TrimSpace(strings.ToLower(line)) != "yes" {
			fmt.Fprintln(out, "aborted")
			return nil
		}
	}

	asset, ver, err := rel.AssetFor(product, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	sums, err := rel.SumsAsset(ver)
	if err != nil {
		return err
	}
	dir, err := update.ExecutableDir()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "downloading %s …\n", asset.Name)
	staged, err := update.DownloadVerified(ctx, asset, sums, dir, nil)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, product)
	if exe, eerr := os.Executable(); eerr == nil {
		if base := filepath.Base(exe); base != "" {
			dest = filepath.Join(dir, base)
		}
	}
	active, _ := service.IsActive(product)
	if err := update.SwapAndRestart(staged, dest, update.SwapOpts{
		Product:          product,
		RestartService:   true,
		WasActive:        active,
		CodesignIdentity: strings.TrimSpace(os.Getenv("MC_CODESIGN_IDENTITY")),
		Log:              func(s string) { fmt.Fprintln(out, s) },
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated %s to %s\n", product, rel.Tag)
	return nil
}
