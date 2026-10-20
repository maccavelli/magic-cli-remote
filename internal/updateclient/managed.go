package updateclient

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/codesign"
	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// The seams below are variables for tests; production never changes them.
var (
	// newBackend builds the service manager's backend, with the product's
	// health wiring (MADR 0054 D2, D3, D5).
	newBackend = service.NewBackendWith
	// definitionInstalled reports whether a service definition exists. It is
	// asked only when no backend can be built.
	definitionInstalled = service.IsInstalled
	// reconcileRunner runs the reconcile child. Nil means the library's
	// ExecRunner.
	reconcileRunner libservice.Runner
	// restoreUnitBackup puts the previous definition back during a rollback.
	restoreUnitBackup = service.RestoreUnitBackup
)

// reconcileArgs make the newly installed binary re-render its own service
// definition and print the receipt (MADR 0100). The old binary cannot render
// what the new release ships.
var reconcileArgs = []string{"setup-service", "--refresh", "--json"}

// managedInstaller composes the library's managed update for product: its
// backend for this OS, which reports whether the service is enabled, so an
// update starts a stopped service only when it is configured to run (MADR
// 0054 D2, amendment 1); and the library's ExecReconciler, which runs the new
// binary's setup-service --refresh and restores a changed definition in this
// process (D4).
//
// When no backend can be built, an installed definition makes the update
// refuse, because it could neither stop nor restart that service; with no
// definition the update replaces the binary alone (MADR 0054 PLAN S1).
//
// health is the product's probe and, on Windows, its shutdown request; the
// backend's WaitHealthy runs the probe after a new instance has settled
// (MADR 0054 D5, A1, A2, A4).
func managedInstaller(product string, standalone *selfupdate.StandaloneInstaller, health service.HealthOptions) (selfupdate.Installer, error) {
	backend, err := newBackend(product, "", health)
	if err != nil {
		installed, ierr := definitionInstalled(product)
		switch {
		case ierr != nil:
			return nil, fmt.Errorf("updateclient: %s service manager: %w; whether a service definition exists is unknown: %v", product, err, ierr)
		case installed:
			return nil, fmt.Errorf("updateclient: a %s service is installed, but its service manager cannot be reached: %w", product, err)
		}
		return standalone, nil
	}
	rec, err := newReconciler()
	if err != nil {
		return nil, err
	}
	managed, err := selfupdate.NewManagedInstaller(standalone, backend, rec)
	if err != nil {
		return nil, fmt.Errorf("updateclient: managed installer: %w", err)
	}
	return managed, nil
}

// newReconciler runs the new binary's setup-service --refresh --json, whose
// output is the library's receipt, schema 1, field for field
// (internal/cli/service/refresh.go RefreshResult). A rollback restores the
// definition's backup in this process: the old binary knows the definition it
// wrote, and no template is involved (MADR 0054 D4).
func newReconciler() (*libservice.ExecReconciler, error) {
	rec, err := libservice.NewExecReconciler(libservice.ExecOptions{
		Reconcile: reconcileArgs,
		RestoreFunc: func(_ context.Context, _ string, r libservice.Receipt) error {
			return restoreUnitBackup(r.Path, r.Backup)
		},
		Runner: reconcileRunner,
	})
	if err != nil {
		return nil, fmt.Errorf("updateclient: reconciler: %w", err)
	}
	return rec, nil
}

// codesignIdentifier is the identifier each product is signed under: the one
// `make codesign-maybe` uses. TCC keys its grants on it, so a re-signed update
// keeps them (MADR 0069).
func codesignIdentifier(product string) string {
	return "com.magiccliremote." + product
}

// signOptions are the library signer's options for product.
func signOptions(product, identity string) codesign.SignOptions {
	return codesign.SignOptions{Identity: identity, Identifier: codesignIdentifier(product)}
}

// newTransformer re-signs the verified staged binary on macOS when an
// identity is configured, with the library's signer: a fixed identifier and
// /usr/bin/codesign, where the previous transformer took the identifier from
// the staging file's name and codesign from PATH (MADR 0054). It modifies
// bytes already verified against the release, so it runs as a Transformer,
// and the updater records the intended digest change (MADR 0005 F8).
//
// Signing is a macOS concern: elsewhere, and with no identity, there is no
// transform, as before.
func newTransformer(product, identity string) (selfupdate.Transformer, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" || runtime.GOOS != "darwin" {
		return nil, nil
	}
	t, err := codesign.NewSigner(signOptions(product, identity))
	if err != nil {
		return nil, fmt.Errorf("updateclient: codesign: %w", err)
	}
	return t, nil
}
