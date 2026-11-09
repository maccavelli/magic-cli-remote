package protocol_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// minDeclaredErrorCodes is a floor on how many Err* constants errors.go
// declares. Below it the parse has gone wrong (the file moved, or the
// constants did), and the test would otherwise pass while checking nothing.
const minDeclaredErrorCodes = 80

// declaredErrorCodes parses errors.go and returns every exported constant whose
// name begins with Err and whose value is a string literal, keyed by name.
func declaredErrorCodes(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "errors.go", nil, 0)
	if err != nil {
		t.Fatalf("parse errors.go: %v", err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Err") || !name.IsExported() || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}
				out[name.Name] = v
			}
		}
	}
	return out
}

// TestEveryDeclaredErrorCodeIsRegistered asserts that every Err* string
// constant errors.go declares is returned by ErrorCodes() (MADR 0241 D3).
//
// TestWSErrorCodesAreRegistered checks string literals at emit sites, and
// TestErrorCodesAreDocumented checks what the registry lists. Neither sees a
// constant that is declared, passed to writeError, and left out of the
// registry: session_cancel_timeout reached the wire that way. An emit site
// can name only a constant this file declares, so this test closes that gap
// with no list of its own to keep.
func TestEveryDeclaredErrorCodeIsRegistered(t *testing.T) {
	declared := declaredErrorCodes(t)
	if len(declared) < minDeclaredErrorCodes {
		t.Fatalf("found %d Err* string constants in errors.go, want at least %d — "+
			"the guard would pass vacuously", len(declared), minDeclaredErrorCodes)
	}
	registered := map[string]bool{}
	for _, c := range protocol.ErrorCodes() {
		registered[c] = true
	}
	for name, value := range declared {
		if !registered[value] {
			t.Errorf("%s (%q) is declared in errors.go but not in ErrorCodes(): register it "+
				"and document it in docs/guides/protocol-v1.md (MADR 0241 D3)", name, value)
		}
	}
}
