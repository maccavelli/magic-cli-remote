package codex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestDecodedNamesHaveASchemaSource is MADR 0236 D3.4. Every JSON name a struct in
// this package decodes from (or encodes to) the app-server must be a property
// somewhere in the pinned export. A name with no source there is a guess about
// codex's wire, and a guess is either removed or listed with its evidence.
//
// It works by name, not by path: a wrong guess whose name exists elsewhere in the
// schema passes (0236 F14, and that record's Consequences).
func TestDecodedNamesHaveASchemaSource(t *testing.T) {
	m := mustLoadContractManifest(t)
	known := make(map[string]struct{}, len(m.SchemaProperties))
	for _, name := range m.SchemaProperties {
		known[name] = struct{}{}
	}
	var missing []string
	for _, field := range decodedJSONNames(t, ".") {
		if _, excluded := notAppServerTypes[field.owner]; excluded {
			continue
		}
		if _, excluded := notAppServerTypes[field.file]; excluded {
			continue
		}
		if _, ok := known[field.name]; ok {
			continue
		}
		if _, ok := knownOutsideSchema[field.name]; ok {
			continue
		}
		missing = append(missing, field.String())
	}
	sort.Strings(missing)
	for _, line := range missing {
		t.Errorf("JSON name with no source in the %s schema: %s", m.CodexVersion, line)
	}
}

// notAppServerTypes are the files, and file:owner pairs, whose JSON names are not
// app-server messages, each with the reason. The owner is the named type, or the
// function an anonymous struct sits in.
var notAppServerTypes = map[string]string{
	"contract.go":            "the contract manifest itself, and the schema export's own document shape",
	"adapter.go:authDotJSON": "codex's auth.json credential file, read from disk",
	"capabilities.go:SanitizedCapabilitySnapshot": "this package's capability snapshot, logged and sent to the phone",
	"diagnostics.go:DoctorReport":                 "the projected `codex doctor --json` report sent to the phone",
	"diagnostics.go:DoctorCheck":                  "the projected `codex doctor --json` report sent to the phone",
	"diagnostics.go:DoctorDetail":                 "the projected `codex doctor --json` report sent to the phone",
	"diagnostics.go:DoctorIssue":                  "`codex doctor --json` issue, read and projected",
	"diagnostics.go:rawDoctorReport":              "`codex doctor --json` output, a CLI report with no schema",
	"diagnostics.go:rawDoctorCheck":               "`codex doctor --json` output, a CLI report with no schema",
	"doctorreport.go:doctorReport":                "`codex doctor --json` output, a CLI report with no schema",
	"managed_daemon.go:daemonLifecycle":           "`codex app-server daemon <op>` JSON, a CLI answer with no schema",
	"runtime.go:RuntimeSnapshot":                  "this package's runtime snapshot, sent to the phone",
	"runtime.go:RuntimeWindow":                    "this package's runtime snapshot, sent to the phone",
	"runtime.go:RuntimeUsage":                     "this package's runtime snapshot, sent to the phone",
	"runtime.go:RuntimeModel":                     "this package's runtime snapshot, sent to the phone",
	"runtime.go:RuntimeConfig":                    "this package's runtime snapshot, sent to the phone",
	"runtime.go:RuntimeMCPServer":                 "this package's runtime snapshot, sent to the phone",
	"settings.go:ConfigLayerProjection":           "a sanitized projection of config/read, built here and sent to the phone",
	"settings.go:ConfigPolicyState":               "a sanitized projection of config/read, built here and sent to the phone",
}

// knownOutsideSchema are wire names codex sends, or was believed to send, that no
// export declares, each with its evidence.
var knownOutsideSchema = map[string]string{
	// rpcErrorBody.CapabilityName reads error.data.capability. No release has sent
	// it: the real 0.166.0 and 0.167.0 gate errors carry data: null, and only
	// hand-written fixtures hold it. Removing the read is 0109's proposed D39.
	"capability": "JSON-RPC error data; never observed on the wire (MADR 0109 amendment E3 and E6, D39)",
}

type decodedName struct {
	file, owner, name string
	line              int
}

func (d decodedName) String() string {
	return d.file + ":" + strconv.Itoa(d.line) + " " + d.owner + " json:" + strconv.Quote(d.name)
}

// decodedJSONNames returns the json tag name of every struct field in the
// non-test Go files of dir, with the type or function that owns the struct.
func decodedJSONNames(t *testing.T, dir string) []decodedName {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []decodedName
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, dir+"/"+file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		collect := func(owner string, node ast.Node) {
			ast.Inspect(node, func(n ast.Node) bool {
				st, ok := n.(*ast.StructType)
				if !ok {
					return true
				}
				for _, field := range st.Fields.List {
					if field.Tag == nil {
						continue
					}
					tag, err := strconv.Unquote(field.Tag.Value)
					if err != nil {
						continue
					}
					name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
					if name == "" || name == "-" {
						continue
					}
					out = append(out, decodedName{file: file, owner: file + ":" + owner, name: name,
						line: fset.Position(field.Pos()).Line})
				}
				return true
			})
		}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						collect(ts.Name.Name, ts.Type)
					}
				}
			case *ast.FuncDecl:
				if d.Body != nil {
					collect(d.Name.Name, d.Body)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no json tags found: the AST walk is broken")
	}
	return out
}
