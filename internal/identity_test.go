package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// identityCommit is the optional commit after an identity line's kind.
const identityCommit = `( [0-9a-f]{12}(-dirty)?)?`

// makeProduct runs the real Makefile target for product with vars, building
// into a temporary directory, and returns the binary.
func makeProduct(t *testing.T, product string, vars ...string) string {
	t.Helper()
	target, binVar := "build-remote", "BIN"
	if product == "mcrelay" {
		target, binVar = "build-relay", "BIN_RELAY"
	}
	bin := filepath.Join(t.TempDir(), product)
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Forward slashes: the recipe passes the path to the shell unquoted, and a
	// shell drops the backslashes of a Windows path.
	args := append([]string{"-C", "..", "--no-print-directory", target, binVar + "=" + filepath.ToSlash(bin)}, vars...)
	cmd := exec.Command("make", args...)
	cmd.Env = append(os.Environ(), "MC_CODESIGN_IDENTITY=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return bin
}

// requireIdentityLine runs `version` and `--version` and matches each first
// line.
func requireIdentityLine(t *testing.T, bin string, want *regexp.Regexp) {
	t.Helper()
	for _, arg := range []string{"version", "--version"} {
		out, err := exec.Command(bin, arg).Output()
		if err != nil {
			t.Fatalf("%s %s: %v", filepath.Base(bin), arg, err)
		}
		first, _, _ := strings.Cut(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
		if !want.MatchString(first) {
			t.Errorf("%s %s first line = %q, want %s", filepath.Base(bin), arg, first, want)
		}
	}
}

// TestReleaseBuildReportsTheTag builds each product as the release lane does,
// `make build-remote|build-relay VERSION=<tag> BUILD_KIND=release`, and
// requires buildinfo.Identity() as the first line of `version` and
// `--version`: the line the library's build workflow and installers run the
// command for (MADR 0054 S2, draft amendment 3; building-releases guide §3).
//
// buildinfo reports "<tag> (release)" only when the version stamp is the tag
// and the kind stamp is exactly "release", the two values buildinfo.LDFlags
// sets. The line is the proof: a -trimpath build, as every Makefile build is,
// records no -ldflags for `go version -m` to read.
func TestReleaseBuildReportsTheTag(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) {
			bin := makeProduct(t, product, "VERSION=v9.9.9", "BUILD_KIND=release")
			requireIdentityLine(t, bin, regexp.MustCompile(`^v9\.9\.9 \(release\)`+identityCommit+`$`))
		})
	}
}

// TestLocalBuildReportsLocal: a plain `make` build is a local build, which
// the updater does not replace without --force.
func TestLocalBuildReportsLocal(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) {
			bin := makeProduct(t, product)
			requireIdentityLine(t, bin, regexp.MustCompile(`^\S+ \(local\)`+identityCommit+`$`))
		})
	}
}
