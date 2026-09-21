package procutil_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spawnAllowlist names the non-test files that may start a child process
// without going through procutil.Command, each with the reason it cannot.
//
// A file is listed here only when the constructor genuinely does not fit. "It
// is not on Windows" is not a reason — MADR 0159 F28 found five sites excused
// that way, none of which carried a build tag, so all five were compiled into
// the Windows daemon and every one of them opened a console window.
var spawnAllowlist = map[string]string{
	"internal/updateclient/codesign_darwin.go": "darwin-only: runs codesign(1) during a macOS update, " +
		"never a child of the Windows daemon, and the file does not build on Windows",
}

// bannedSpawns are the calls that start a process behind the constructor's
// back. os.StartProcess is included because banning only exec.Command would
// leave an equivalent bypass one layer down.
var bannedSpawns = map[string][]string{
	"exec": {"Command", "CommandContext"},
	"os":   {"StartProcess"},
}

// TestNoBareSpawnOutsideProcutil is acceptance criterion A17.
//
// The point is not tidiness. Three separate Windows guarantees were absent for
// releases at a time because a helper had to be *remembered*: the job object
// had no caller at all until MADR 0150 (F1), five spawn sites never set a
// process group (0159 F28), and the batch-argument guard was never called
// (0159 F29). This test is what makes the constructor unforgettable.
func TestNoBareSpawnOutsideProcutil(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	used := map[string]bool{}
	var violations []string

	for _, dir := range []string{"cmd", "internal"} {
		walkRoot := filepath.Join(root, dir)
		err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "testdata" || strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			key := filepath.ToSlash(rel)
			if strings.HasPrefix(key, "internal/procutil/") {
				return nil // the constructor lives here
			}

			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				for _, fn := range bannedSpawns[pkg.Name] {
					if sel.Sel.Name != fn {
						continue
					}
					if _, allowed := spawnAllowlist[key]; allowed {
						used[key] = true
						return true
					}
					violations = append(violations, "\t"+fset.Position(call.Pos()).String()+
						": "+pkg.Name+"."+fn)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", walkRoot, err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("%d site(s) start a child process without procutil.Command:\n%s\n\n"+
			"Use procutil.Command(ctx, name, args...). On Windows it also decides whether the "+
			"child needs CREATE_NO_WINDOW, without which a daemon that has no console gives "+
			"every child a visible terminal window (MADR 0159 F22/D19). If a site truly cannot "+
			"use it, add it to spawnAllowlist with the reason.",
			len(violations), strings.Join(violations, "\n"))
	}

	// An allowlist that outlives its reason is how the stale-exemption problem
	// comes back, so a entry that no longer matches anything is a failure too.
	for path, reason := range spawnAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("spawnAllowlist[%q] has no reason", path)
		}
		if !used[path] {
			t.Errorf("spawnAllowlist[%q] no longer contains a banned spawn call: "+
				"delete the entry rather than leaving an exemption nothing needs", path)
		}
	}
}

// windowsSandboxSetupStart is the RPC that provisions Codex's Windows sandbox:
// it creates local accounts and a group, writes firewall rules, and applies
// deny-read ACLs whose only undo record is
// $CODEX_HOME\.sandbox\deny_read_acl_state.json. On 2026-09-19 that mechanism
// left the owner's ~/.codex/config.toml unreadable by its own owner, and Codex
// ships no repair or uninstall subcommand — the sole caller of its cleanup is an
// MSIX uninstall event. So the damage is not something we can undo, which is why
// this is a ban rather than a guideline (MADR 0163 D11/F30, PLAN 0163 C5).
//
// windowsSandbox/readiness is the read-only probe and is deliberately NOT banned.
const windowsSandboxSetupStart = "windowsSandbox/setupStart"

// TestNeverCallsWindowsSandboxSetup is acceptance criterion A17 of PLAN 0163.
func TestNeverCallsWindowsSandboxSetup(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// testdata holds the captured protocol inventory, which names
				// every method by design; a name in an inventory is not a call.
				if name := d.Name(); name == "testdata" || strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(body), windowsSandboxSetupStart) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%s is referenced by non-test file(s) %v.\n\n"+
			"Provisioning the Windows sandbox applies deny-read ACLs that Codex cannot "+
			"undo: there is no repair subcommand, and a missing or empty "+
			"deny_read_acl_state.json orphans every ACE it recorded. Use "+
			"windowsSandbox/readiness to ask whether the sandbox is ready; never start "+
			"provisioning from here (MADR 0163 D11/F30).", windowsSandboxSetupStart, offenders)
	}
}
