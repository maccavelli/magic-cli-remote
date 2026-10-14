package internal

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The ACP SDK is a direct requirement of a released tag (MADR 0184 D1):
// github.com/maccavelli/acp-go-sdk2 at an exact vX.Y.Z, never replaced, and the
// Coder module it replaced is neither required nor imported. A pseudo-version
// or prerelease names a commit rather than a reviewed release, and a replace
// would let the build change without one.
const (
	acpModule = "github.com/maccavelli/acp-go-sdk2"
	// coderACP is split so this file's own source never matches the import scan.
	coderACP = "github.com/coder/" + "acp-go-sdk"
)

var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// isACPModule reports whether a module path is an ACP SDK, the current one or
// its predecessors (the Coder module and the owner's fork of it).
func isACPModule(path string) bool {
	return path == acpModule || path == coderACP || strings.HasSuffix(path, "/acp-go-sdk")
}

// goModProblems returns every reason the go.mod text is unacceptable. It
// understands both the single-line and the block form of require and replace.
//
// Parsed by hand on purpose: golang.org/x/mod/modfile is only an indirect
// dependency, and importing it would change go.mod for the sake of a test.
func goModProblems(gomod string) []string {
	var problems []string
	block := ""
	sawACP := false
	for n, raw := range strings.Split(gomod, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "replace (" || line == "require (":
			block = strings.TrimSuffix(line, " (")
			continue
		case block != "" && line == ")":
			block = ""
			continue
		case strings.HasPrefix(line, "replace "):
			problems = append(problems, replaceProblems(n, raw, strings.TrimPrefix(line, "replace "))...)
			continue
		case strings.HasPrefix(line, "require "):
			p, saw := requireProblems(n, raw, strings.TrimPrefix(line, "require "))
			problems, sawACP = append(problems, p...), sawACP || saw
			continue
		case line == "":
			continue
		}
		switch block {
		case "replace":
			problems = append(problems, replaceProblems(n, raw, line)...)
		case "require":
			p, saw := requireProblems(n, raw, line)
			problems, sawACP = append(problems, p...), sawACP || saw
		}
	}
	if !sawACP {
		problems = append(problems, "go.mod: "+acpModule+" is not required")
	}
	return problems
}

func replaceProblems(n int, raw, spec string) []string {
	from, to, ok := strings.Cut(spec, "=>")
	if !ok {
		return []string{lineErr(n, "replace without =>", raw)}
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	var problems []string
	if fields := strings.Fields(from); len(fields) > 0 && isACPModule(fields[0]) {
		problems = append(problems, lineErr(n, "an ACP SDK must not be replaced", raw))
	}
	if isFilesystemPath(to) {
		problems = append(problems, lineErr(n, "replace targets a filesystem path, which only exists on one machine", raw))
	}
	return problems
}

// requireProblems checks one requirement, and reports whether it was the ACP SDK.
func requireProblems(n int, raw, spec string) ([]string, bool) {
	fields := strings.Fields(spec)
	if len(fields) < 2 {
		return nil, false
	}
	switch path, version := fields[0], fields[1]; {
	case path == coderACP:
		return []string{lineErr(n, "the Coder ACP module is replaced by "+acpModule+" (MADR 0184 D1)", raw)}, false
	case path == acpModule && !releaseTag.MatchString(version):
		return []string{lineErr(n, acpModule+" must be required at a release tag vX.Y.Z", raw)}, true
	case path == acpModule:
		return nil, true
	}
	return nil, false
}

// importProblems returns every Go source file under root that imports the Coder module.
func importProblems(root string) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "apps", "node_modules", "build", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), `"`+coderACP+`"`) || strings.Contains(string(data), `"`+coderACP+`/`) {
			problems = append(problems, path+": imports "+coderACP)
		}
		return nil
	})
	return problems, err
}

// isFilesystemPath reports whether a replace target is a local directory: a
// target without a version, or one that starts like a path.
func isFilesystemPath(target string) bool {
	fields := strings.Fields(target)
	if len(fields) == 1 {
		return true // module targets carry a version; directories do not
	}
	t := fields[0]
	return strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.HasPrefix(t, "/") ||
		strings.HasPrefix(t, `.\`) || strings.HasPrefix(t, `..\`) ||
		(len(t) >= 3 && t[1] == ':' && (t[2] == '\\' || t[2] == '/'))
}

func lineErr(n int, why, raw string) string {
	return "go.mod:" + strconv.Itoa(n+1) + ": " + why + ": " + strings.TrimSpace(raw)
}

// TestGoModRequiresTaggedACPDirectly guards the real go.mod and source tree.
func TestGoModRequiresTaggedACPDirectly(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, p := range goModProblems(string(data)) {
		t.Error(p)
	}
	problems, err := importProblems("..")
	if err != nil {
		t.Fatalf("scan imports: %v", err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// TestGoModProblemsRejectsWhatItMust shows the guard can fail: each fixture is
// a go.mod the real test must refuse, or one it must accept.
func TestGoModProblemsRejectsWhatItMust(t *testing.T) {
	const good = "module x\n\ngo 1.27.2\n\nrequire " + acpModule + " v0.17.1\n"
	cases := []struct {
		name  string
		gomod string
		bad   bool
	}{
		{"tagged direct requirement", good, false},
		{"tagged requirement inside a block", "module x\n\nrequire (\n\t" + acpModule + " v0.17.1\n\tgolang.org/x/sys v0.48.0 // indirect\n)\n", false},
		{"an unrelated module replaced by a version", good + "replace example.com/a => example.com/b v1.0.0\n", false},
		{"no ACP requirement", "module x\n\ngo 1.27.2\n", true},
		{"the Coder module required", good + "require " + coderACP + " v0.13.5\n", true},
		{"a pseudo-version", "module x\n\nrequire " + acpModule + " v0.17.2-0.20261013000000-e91d1b8465a1\n", true},
		{"a prerelease", "module x\n\nrequire " + acpModule + " v0.18.0-rc.1\n", true},
		{"the SDK replaced by a tag", good + "replace " + acpModule + " => github.com/someone/acp-go-sdk2 v0.17.1\n", true},
		{"the Coder fork replace", good + "replace " + coderACP + " => github.com/someone/acp-go-sdk v0.13.6-mcr.1\n", true},
		{"the SDK replaced inside a block", good + "replace (\n\t" + acpModule + " => ../acp-go-sdk2\n)\n", true},
		{"a filesystem replace of another module", good + "replace example.com/a => ./vendor/a\n", true},
		{"a Windows path replace", good + `replace example.com/a => C:\src\a` + "\n", true},
		{"a comment mentioning a replace is ignored", "// replace " + acpModule + " => ../x\n" + good, false},
	}
	for _, tc := range cases {
		got := goModProblems(tc.gomod)
		if tc.bad && len(got) == 0 {
			t.Errorf("%s: accepted, but the guard must reject it:\n%s", tc.name, tc.gomod)
		}
		if !tc.bad && len(got) != 0 {
			t.Errorf("%s: rejected, but it is acceptable: %v", tc.name, got)
		}
	}
}

// TestImportProblemsFindsTheCoderImport shows the import scan can fail.
func TestImportProblemsFindsTheCoderImport(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nimport acp \"" + coderACP + "\"\n\nvar _ = acp.ProtocolVersionNumber\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := importProblems(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("import scan found %v, want the one planted import", got)
	}
}
