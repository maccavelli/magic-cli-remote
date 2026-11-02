package acpagent

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

// rawCallees are the calls that put an untyped method on the wire, or pass
// one on to such a call: Host.CallExtension.
var rawCallees = map[string]bool{"rawRequest": true, "RawRequest": true, "SendRequest": true,
	"CallExtension": true}

// rawWrappers are the functions whose method is a parameter they pass on, by
// package; their callers are checked instead.
var rawWrappers = map[string]bool{"acpagent.rawRequest": true, "acpagent.RawRequest": true,
	"acpagent.CallExtension": true}

// TestStandardMethodsAreTypedInAcpagent is PLAN 0184 A3 step 1: it fails when
// a standard (not "_"-prefixed) ACP method reaches an untyped call anywhere in
// acpagent. Only a Spec's named fallbacks may send one, in its agent's package,
// whose own copy of this scan pins them (MADR 0184 D6; 0208 PLAN, P6c
// Deviation 1).
func TestStandardMethodsAreTypedInAcpagent(t *testing.T) {
	sites := 0
	for _, problem := range standardRawCalls(t, "acpagent", ".", &sites) {
		t.Error(problem)
	}
	if sites == 0 {
		t.Fatal("no untyped call was found at all; the scan is not seeing the code")
	}
}

// standardRawCalls parses dir's non-test files and returns one problem per
// untyped call with a literal standard method.
// sites counts every untyped call it inspected.
func standardRawCalls(t *testing.T, pkg, dir string, sites *int) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var problems []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			where := pkg + "." + fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !rawCallees[calleeName(call.Fun)] {
					return true
				}
				*sites++
				if rawWrappers[where] {
					return true // the transport and its wrappers; the method is a parameter
				}
				method, literal := methodArg(call)
				switch {
				case !literal:
					// The one computed name allowed is "_"+method: an extension by
					// construction.
					if !isUnderscorePrefixed(call) {
						problems = append(problems, fset.Position(call.Pos()).String()+": "+where+
							" sends a computed method name that is not provably an extension")
					}
				case !strings.HasPrefix(method, "_"):
					problems = append(problems, fset.Position(call.Pos()).String()+": "+where+
						" sends the standard method "+strconv.Quote(method)+" untyped; use the SDK's typed call, "+
						"or a named fallback in the agent's Spec")
				}
				return true
			})
		}
	}
	return problems
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr: // acp.SendRequest[T]
		return calleeName(f.X)
	}
	return ""
}

// methodArg returns the call's method argument when it is a string literal:
// the second argument of rawRequest/RawRequest(ctx, method, …), or the third of
// SendRequest(conn, ctx, method, …).
func methodArg(call *ast.CallExpr) (string, bool) {
	idx := methodIndex(call)
	if len(call.Args) <= idx {
		return "", false
	}
	lit, ok := call.Args[idx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// methodIndex is the position of the call's method argument:
// SendRequest(conn, ctx, method, …) takes it third, the others (ctx, method, …)
// second.
func methodIndex(call *ast.CallExpr) int {
	if calleeName(call.Fun) == "SendRequest" {
		return 2
	}
	return 1
}

// isUnderscorePrefixed reports whether the call's method argument is `"_" + …`.
func isUnderscorePrefixed(call *ast.CallExpr) bool {
	idx := methodIndex(call)
	if len(call.Args) <= idx {
		return false
	}
	bin, ok := call.Args[idx].(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	lit, ok := bin.X.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `"_"`
}
