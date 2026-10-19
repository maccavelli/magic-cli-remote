package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// setup-service records the unit name it installed under, so every later
// control and refresh path manages the definition setup wrote, not one named
// after the product (MADR 0201 D30, 0073 §6.10(d)). A service installed with
// --unit-name could otherwise not be updated, reported on or removed.

// unitRecordName is the record's file name in the product's state directory.
const unitRecordName = "service-unit"

// unitRecordDir is the directory that holds product's record: its state
// directory, which no --data-dir changes, so `update` and `doctor`, which take
// none, find what setup-service wrote. It is a variable for tests.
var unitRecordDir = func(product string) (string, error) {
	p, err := resolveProductPaths(product, "")
	if err != nil {
		return "", err
	}
	return p.StateDir, nil
}

func unitRecordPath(product string) (string, error) {
	dir, err := unitRecordDir(product)
	if err != nil {
		return "", fmt.Errorf("service unit record: %w", err)
	}
	return filepath.Join(dir, unitRecordName), nil
}

// ResolveUnitName returns the unit name setup-service recorded for product,
// or the product name when nothing is recorded. It is the one place a control
// or refresh path learns the name.
func ResolveUnitName(product string) (string, error) {
	if product == "" {
		product = "mcremote"
	}
	path, err := unitRecordPath(product)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return product, nil
	}
	if err != nil {
		return "", fmt.Errorf("read the service unit record: %w", err)
	}
	name := strings.TrimSpace(string(b))
	if !unitNameRe.MatchString(name) || strings.HasSuffix(name, ".service") {
		return "", fmt.Errorf("the service unit record %s holds %q, which is not a unit name", path, name)
	}
	return name, nil
}

// recordUnitName records name as product's unit name.
func recordUnitName(product, name string) error {
	path, err := unitRecordPath(product)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		return fmt.Errorf("prepare %s: %w", dir, err)
	}
	return writeUnitAtomic(dir, path, []byte(name+"\n"), 0o600)
}

// forgetUnitName removes product's record when it names name: removing that
// service removes the record, and removing another instance leaves it.
func forgetUnitName(product, name string) error {
	path, err := unitRecordPath(product)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the service unit record: %w", err)
	}
	if strings.TrimSpace(string(b)) != name {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the service unit record: %w", err)
	}
	return nil
}

// unitNames are one definition's names, as each service manager knows it.
type unitNames struct {
	// Unit is the systemd unit, "<name>.service".
	Unit string
	// Label is the launchd label.
	Label string
	// Task is the scheduled task's name.
	Task string
}

// namesFor returns the definition's names for product. An empty unitName
// means the recorded one (ResolveUnitName).
func namesFor(product, unitName string) (unitNames, error) {
	if unitName == "" {
		var err error
		if unitName, err = ResolveUnitName(product); err != nil {
			return unitNames{}, err
		}
	}
	label, err := LaunchdLabel(product, unitName)
	if err != nil {
		return unitNames{}, err
	}
	return unitNames{Unit: unitName + ".service", Label: label, Task: taskNameFor(unitName)}, nil
}
