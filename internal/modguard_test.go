package internal

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The acp-go-sdk replace must point at a published module pinned to an
// immutable fork tag (MADR 0167 D16, D20). vX.Y.Z-mcr.N is the tag scheme; a
// pseudo-version (vX.Y.Z-0.<timestamp>-<hash>) is rejected because it names a
// commit rather than a reviewed release of the fork.
var acpForkTarget = regexp.MustCompile(`^github\.com/[A-Za-z0-9-]+/acp-go-sdk v\d+\.\d+\.\d+-mcr\.\d+$`)

// replaceProblems returns every reason the go.mod text's replace directives are
// unacceptable. It understands both the single-line and the block form.
//
// Parsed by hand on purpose: golang.org/x/mod/modfile is only an indirect
// dependency, and importing it would change go.mod for the sake of a test.
func replaceProblems(gomod string) []string {
	var problems []string
	inBlock := false
	for n, raw := range strings.Split(gomod, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "replace (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "replace "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "replace "))
		case !inBlock:
			continue
		}
		if line == "" {
			continue
		}
		from, to, ok := strings.Cut(line, "=>")
		if !ok {
			problems = append(problems, lineErr(n, "replace without =>", raw))
			continue
		}
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if isFilesystemPath(to) {
			problems = append(problems, lineErr(n, "replace targets a filesystem path, which only exists on one machine", raw))
			continue
		}
		if strings.HasPrefix(from, "github.com/coder/acp-go-sdk") && !acpForkTarget.MatchString(to) {
			problems = append(problems, lineErr(n, "acp-go-sdk replace is not pinned to an immutable vX.Y.Z-mcr.N fork tag", raw))
		}
	}
	return problems
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

// TestGoModReplacesArePinnedAndRemote guards the real go.mod: a filesystem
// replace would build on one machine and fail everywhere else, and an unpinned
// fork version would let the build change without a reviewed release.
func TestGoModReplacesArePinnedAndRemote(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, p := range replaceProblems(string(data)) {
		t.Error(p)
	}
}

// TestReplaceProblemsRejectsWhatItMust shows the guard can fail: each fixture is
// a go.mod the real test must refuse, and one it must accept.
func TestReplaceProblemsRejectsWhatItMust(t *testing.T) {
	const pinned = "replace github.com/coder/acp-go-sdk => github.com/someone/acp-go-sdk v0.13.6-mcr.1\n"
	cases := []struct {
		name  string
		gomod string
		bad   bool
	}{
		{"pinned fork tag", pinned, false},
		{"no replace at all", "module x\n\ngo 1.26\n", false},
		{"relative path", "replace github.com/coder/acp-go-sdk => ../acp-go-sdk\n", true},
		{"windows path", `replace github.com/coder/acp-go-sdk => C:\src\acp-go-sdk` + "\n", true},
		{"pseudo-version", "replace github.com/coder/acp-go-sdk => github.com/someone/acp-go-sdk v0.13.6-0.20260922101500-eb6e808d9f7f\n", true},
		{"plain upstream-style tag", "replace github.com/coder/acp-go-sdk => github.com/someone/acp-go-sdk v0.13.6\n", true},
		{"filesystem path inside a block", "replace (\n\tgithub.com/other/mod => ./vendor/mod\n)\n", true},
		{"pinned tag inside a block", "replace (\n\t" + strings.TrimPrefix(pinned, "replace ") + ")\n", false},
		{"comment mentioning ../ is ignored", "// see ../acp-go-sdk for the fork\n" + pinned, false},
	}
	for _, tc := range cases {
		got := replaceProblems(tc.gomod)
		if tc.bad && len(got) == 0 {
			t.Errorf("%s: accepted, but the guard must reject it:\n%s", tc.name, tc.gomod)
		}
		if !tc.bad && len(got) != 0 {
			t.Errorf("%s: rejected, but it is acceptable: %v", tc.name, got)
		}
	}
}
