package updateclient

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// G1 of MADR 0207 amendment D (D35). The update replaces the binary while the
// old daemon still runs, then stops it (D33). That is safe only while the
// daemon never starts its own executable while it serves: after the swap, it
// would start the NEW binary as part of the OLD daemon. This test holds that
// premise over every non-test file under cmd and internal.

// ownExecutableAllowlist names the files that may find or start the running
// product's own executable, each with the reason it never does so while the
// daemon serves. An entry that no longer matches anything fails too, as
// internal/procutil's spawn-site allowlist does.
var ownExecutableAllowlist = map[string]string{
	"internal/cli/doctor.go": "`doctor` is a command run from a shell; it reads the path of the " +
		"handoff's result file beside this binary, and the daemon never runs it (the daemon's own " +
		"doctor call runs Codex's binary)",
	"internal/cli/service/setup.go": "`setup-service` runs from a shell, or as the update's reconcile " +
		"child, which is the new binary by design; it writes this binary's path into the definition",
	"internal/cli/service/refresh.go": "`setup-service --refresh` runs from a shell or as the update's " +
		"reconcile child; it compares the definition's binary with this one (binaryWarning)",
	"internal/cli/service/uninstall.go": "`setup-service --remove` runs from a shell; it checks that " +
		"the definition it removes names this binary",
}

// ownExecutableProducts are the program names a spawn of the product itself
// would use.
var ownExecutableProducts = []string{"mcremote", "mcrelay"}

const procutilPath = "github.com/maccavelli/magic-cli-remote/internal/procutil"

// TestNothingRunsItsOwnExecutableWhileServing is D35's G1: no non-test file
// outside the allowlist refers to os.Executable or os.Args[0], or starts the
// product by name through procutil.Command. Imports are resolved per file,
// so an alias cannot hide a use.
func TestNothingRunsItsOwnExecutableWhileServing(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	used := map[string]bool{}
	var violations []string
	files := 0

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
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			key := filepath.ToSlash(rel)
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files++
			for _, site := range ownExecutableSites(fset, file) {
				if _, ok := ownExecutableAllowlist[key]; ok {
					used[key] = true
					continue
				}
				violations = append(violations, "\t"+site)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", walkRoot, err)
		}
	}
	if files == 0 {
		t.Fatal("no non-test Go file was checked")
	}

	if len(violations) > 0 {
		t.Errorf("%d site(s) find or start the product's own executable:\n%s\n\n"+
			"The update replaces the binary before it stops the service (MADR 0207 D33), so a "+
			"daemon that ran its own executable while serving would run the new binary inside the "+
			"old daemon. If the site never runs while the daemon serves, add its file to "+
			"ownExecutableAllowlist with the reason.", len(violations), strings.Join(violations, "\n"))
	}
	for path, reason := range ownExecutableAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("ownExecutableAllowlist[%q] has no reason", path)
		}
		if !used[path] {
			t.Errorf("ownExecutableAllowlist[%q] no longer finds or starts the product's own executable: "+
				"delete the entry rather than leaving an exemption nothing needs", path)
		}
	}
}

// ownExecutableSites returns each place in file that refers to os.Executable,
// indexes os.Args with 0, or calls procutil.Command with the product's name
// as the program, as "file:line:col: what".
func ownExecutableSites(fset *token.FileSet, file *ast.File) []string {
	osNames := importNames(file, "os")
	procutilNames := importNames(file, procutilPath)
	var sites []string
	add := func(n ast.Node, what string) {
		sites = append(sites, fset.Position(n.Pos()).String()+": "+what)
	}
	// A dot import would let a bare Executable or Command escape the checks
	// below, so it is a site of its own.
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && (p == "os" || p == procutilPath) &&
			imp.Name != nil && imp.Name.Name == "." {
			add(imp, "dot import of "+p)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			// A reference, called or not: a function value is as good as a call.
			if isPkgSelector(n, osNames, "Executable") {
				add(n, "os.Executable")
			}
		case *ast.IndexExpr:
			if sel, ok := n.X.(*ast.SelectorExpr); ok && isPkgSelector(sel, osNames, "Args") {
				if lit, ok := n.Index.(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value == "0" {
					add(n, "os.Args[0]")
				}
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || !isPkgSelector(sel, procutilNames, "Command") || len(n.Args) < 2 {
				return true
			}
			// procutil.Command(ctx, name, args...): the program is the second.
			if lit, ok := n.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if name, err := strconv.Unquote(lit.Value); err == nil && slices.Contains(ownExecutableProducts, name) {
					add(n, "procutil.Command(…, "+lit.Value+", …)")
				}
			}
		}
		return true
	})
	return sites
}

// importNames returns the names file refers to the package at path by: its
// import's own name, or the path's last element. A file that does not import
// it has none.
func importNames(file *ast.File, path string) []string {
	var names []string
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != path {
			continue
		}
		switch {
		case imp.Name == nil:
			names = append(names, path[strings.LastIndex(path, "/")+1:])
		case imp.Name.Name != "_":
			names = append(names, imp.Name.Name)
		}
	}
	return names
}

// isPkgSelector reports whether sel is <name>.<member> for one of names.
func isPkgSelector(sel *ast.SelectorExpr, names []string, member string) bool {
	id, ok := sel.X.(*ast.Ident)
	return ok && sel.Sel.Name == member && slices.Contains(names, id.Name)
}
