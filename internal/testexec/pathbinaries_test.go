package testexec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoTestResolvesABareBinaryName is the class guard for MADR 0147 D11.
//
// A test that calls exec.Command/exec.LookPath with a bare name — no path
// separator — is asking PATH to find it, and PATH is a property of the shell,
// not of the platform. Git Bash puts C:\Program Files\Git\usr\bin on PATH and
// PowerShell does not, so such a test runs under one and skips or fails under
// the other, on the same commit and machine. CI never sees it, because
// ci.yml pins `shell: bash` for the Windows lane.
//
// Three of these were found by hand (F4, F14) and each was silent in a
// different way: one failed loudly, one passed while asserting nothing, one
// skipped. MADR 0147 D8 first rejected this guard on the grounds that a
// denylist of binary names rots, and that objection was right — so this is not
// a denylist. It is a rule about the *shape* of a call, with exemptions that
// are structural:
//
//   - files carrying a live_* build constraint resolve the real CLI on
//     purpose, and a new live test file carries the tag by construction;
//   - files with a platform filename suffix only build on that platform;
//   - "go" is allowed, because `go test` cannot have started without it.
//
// D11 carries its own falsification: if this guard ever needs a third
// hand-maintained name in allowedBareNames, D8's objection has won and the
// guard should be deleted rather than extended.
func TestNoTestResolvesABareBinaryName(t *testing.T) {
	root := moduleRoot(t)

	var scanned, skippedLive, skippedPlatform int
	var violations []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata holds fixtures, not compiled tests.
			if name := d.Name(); name == ".git" || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if hasPlatformSuffix(d.Name()) {
			skippedPlatform++
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		if hasLiveBuildTag(f) {
			skippedLive++
			return nil
		}
		scanned++

		rel, _ := filepath.Rel(root, path)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := execCallName(call)
			if !ok {
				return true
			}
			arg, ok := firstStringLiteral(call, name)
			if !ok {
				return true
			}
			if strings.ContainsAny(arg, `/\`) || allowedBareNames[arg] {
				return true
			}
			violations = append(violations, filepath.ToSlash(rel)+":"+
				strconv.Itoa(fset.Position(call.Pos()).Line)+
				": exec."+name+"("+strconv.Quote(arg)+")")
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// A scan that silently matched nothing is the failure mode these
	// source-walking tests actually have (MADR 0147 F8 was exactly that), so
	// assert the walk did work before trusting an empty result.
	if scanned < 50 {
		t.Fatalf("only scanned %d test files; the walk is broken, not the tree", scanned)
	}
	if skippedLive == 0 {
		t.Error("no live_* files were skipped; the build-tag exemption is not matching, " +
			"which would make this guard fail on tests that resolve a real CLI by design")
	}

	for _, v := range violations {
		t.Errorf("%s resolves a bare binary name from PATH: it will behave "+
			"differently under Git Bash and PowerShell, and CI (shell: bash) "+
			"cannot see the difference. Re-exec the test binary as a helper "+
			"process instead — see MADR 0147 D9/D10.", v)
	}
	t.Logf("scanned %d test files (%d live_* skipped, %d platform-specific skipped)",
		scanned, skippedLive, skippedPlatform)
}

// allowedBareNames is the exemption list, and it is meant to stay this short.
// See the falsification note above before adding to it.
var allowedBareNames = map[string]bool{
	"go": true, // the toolchain running this very test
}

// execCallName reports the exec function being called, if it is one of the
// three that resolve through PATH.
func execCallName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return "", false
	}
	switch sel.Sel.Name {
	case "Command", "CommandContext", "LookPath":
		return sel.Sel.Name, true
	}
	return "", false
}

// firstStringLiteral returns the name argument if it is a literal string.
// A non-literal (a variable, os.Executable(), a helper call) is exactly what
// this guard wants people to use, so it is not a violation.
func firstStringLiteral(call *ast.CallExpr, fn string) (string, bool) {
	idx := 0
	if fn == "CommandContext" {
		idx = 1 // (ctx, name, args...)
	}
	if len(call.Args) <= idx {
		return "", false
	}
	lit, ok := call.Args[idx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// hasPlatformSuffix reports whether the filename constrains the file to one
// GOOS, in which case resolving a binary that exists there is legitimate.
func hasPlatformSuffix(name string) bool {
	for _, suf := range []string{
		"_windows_test.go", "_linux_test.go", "_darwin_test.go",
		"_unix_test.go", "_js_test.go", "_wasip1_test.go",
	} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

// hasLiveBuildTag reports whether the file is behind a live_* build
// constraint. Those tests drive the real provider CLIs, so resolving one from
// PATH is their entire purpose.
func hasLiveBuildTag(f *ast.File) bool {
	for _, group := range f.Comments {
		for _, c := range group.List {
			text := c.Text
			if !strings.HasPrefix(text, "//go:build") && !strings.HasPrefix(text, "// +build") {
				continue
			}
			if strings.Contains(text, "live_") {
				return true
			}
		}
	}
	return false
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod, so the guard covers the whole module rather than this package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}
