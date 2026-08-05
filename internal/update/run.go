package update

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrUpdateAvailable is returned by Run when --check finds a newer base
// (exit code 10 for CLI wrappers).
var ErrUpdateAvailable = errors.New("update available")

// RunOpts drives a full update (or --check) for one product.
type RunOpts struct {
	Product      string
	LocalVersion string
	Check        bool
	Yes          bool
	Force        bool
	// APIURL overrides GitHub releases URL (tests).
	APIURL string
	// Service for swap; nil skips service cycle.
	Service ServiceControl
	// Out/Err streams.
	Out io.Writer
	Err io.Writer
	// Stdin for confirmation (default os.Stdin).
	In io.Reader
	// CodesignIdentity optional.
	CodesignIdentity string
	// Now for tests.
	Now func() time.Time
}

// Run performs discovery, optional check, download, verify, and swap.
func Run(ctx context.Context, opts RunOpts) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errW := opts.Err
	if errW == nil {
		errW = os.Stderr
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}

	rel, err := Latest(ctx, opts.APIURL)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "latest release: %s (base %s)\n", rel.Tag, rel.Base)
	fmt.Fprintf(out, "local version:  %s\n", opts.LocalVersion)

	_, _, _, localDev, _ := ParseBase(opts.LocalVersion)
	newer, err := NewerBase(rel.Base, opts.LocalVersion)
	if err != nil {
		return err
	}
	if !newer {
		fmt.Fprintln(out, "already up to date")
		return nil
	}
	if localDev && !opts.Force {
		return fmt.Errorf("local build %q looks like a dev suffix; pass --force to update anyway", opts.LocalVersion)
	}
	if opts.Check {
		fmt.Fprintf(out, "update available: %s → %s\n", opts.LocalVersion, rel.Base)
		return ErrUpdateAvailable
	}
	if !opts.Yes {
		fmt.Fprintf(out, "update %s → %s? [y/N] ", opts.LocalVersion, rel.Base)
		line, _ := bufio.NewReader(in).ReadString('\n')
		ans := strings.TrimSpace(strings.ToLower(line))
		if ans != "y" && ans != "yes" {
			fmt.Fprintln(out, "aborted")
			return nil
		}
	}

	asset, ver, err := rel.AssetFor(opts.Product, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	sums, err := rel.SumsAsset(ver)
	if err != nil {
		return err
	}
	dir, err := ExecutableDir()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "downloading %s …\n", asset.Name)
	staged, err := DownloadVerified(ctx, asset, sums, dir, nil)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, opts.Product)
	if exe, eerr := os.Executable(); eerr == nil {
		if base := filepath.Base(exe); base != "" {
			dest = filepath.Join(dir, base)
		}
	}

	active := false
	if opts.Service != nil {
		active, _ = opts.Service.IsActive(opts.Product)
	}
	if err := SwapAndRestart(staged, dest, SwapOpts{
		Product:          opts.Product,
		RestartService:   opts.Service != nil,
		WasActive:        active,
		Service:          opts.Service,
		CodesignIdentity: opts.CodesignIdentity,
		Log:              func(s string) { fmt.Fprintln(out, s) },
	}); err != nil {
		fmt.Fprintln(errW, err.Error())
		return err
	}
	fmt.Fprintf(out, "updated %s to %s\n", opts.Product, rel.Tag)
	return nil
}
