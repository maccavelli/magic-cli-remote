package event_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestTypesEnumerationIsComplete asserts event.Types() lists every `Type`
// constant declared in the package.
//
// Without this, Types() could silently go stale: someone adds a constant,
// forgets the slice, and TestEventTypesAreDocumented passes vacuously for the
// new type — reproducing the exact `session_title` failure MADR 0036 D6 exists
// to prevent, one level further out.
func TestTypesEnumerationIsComplete(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "event.go", nil, 0)
	if err != nil {
		t.Fatalf("parse event.go: %v", err)
	}

	declared := map[string]token.Position{}
	for _, d := range file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Only `X Type = "..."` declarations; the plan-status and
			// priority constants in this file are untyped strings.
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != "Type" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				declared[s] = fset.Position(lit.Pos())
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no `Type` constants found — this guard would pass vacuously")
	}

	listed := map[string]bool{}
	for _, typ := range event.Types() {
		listed[string(typ)] = true
	}

	for value, pos := range declared {
		if !listed[value] {
			t.Errorf("event type %q declared at %s is missing from event.Types() — "+
				"add it there and document it in docs/protocol-v1.md", value, pos)
		}
	}
	for value := range listed {
		if _, ok := declared[value]; !ok {
			t.Errorf("event.Types() lists %q, which is not a declared Type constant", value)
		}
	}
}
