package grok

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sdkModule is go-llmprovider-sdk, which only this package may import, for
// grok's static floor (MADR 0235 D4, D5).
const sdkModule = "github.com/maccavelli/go-llmprovider-sdk"

// sdkAllowed is every SDK import this package may make, with the only names
// its production code may use from each: the floor needs the grok id, its
// static list, its rank order and its display names, and nothing that
// fetches, lists or caches (MADR 0235 D4, as amended 2026-11-06).
var sdkAllowed = map[string]map[string]bool{
	sdkModule + "/llmprovider":         {"ProviderGrok": true},
	sdkModule + "/llmprovider/catalog": {"Static": true, "SortByRank": true, "LabelName": true},
}

// TestSDKIsImportedOnlyByGrok walks every Go file of the module. A file
// outside this package that imports the SDK fails; so does an import here of
// any other SDK package, or a production use of any other SDK name. Test
// files here may use more of the SDK, to check the floor against it.
func TestSDKIsImportedOnlyByGrok(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules", "apps", "bin", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		files++
		rel, _ := filepath.Rel(root, p)
		inGrok := filepath.Dir(p) == here
		mode := parser.ImportsOnly
		production := inGrok && !strings.HasSuffix(p, "_test.go")
		if production {
			mode = parser.SkipObjectResolution
		}
		f, err := parser.ParseFile(fset, p, nil, mode)
		if err != nil {
			return err
		}
		names := map[string]string{}
		for _, imp := range f.Imports {
			ip, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(ip, sdkModule) {
				continue
			}
			switch _, allowed := sdkAllowed[ip]; {
			case !inGrok:
				t.Errorf("%s imports %s; only internal/provider/grok may (MADR 0235 D4)", rel, ip)
			case !allowed:
				t.Errorf("%s imports %s; grok may import only llmprovider and llmprovider/catalog", rel, ip)
			default:
				name := path.Base(ip)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				names[name] = ip
			}
		}
		if !production || len(names) == 0 {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ip, ok := names[id.Name]; ok && !sdkAllowed[ip][sel.Sel.Name] {
				t.Errorf("%s uses %s.%s; grok may use only catalog.Static, catalog.SortByRank, "+
					"catalog.LabelName and llmprovider.ProviderGrok (MADR 0235 D4)", rel, id.Name, sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 {
		t.Fatalf("scanned only %d Go files; the walk is not seeing the module", files)
	}
}
