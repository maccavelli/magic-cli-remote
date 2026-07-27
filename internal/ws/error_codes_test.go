package ws_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// parsePackageFiles parses this package's production sources (test files are
// excluded: they legitimately use invented codes as negative fixtures).
func parsePackageFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no production sources parsed — the guard would pass vacuously")
	}
	return fset, files
}

// errorCodeEmitters are the helpers whose 4th argument (index 3) is the wire
// error code: writeError(ctx, c, id, code, msg), writePairError(same shape),
// writeSessionErr(ctx, c, id, fallbackCode, err).
var errorCodeEmitters = map[string]bool{
	"writeError":      true,
	"writePairError":  true,
	"writeSessionErr": true,
}

// TestWSErrorCodesAreRegistered walks this package's syntax tree and asserts
// every string literal used as a wire error code is registered in
// protocol.ErrorCodes() (MADR 0036 D4).
//
// Registration is what makes the codes documentable: TestErrorCodesAreDocumented
// asserts the registry appears in docs/protocol-v1.md, so an unregistered
// literal would be an undocumented code on the wire — the failure mode that let
// twelve codes ship unspecified.
//
// Two shapes are recognised, which between them cover every emit site:
//
//   - argument index 3 of the helpers in errorCodeEmitters;
//   - a string literal assigned to an identifier named `code` (how
//     writeAuthError and writeSessionErr select theirs).
//
// This is an AST walk rather than a regex so it is insensitive to formatting.
func TestWSErrorCodesAreRegistered(t *testing.T) {
	registered := make(map[string]bool, len(protocol.ErrorCodes()))
	for _, c := range protocol.ErrorCodes() {
		registered[c] = true
	}

	fset, files := parsePackageFiles(t)

	var found []string
	note := func(lit *ast.BasicLit) {
		if lit == nil || lit.Kind != token.STRING {
			return
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil || v == "" {
			return
		}
		if !registered[v] {
			found = append(found, v+" at "+fset.Position(lit.Pos()).String())
		}
	}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				sel, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || !errorCodeEmitters[sel.Sel.Name] || len(node.Args) < 4 {
					return true
				}
				if lit, ok := node.Args[3].(*ast.BasicLit); ok {
					note(lit)
				}
			case *ast.AssignStmt:
				// `code = "x"` and `code, msg = "x", "y"`: take the RHS at
				// the same index as the `code` identifier on the LHS.
				for i, lhs := range node.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name != "code" || i >= len(node.Rhs) {
						continue
					}
					if lit, ok := node.Rhs[i].(*ast.BasicLit); ok {
						note(lit)
					}
				}
			}
			return true
		})
	}

	slices.Sort(found)
	for _, f := range slices.Compact(found) {
		t.Errorf("unregistered error code %s — add it to protocol.ErrorCodes() "+
			"and document it in docs/protocol-v1.md", f)
	}
}

// TestErrorCodeEmittersStillExist guards the walk above against silent decay:
// if a helper is renamed, errorCodeEmitters stops matching and the test would
// pass vacuously while checking nothing.
func TestErrorCodeEmittersStillExist(t *testing.T) {
	_, files := parsePackageFiles(t)

	declared := map[string]bool{}
	for _, file := range files {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				declared[fn.Name.Name] = true
			}
		}
	}
	for name := range errorCodeEmitters {
		if !declared[name] {
			t.Errorf("errorCodeEmitters names %q but this package declares no such "+
				"function — the AST guard is no longer checking it", name)
		}
	}
	if !declared["writeAuthError"] {
		t.Error("writeAuthError is gone; the `code` assignment shape may no longer apply")
	}
}
