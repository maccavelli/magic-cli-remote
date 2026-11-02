package grok

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestNoExtensionMethodIsWrittenWithoutItsUnderscore is acceptance 4 over the
// call sites rather than over one request. It moved here with grok's
// operations, whose calls it reads (MADR 0208, P6b).
//
// The mistake this catches is not the routing — acpagent's M5 pins that — but
// a *new* extension call added later and written as `x.ai/…` instead of
// `_x.ai/…`. acpagent's rawRequest would classify it as a standard method and
// put it on the standard path, which is precisely the path Phase 9 took
// extension methods off.
func TestNoExtensionMethodIsWrittenWithoutItsUnderscore(t *testing.T) {
	// The model and thinking fallbacks send standard methods untyped by design
	// (MADR 0184 D6); acpagent's TestStandardMethodsAreTypedOutsideGrokFallbacks
	// pins that only they do.
	fallbacks := map[string]bool{"setModelFallback": true, "setThinkingFallback": true}

	fset := token.NewFileSet()
	// parser.ParseDir is deprecated (it ignores build tags). Parse every
	// non-test file directly, which is exactly the set ParseDir returned here.
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
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}

	// literalArg returns the string literal at index i, or "" if it is not one.
	literalArg := func(call *ast.CallExpr, i int) string {
		if len(call.Args) <= i {
			return ""
		}
		lit, ok := call.Args[i].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return ""
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return ""
		}
		return v
	}

	sites := 0
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			// extensionResult passes its method parameter to CallExtension as it
			// is; its callers are checked instead.
			if !ok || fd.Body == nil || fd.Name.Name == "extensionResult" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				where := fset.Position(call.Pos())
				var callee string
				idx := 1
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					// RawRequest and CallExtension(ctx, method, params, out) take
					// the method in full, as the wire carries it.
					if fn.Sel.Name != "RawRequest" && fn.Sel.Name != "CallExtension" {
						return true
					}
					callee = fn.Sel.Name
				case *ast.Ident:
					// extensionResult(ctx, h, method, params, what).
					if fn.Name != "extensionResult" {
						return true
					}
					callee, idx = fn.Name, 2
				default:
					return true
				}
				method := literalArg(call, idx)
				if method == "" {
					t.Errorf("%s: %s is called with a computed method name; this check can only "+
						"reason about literals", where, callee)
					return true
				}
				sites++
				if !strings.HasPrefix(method, "_") && !fallbacks[fd.Name.Name] {
					t.Errorf("%s: %s(%q) takes the standard-method raw path, which only grok's "+
						"named fallbacks may use. If %q is a vendor extension it must be written "+
						"with its leading underscore", where, callee, method, method)
				}
				return true
			})
		}
	}
	if sites < 6 {
		t.Fatalf("found only %d call sites; the scan is not seeing the package", sites)
	}
}
