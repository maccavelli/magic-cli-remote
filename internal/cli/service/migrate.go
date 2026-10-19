package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// The Windows folder move (MADR 0054 D9, amendment S5-4, PLAN S5g). Today's
// install.ps1 gives each product its own folder and User PATH entry,
// %LOCALAPPDATA%\Programs\<product>\<product>.exe (scripts/install.ps1:324-326,
// :357-369). The library's installer uses one folder named after the release,
// %LOCALAPPDATA%\Programs\magic-cli-remote\<product>.exe. A managed update
// replaces the binary where it is, so the task's refresh moves it: it copies
// each product's binary to the new folder and checks each copy, re-registers
// the task there, moves the PATH entries, and deletes the old copies that
// nothing runs any more.

// installerName names the new folder: the release spec's installer.name
// (MADR 0054 D8, D9).
const installerName = "magic-cli-remote"

// windowsProducts are the binaries the folder holds.
var windowsProducts = []string{"mcremote", "mcrelay"}

// localAppData is %LOCALAPPDATA%, or the user's AppData\Local.
func localAppData() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return base
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Local")
}

// oldWindowsDir is install.ps1's folder for product.
func oldWindowsDir(product string) string {
	return filepath.Join(localAppData(), "Programs", product)
}

// newWindowsDir is the library installer's folder.
func newWindowsDir() string {
	return filepath.Join(localAppData(), "Programs", installerName)
}

// samePath compares Windows paths: case-insensitive, separators and a
// trailing backslash ignored.
func samePath(a, b string) bool {
	norm := func(p string) string {
		p = strings.ReplaceAll(p, "/", `\`)
		return strings.ToLower(strings.TrimRight(p, `\`))
	}
	return norm(a) == norm(b)
}

// inOldLayout reports whether binary is install.ps1's copy of product.
func inOldLayout(product, binary string) bool {
	return samePath(binary, filepath.Join(oldWindowsDir(product), product+".exe"))
}

// rewriteUserPath removes the entries in remove from a ;-separated PATH value
// and appends add once, keeping every other entry, empty ones included, in
// its place. It reports whether the value changed.
func rewriteUserPath(value string, remove []string, add string) (string, bool) {
	var out []string
	have := false
	for _, e := range strings.Split(value, ";") {
		drop := false
		for _, r := range remove {
			if e != "" && samePath(e, r) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		if e != "" && samePath(e, add) {
			have = true
		}
		out = append(out, e)
	}
	if !have {
		if len(out) == 1 && out[0] == "" {
			out = nil
		}
		out = append(out, add)
	}
	next := strings.Join(out, ";")
	return next, next != value
}

// pathStore reads and writes the User PATH and announces a change. The
// Windows one is the registry's HKCU\Environment; it is a variable for tests.
type pathStore interface {
	// get returns the value and its registry type, keeping REG_EXPAND_SZ.
	get() (string, uint32, error)
	set(value string, typ uint32) error
	broadcast()
}

// binaryIdentity is the first line `<binary> version` prints. It is a
// variable for tests.
var binaryIdentity = func(binary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := procutil.Command(ctx, binary, "version").Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", binary, err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), nil
}

// migration is what the folder move did.
type migration struct {
	// To is the new folder; empty when nothing moved.
	To string
	// Notes are told to the operator in the refresh's warnings.
	Notes []string
}

// copyChecked copies src to dst, and checks the copy reports the identity
// src does. An identical dst is kept.
func copyChecked(src, dst string) error {
	want, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if have, err := os.ReadFile(dst); err != nil || !bytes.Equal(have, want) {
		tmp := dst + ".migrating"
		if err := os.WriteFile(tmp, want, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	a, err := binaryIdentity(src)
	if err != nil {
		return err
	}
	b, err := binaryIdentity(dst)
	if err != nil {
		return err
	}
	if a != b {
		return fmt.Errorf("the copy at %s reports %q, its source %q", dst, b, a)
	}
	return nil
}

// copyToNewFolder copies every product's binary that install.ps1 left into
// the new folder and checks each copy. Any failure stops the move: the task
// is then not switched (MADR 0054 amendment S5-4).
func copyToNewFolder() error {
	if err := os.MkdirAll(newWindowsDir(), 0o755); err != nil {
		return err
	}
	for _, p := range windowsProducts {
		src := filepath.Join(oldWindowsDir(p), p+".exe")
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := copyChecked(src, filepath.Join(newWindowsDir(), p+".exe")); err != nil {
			return fmt.Errorf("copy %s: %w", p, err)
		}
	}
	return nil
}

// movePathEntries replaces the per-product User PATH entries with the new
// folder, keeping the value's type and every other entry.
func movePathEntries(store pathStore) (bool, error) {
	value, typ, err := store.get()
	if err != nil {
		return false, err
	}
	var old []string
	for _, p := range windowsProducts {
		old = append(old, oldWindowsDir(p))
	}
	next, changed := rewriteUserPath(value, old, newWindowsDir())
	if !changed {
		return false, nil
	}
	if err := store.set(next, typ); err != nil {
		return false, err
	}
	store.broadcast()
	return true, nil
}

// cleanOldFolders deletes each old copy that has a copy in the new folder and
// that no task still runs, with its .prev and the library's own files for it,
// and the folder when nothing else is left. A copy in use cannot be deleted on
// Windows; it is left for a later refresh.
func cleanOldFolders() []string {
	var notes []string
	for _, p := range windowsProducts {
		if _, err := os.Stat(filepath.Join(newWindowsDir(), p+".exe")); err != nil {
			continue // nothing moved for p
		}
		old := filepath.Join(oldWindowsDir(p), p+".exe")
		if _, err := os.Stat(old); err == nil {
			if runsOldCopy(p, old) {
				notes = append(notes, fmt.Sprintf("%s is kept: the %s task still runs it", old, p))
				continue
			}
			if err := os.Remove(old); err != nil {
				notes = append(notes, fmt.Sprintf("%s is in use and was kept; a later refresh removes it", old))
				continue
			}
			_ = os.Remove(old + ".prev")
		}
		removeLibraryLeftovers(oldWindowsDir(p), p)
		if entries, err := os.ReadDir(oldWindowsDir(p)); err == nil && len(entries) == 0 {
			_ = os.Remove(oldWindowsDir(p))
		}
	}
	return notes
}

// removeLibraryLeftovers deletes the files go-selfupdate-lib kept beside
// product's binary in dir: its lock, its cleanup journal and its backups, all
// named ".<product>.exe.selfupdate…". Once the binary has moved, later updates
// target the new folder, and nothing else would clean them (owner decision,
// MADR 0054 PLAN S5g Deviation 1). A file in use stays. No other file is
// touched.
func removeLibraryLeftovers(dir, product string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := "." + product + ".exe.selfupdate"
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(prefix)) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// runsOldCopy reports whether product's task still runs binary, or cannot be
// read, which keeps the copy.
func runsOldCopy(product, binary string) bool {
	item, found := findDefinition(KindWindowsTask, product, taskNameFor(product))
	if !found {
		return false
	}
	return item.Program == "" || samePath(item.Program, binary)
}

// planMigration decides the folder move for product's task, which runs
// binary. It returns the binary the task should run: the new folder's copy
// when the move can be made, else binary. When the copy fails, nothing is
// switched, and the reason is a note.
func planMigration(product, binary string) (string, migration) {
	var m migration
	if !inOldLayout(product, binary) {
		return binary, m
	}
	if err := copyToNewFolder(); err != nil {
		m.Notes = append(m.Notes, "the move to "+newWindowsDir()+" was not made, and the task is unchanged: "+err.Error())
		return binary, m
	}
	m.To = newWindowsDir()
	return filepath.Join(m.To, product+".exe"), m
}

// finishMigration runs after the task is registered (or found) in the new
// folder: it moves the PATH entries and deletes the old copies.
func finishMigration(m *migration, store pathStore) {
	if changed, err := movePathEntries(store); err != nil {
		m.Notes = append(m.Notes, "the User PATH was not updated: "+err.Error())
	} else if changed {
		m.Notes = append(m.Notes, "the User PATH now has "+newWindowsDir()+" in place of the per-product folders; open a new terminal to see it")
	}
	m.Notes = append(m.Notes, cleanOldFolders()...)
}
