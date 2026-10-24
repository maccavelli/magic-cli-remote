package service

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// setup-service --uninstall (MADR 0054 PLAN S5e, gap G4). It is install.sh's
// do_uninstall (scripts/install.sh:960-997): every definition of either
// product, on every service manager this OS has, that runs a program from the
// folder of the binary running the command is stopped and removed; any other
// is reported and left, so two installs in different folders do not remove
// each other. With --from-installer it is the release installer's
// before_uninstall hook, run once per product the installer removes, and it
// looks only at that product (MADR 0054 D11, amendment S9, U1).

// UninstallItem is one definition --uninstall found.
type UninstallItem struct {
	Product string
	Kind    Kind
	Path    string
	// Program is the binary the definition runs, when it could be read.
	Program string
	// Reason says why a definition was left, or why its removal failed.
	Reason string
	// Backup is the definition's backup the removal deleted, if there was
	// one (MADR 0210 D9).
	Backup string
}

// UninstallResult is what --uninstall did.
type UninstallResult struct {
	// Dir is the folder whose definitions were removed.
	Dir string
	// Products are the products looked at.
	Products []string
	// FromInstaller is set when the release installer's before_uninstall
	// hook ran it: the installer deletes the binaries next.
	FromInstaller bool
	Removed       []UninstallItem
	Left          []UninstallItem
	Failed        []UninstallItem
}

// uninstallExeDir is the folder of the binary running the command, symlinks
// resolved. It is a variable for tests.
var uninstallExeDir = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe), nil
}

// uninstallKinds are the service managers --uninstall looks in on osName.
func uninstallKinds(osName string) []Kind {
	switch osName {
	case "linux":
		return []Kind{KindSystemdUser, KindRunit, KindS6, KindOpenRCUser}
	case "darwin":
		return []Kind{KindLaunchdAgent}
	case "windows":
		return []Kind{KindWindowsTask}
	}
	return nil
}

// Uninstall removes every definition of mcremote and mcrelay that runs a
// program from this binary's folder, and reports the rest. A failed removal
// is reported and the others still run; the error says how many failed.
func Uninstall() (UninstallResult, error) {
	return uninstall([]string{"mcremote", "mcrelay"})
}

// UninstallFromInstaller is Uninstall for product alone: the release
// installer's before_uninstall hook, which runs once for each product it
// removes. So removing only mcrelay leaves mcremote's service (MADR 0054
// amendment S9, U1). A failure fails the hook, and the installer then
// removes nothing.
func UninstallFromInstaller(product string) (UninstallResult, error) {
	res, err := uninstall([]string{product})
	res.FromInstaller = true
	return res, err
}

func uninstall(products []string) (UninstallResult, error) {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	res := UninstallResult{Products: products}
	dir, err := uninstallExeDir()
	if err != nil {
		return res, fmt.Errorf("resolve this binary's folder: %w", err)
	}
	res.Dir = dir
	for _, product := range products {
		recorded, err := ResolveUnitName(product)
		if err != nil {
			return res, err
		}
		names := []string{recorded}
		if recorded != product {
			names = append(names, product)
		}
		if osName == "windows" {
			// The task is always named after the product (MADR 0159 D13).
			names = []string{taskNameFor(product)}
		}
		for _, kind := range uninstallKinds(osName) {
			for _, name := range names {
				item, found := findDefinition(kind, product, name)
				if !found {
					continue
				}
				if item.Program == "" || !sameDir(filepath.Dir(item.Program), dir) {
					if item.Reason == "" {
						item.Reason = "runs " + item.Program + ", not a program from " + dir
					}
					res.Left = append(res.Left, item)
					continue
				}
				backup, err := removeDefinition(kind, product, name)
				if err != nil {
					item.Reason = err.Error()
					res.Failed = append(res.Failed, item)
					continue
				}
				item.Backup = backup
				if name == recorded {
					if err := forgetUnitName(product, name); err != nil {
						item.Reason = err.Error()
						res.Failed = append(res.Failed, item)
						continue
					}
					if err := forgetBackend(product); err != nil {
						item.Reason = err.Error()
						res.Failed = append(res.Failed, item)
						continue
					}
				}
				res.Removed = append(res.Removed, item)
			}
		}
	}
	if len(res.Failed) > 0 {
		return res, fmt.Errorf("%d service definition(s) could not be removed", len(res.Failed))
	}
	return res, nil
}

// sameDir reports whether a and b are the same folder, symlinks resolved
// where they exist.
func sameDir(a, b string) bool {
	clean := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return filepath.Clean(p)
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(clean(a), clean(b))
	}
	return clean(a) == clean(b)
}

// findDefinition reports whether kind has a definition named name for
// product, and the program it runs. A definition whose program cannot be read
// is found with an empty Program and a Reason, and is left alone.
func findDefinition(kind Kind, product, name string) (UninstallItem, bool) {
	item := UninstallItem{Product: product, Kind: kind}
	switch kind {
	case KindSystemdUser:
		item.Path = filepath.Join(xdgConfigHome(), "systemd", "user", name+".service")
		body, ok := readDefinition(item.Path)
		if !ok {
			return item, false
		}
		item.Program, item.Reason = programOfUnit(body)
	case KindRunit, KindS6, KindOpenRCUser:
		def := scriptDefFor(kind, name, "")
		item.Path = def.Path
		body, ok := readDefinition(def.Path)
		if !ok {
			return item, false
		}
		rec, why, ok := recoverScript(kind, product, name, body)
		if !ok {
			item.Reason = "its program could not be read: " + why
			return item, true
		}
		item.Program = rec.Opts.Binary
	case KindLaunchdAgent:
		label, err := LaunchdLabel(product, name)
		if err != nil {
			return item, false
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return item, false
		}
		item.Path = filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		body, ok := readDefinition(item.Path)
		if !ok {
			return item, false
		}
		rec, why, ok := recoverPlistOptions(product, name, label, body)
		if !ok {
			item.Reason = "its program could not be read: " + why
			return item, true
		}
		item.Program = rec.Opts.Binary
	case KindWindowsTask:
		item.Path = taskPathPrefix + name
		_, found, err := taskState(name)
		if err != nil || !found {
			return item, false
		}
		xml, err := runSchtasks("/query", "/tn", name, "/xml", "ONE")
		if err != nil {
			item.Reason = "its definition could not be exported: " + err.Error()
			return item, true
		}
		f, err := taskFieldsFromXML(xml)
		if err != nil || f.Command == "" {
			item.Reason = "its program could not be read"
			return item, true
		}
		item.Program = f.Command
	}
	return item, true
}

// readDefinition reads a definition file, reporting whether there is one.
func readDefinition(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || err != nil {
		return "", false
	}
	return string(b), true
}

// programOfUnit reads a unit's ExecStart program.
func programOfUnit(body string) (string, string) {
	for raw := range strings.SplitSeq(body, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok || strings.TrimSpace(k) != "ExecStart" {
			continue
		}
		toks, err := splitExecStart(v)
		if err != nil || len(toks) == 0 {
			return "", "its ExecStart= could not be read"
		}
		return strings.TrimLeft(toks[0], "-@:!+"), ""
	}
	return "", "it has no ExecStart="
}

// removeDefinition stops, disables and deletes one definition, and the backup
// a refresh kept of it, whose path it returns (MADR 0210 D9).
func removeDefinition(kind Kind, product, name string) (string, error) {
	opts := Options{Product: product, UnitName: name}
	var res Result
	var err error
	switch kind {
	case KindSystemdUser:
		res, err = removeSystemd(opts)
	case KindRunit, KindS6, KindOpenRCUser:
		res, err = removeScript(kind, opts)
	case KindLaunchdAgent:
		res, err = removeLaunchdAgent(opts)
	case KindWindowsTask:
		res, err = removeSchtasks(opts)
	default:
		err = fmt.Errorf("no removal for %s", kind)
	}
	return res.RemovedBackup, err
}

// PrintUninstallResult writes what --uninstall did.
func PrintUninstallResult(w io.Writer, res UninstallResult) {
	if len(res.Removed)+len(res.Left)+len(res.Failed) == 0 {
		products := res.Products
		if len(products) == 0 {
			products = []string{"mcremote", "mcrelay"}
		}
		fmt.Fprintf(w, "no service definition of %s was found\n", strings.Join(products, " or "))
		return
	}
	for _, it := range res.Removed {
		fmt.Fprintf(w, "removed: %s %s service %s\n", it.Product, it.Kind, it.Path)
		if it.Backup != "" {
			fmt.Fprintf(w, "removed: %s %s backup %s\n", it.Product, it.Kind, it.Backup)
		}
	}
	for _, it := range res.Left {
		fmt.Fprintf(w, "left:    %s %s service %s — %s\n", it.Product, it.Kind, it.Path, it.Reason)
	}
	for _, it := range res.Failed {
		fmt.Fprintf(w, "failed:  %s %s service %s — %s\n", it.Product, it.Kind, it.Path, it.Reason)
	}
	switch {
	case len(res.Removed) == 0:
	case res.FromInstaller:
		fmt.Fprintln(w, "Configuration and data were left in place. The installer removes the binaries next.")
	default:
		fmt.Fprintln(w, "Configuration and data were left in place. Remove the binaries with the installer's --uninstall.")
	}
}
