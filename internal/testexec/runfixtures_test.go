package testexec

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestRunFixturesAreWarmed is MADR 0193 D5's guard.
//
// A test that writes a `#!/bin/sh` script and runs it must get the script from
// WriteShellStub, which runs it once before the test can time it. On macOS the
// first run of a new file waits for a Gatekeeper scan, from 150 ms to many
// seconds, and that wait falls inside whatever bound the test puts around the
// run. Hand-written stubs failed three tests' waits and let a fourth pass when
// it should have failed (MADR 0193 E7, E8).
//
// The rule is about the literal, not about whether a script runs: any
// `#!/bin/sh` in a _test.go file outside this package is a violation, unless
// the file is in neverRunFixtures with the reason its scripts are never run.
func TestRunFixturesAreWarmed(t *testing.T) {
	root := moduleRoot(t)
	self := filepath.Join(root, "internal", "testexec")

	var scanned int
	var violations []string
	found := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "testdata" || name == "node_modules" || path == self {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		lines, ferr := shebangLines(path)
		if ferr != nil {
			t.Fatalf("read %s: %v", rel, ferr)
		}
		if len(lines) == 0 {
			return nil
		}
		found[rel] = true
		if _, ok := neverRunFixtures[rel]; ok {
			return nil
		}
		for _, n := range lines {
			violations = append(violations, rel+":"+strconv.Itoa(n))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// A walk that matched nothing would pass; make sure it did the work.
	if scanned < 50 {
		t.Fatalf("only scanned %d test files; the walk is broken, not the tree", scanned)
	}
	for _, v := range violations {
		t.Errorf("%s writes a #!/bin/sh script by hand. Use testexec.WriteShellStub, "+
			"which runs it once before the test can time it (MADR 0193 D1, D5). "+
			"If the script is never run, add the file to neverRunFixtures with the reason.", v)
	}
	var stale []string
	for rel := range neverRunFixtures {
		if !found[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		t.Errorf("%s is in neverRunFixtures but writes no #!/bin/sh script; remove it", rel)
	}
	t.Logf("scanned %d test files; %d write never-run fixtures", scanned, len(neverRunFixtures))
}

// neverRunFixtures lists test files whose `#!/bin/sh` scripts are never run,
// so their first-run scan can never fall inside a bound. Each entry says why.
var neverRunFixtures = map[string]string{
	"internal/cli/service/launchd_sequence_test.go": "Setup only checks the executable bit and renders the path into a plist",
	"internal/cli/service/setup_launchd_test.go":    "Setup only checks the executable bit and renders the path into a plist",
	"internal/cli/service/refresh_test.go":          "Setup only checks the executable bit and renders the path into a unit",
	"internal/cli/service/setup_test.go":            "Setup only checks the executable bit and renders the path into a unit",
}

// shebangLines returns the line numbers in path that contain `#!/bin/sh`.
func shebangLines(path string) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		if strings.Contains(sc.Text(), "#!/bin/sh") {
			out = append(out, n)
		}
	}
	return out, sc.Err()
}
