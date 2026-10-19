package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// setup-service records which kind of service manager it installed under,
// beside the unit name (MADR 0054 PLAN S5a). A host can have more than one
// supervisor, so `update` and `doctor` read what setup used rather than
// detecting again. With no record, the OS default applies, which is what every
// install made before the record existed used.

// backendRecordName is the record's file name in the product's state
// directory, beside unitRecordName.
const backendRecordName = "service-backend"

func backendRecordPath(product string) (string, error) {
	dir, err := unitRecordDir(product)
	if err != nil {
		return "", fmt.Errorf("service backend record: %w", err)
	}
	return filepath.Join(dir, backendRecordName), nil
}

// ResolveBackend returns the kind setup-service recorded for product. With no
// record it returns the kind detected on this host when setup-service installs
// under it, and otherwise this OS's default. On a systemd host that is the
// default either way; on a runit, s6 or OpenRC host it is that supervisor,
// which a setup that failed before recording may have written (MADR 0054 PLAN
// S5b, Deviation 3).
func ResolveBackend(product string) (Kind, error) {
	if product == "" {
		product = "mcremote"
	}
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	path, err := backendRecordPath(product)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if k := detectKind(); k.installable() {
			return k, nil
		}
		return osDefaultKind(osName), nil
	}
	if err != nil {
		return "", fmt.Errorf("read the service backend record: %w", err)
	}
	k := Kind(strings.TrimSpace(string(b)))
	if !k.installable() {
		return "", fmt.Errorf("the service backend record %s holds %q, which is not a backend setup-service installs", path, string(k))
	}
	return k, nil
}

// recordBackend records k as product's backend.
func recordBackend(product string, k Kind) error {
	path, err := backendRecordPath(product)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		return fmt.Errorf("prepare %s: %w", dir, err)
	}
	return writeUnitAtomic(dir, path, []byte(string(k)+"\n"), 0o600)
}

// forgetBackend removes product's backend record.
func forgetBackend(product string) error {
	path, err := backendRecordPath(product)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the service backend record: %w", err)
	}
	return nil
}
