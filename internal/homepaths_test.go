package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// This repository is public, and no committed file may carry a real account
// name (AGENTS.md, "Host identifiers never appear in records"). One account
// name reached 28 files before anything checked
// (0196-MADR-remove-the-development-account-name-and-guard-home-paths.md).
//
// The guard is an allowlist on purpose: a list of the names to keep out
// would itself publish them. Every home path's account segment must be one of
// these placeholders, or written in angle brackets, as <user> is. A segment
// that is not a real account, and is not here, is added here, in review.
var placeholderHomes = map[string]bool{
	"user":       true, // the common placeholder
	"testuser":   true,
	"alice":      true,
	"me":         true,
	"you":        true,
	"u":          true,
	"x":          true,
	"X":          true,
	"dev":        true, // C:\Users\dev in the Windows guide
	"Dev":        true, // "Dev User", a home with a space, in schtasks tests
	"proj":       true,
	"secret":     true,
	"sentinel":   true,
	"mcremote":   true, // a hypothetical home named like the binary (MADR 0116)
	"REPLACE_ME": true, // the shipped launchd plists' placeholder
	"a&b":        true, // plist escaping test
	"Zoë":        true, // non-ASCII home test
	"RUNNER~1":   true, // the GitHub Actions Windows runner's short name
	"%3Cuser%3E": true, // <user>, URL-encoded
}

// homePrefix finds the start of a home directory's account segment: Unix
// /home/ and macOS /Users/, and Windows <drive>:\Users\ written raw, with
// forward slashes, or JSON-escaped.
var homePrefix = regexp.MustCompile(`/home/|/Users/|[A-Za-z]:(?:\\\\|\\|/)Users(?:\\\\|\\|/)`)

// homeSegments returns the account segment of every home path in line. A
// segment ends at a separator, whitespace, quote, bracket or '.', so the
// placeholder in "projects./home/user.trust_level" is "user". An empty segment,
// as in "/home/" alone, and an angle-bracket placeholder are skipped.
func homeSegments(line string) []string {
	var out []string
	for _, loc := range homePrefix.FindAllStringIndex(line, -1) {
		rest := line[loc[1]:]
		if strings.HasPrefix(rest, "<") {
			continue
		}
		end := strings.IndexAny(rest, "/\\ \t\"'`.,;:*()[]{}<>|")
		if end < 0 {
			end = len(rest)
		}
		if seg := rest[:end]; seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// gitIndexPath finds the index of the repository at root. A worktree or
// submodule has a .git file reading "gitdir: <path>" instead of a directory.
func gitIndexPath(root string) (index, gitDir string, err error) {
	gitDir = filepath.Join(root, ".git")
	fi, err := os.Stat(gitDir)
	if err != nil {
		return "", "", err
	}
	if !fi.IsDir() {
		b, err := os.ReadFile(gitDir)
		if err != nil {
			return "", "", err
		}
		target, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
		if !ok {
			return "", "", fmt.Errorf("%s is neither a directory nor a gitdir file", gitDir)
		}
		gitDir = strings.TrimSpace(target)
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
	}
	return filepath.Join(gitDir, "index"), gitDir, nil
}

// objectIDSize is 20 for SHA-1 repositories and 32 for SHA-256 ones, which
// say so in their config ([extensions] objectFormat = sha256).
func objectIDSize(gitDir string) int {
	b, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err == nil && regexp.MustCompile(`(?im)^\s*objectformat\s*=\s*sha256\s*$`).Match(b) {
		return 32
	}
	return 20
}

// indexPaths returns the paths a git index tracks, in index order, each once.
//
// The guard reads the index rather than running `git ls-files` because a
// test may not resolve a bare binary from PATH (MADR 0147 D11; 0196's PLAN,
// Deviation 2). Index versions 2, 3 and 4 are understood
// (gitformat-index(5)): each entry is a fixed stat block, the object ID and a
// 16-bit flags word, then, from version 3, a second flags word when the
// extended bit is set. Versions 2 and 3 store the path NUL-terminated and pad
// the entry to a multiple of 8 bytes; version 4 stores it as a varint count of
// bytes to drop from the previous path and a NUL-terminated suffix.
func indexPaths(b []byte, idSize int) ([]string, error) {
	if len(b) < 12 || string(b[:4]) != "DIRC" {
		return nil, errors.New("not a git index: no DIRC signature")
	}
	version := binary.BigEndian.Uint32(b[4:8])
	if version < 2 || version > 4 {
		return nil, fmt.Errorf("git index version %d is not 2, 3 or 4", version)
	}
	count := int(binary.BigEndian.Uint32(b[8:12]))
	const statSize = 40 // ctime, mtime, dev, ino, mode, uid, gid, size
	pos := 12
	prev := ""
	seen := make(map[string]bool, count)
	var out []string
	for i := 0; i < count; i++ {
		start := pos
		pos += statSize + idSize
		if pos+2 > len(b) {
			return nil, fmt.Errorf("entry %d of %d is truncated", i+1, count)
		}
		flags := binary.BigEndian.Uint16(b[pos : pos+2])
		pos += 2
		if version >= 3 && flags&0x4000 != 0 {
			pos += 2
		}
		var path string
		if version == 4 {
			drop, n := indexVarint(b[pos:])
			if n == 0 || drop > uint64(len(prev)) {
				return nil, fmt.Errorf("entry %d has a bad path prefix", i+1)
			}
			pos += n
			end := bytes.IndexByte(b[pos:], 0)
			if end < 0 {
				return nil, fmt.Errorf("entry %d has no path terminator", i+1)
			}
			path = prev[:len(prev)-int(drop)] + string(b[pos:pos+end])
			pos += end + 1
		} else {
			end := bytes.IndexByte(b[pos:], 0)
			if end < 0 {
				return nil, fmt.Errorf("entry %d has no path terminator", i+1)
			}
			path = string(b[pos : pos+end])
			// The entry, path and at least one NUL, padded to 8 bytes.
			pos = start + (pos-start+end+8)/8*8
		}
		prev = path
		// A conflict lists a path once per stage, and a sparse index lists a
		// directory as "dir/": neither is a file to read twice or at all.
		if !seen[path] && !strings.HasSuffix(path, "/") {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out, nil
}

// indexVarint decodes git's offset varint, returning the value and the bytes
// it used, or 0 bytes if it runs off the end.
func indexVarint(b []byte) (uint64, int) {
	if len(b) == 0 {
		return 0, 0
	}
	c := b[0]
	v := uint64(c & 0x7f)
	n := 1
	for c&0x80 != 0 {
		if n >= len(b) {
			return 0, 0
		}
		c = b[n]
		n++
		v = ((v + 1) << 7) | uint64(c&0x7f)
	}
	return v, n
}

func TestTrackedFilesUseOnlyPlaceholderHomes(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	index, gitDir, err := gitIndexPath(root)
	if err != nil {
		t.Fatalf("no git index: %v: the guard cannot read the tracked files", err)
	}
	raw, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("read %s: %v: the guard cannot read the tracked files", index, err)
	}
	files, err := indexPaths(raw, objectIDSize(gitDir))
	if err != nil {
		t.Fatalf("%s: %v", index, err)
	}
	// An index that lists nothing would make this pass while reading nothing.
	// The repository has thousands of tracked files.
	if len(files) < 100 {
		t.Fatalf("the git index lists %d files: the guard is not reading the repository", len(files))
	}

	read := 0
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil || bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
			continue // deleted in the worktree, or not text
		}
		read++
		for i, line := range strings.Split(string(b), "\n") {
			for _, seg := range homeSegments(line) {
				if !placeholderHomes[seg] {
					t.Errorf("%s:%d: home path account %q is not a placeholder: write <user>, "+
						"or, if it is not a real account, add it to placeholderHomes", f, i+1, seg)
				}
			}
		}
	}
	if read < 100 {
		t.Fatalf("read %d text files of %d tracked: the guard is not reading the repository", read, len(files))
	}
}

// buildIndex writes a git index by hand, so indexPaths is checked against the
// format rather than against itself. Each entry is a zeroed stat block and
// object ID, flags carrying the name length (and the extended bit when ext),
// then the path as the version stores it.
func buildIndex(version uint32, idSize int, entries []string, ext map[string]bool) []byte {
	var b bytes.Buffer
	b.WriteString("DIRC")
	_ = binary.Write(&b, binary.BigEndian, version)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(entries)))
	prev := ""
	for _, p := range entries {
		start := b.Len()
		b.Write(make([]byte, 40+idSize))
		flags := uint16(min(len(p), 0xfff))
		if ext[p] {
			flags |= 0x4000
		}
		_ = binary.Write(&b, binary.BigEndian, flags)
		if ext[p] {
			b.Write([]byte{0, 0})
		}
		if version == 4 {
			common := 0
			for common < len(prev) && common < len(p) && prev[common] == p[common] {
				common++
			}
			b.WriteByte(byte(len(prev) - common)) // every drop here is below 128
			b.WriteString(p[common:])
			b.WriteByte(0)
		} else {
			b.WriteString(p)
			size := (b.Len() - start + 8) / 8 * 8 // 1 to 8 NULs
			b.Write(make([]byte, size-(b.Len()-start)))
		}
		prev = p
	}
	b.Write(make([]byte, 20)) // the trailing checksum, which the reader ignores
	return b.Bytes()
}

// TestIndexPathsReadsEveryVersion: the reader must agree with the format, or
// the guard reads the wrong files and passes. The names cover the padding's
// edges (a 1-letter name leaves exactly one NUL, a 2-letter one leaves eight),
// version 3's extended flags word, a SHA-256 object ID, and version 4's prefix
// compression, including a path that drops all of its predecessor.
func TestIndexPathsReadsEveryVersion(t *testing.T) {
	names := []string{"a", "bc", "dir/a.go", "dir/b.go", "dir/sub/c.txt", "e"}
	for _, tc := range []struct {
		name    string
		version uint32
		idSize  int
		ext     map[string]bool
	}{
		{"v2 sha1", 2, 20, nil},
		{"v3 extended flags, sha256", 3, 32, map[string]bool{"bc": true, "dir/b.go": true}},
		{"v4 prefix compression", 4, 20, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := indexPaths(buildIndex(tc.version, tc.idSize, names, tc.ext), tc.idSize)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(names, ",") {
				t.Errorf("paths = %q, want %q", got, names)
			}
		})
	}

	for _, bad := range []struct {
		name string
		raw  []byte
	}{
		{"no signature", []byte("XXXX\x00\x00\x00\x02\x00\x00\x00\x00")},
		{"version 5", buildIndex(5, 20, nil, nil)},
		{"truncated", buildIndex(2, 20, names, nil)[:100]},
	} {
		if _, err := indexPaths(bad.raw, 20); err == nil {
			t.Errorf("%s: read without error", bad.name)
		}
	}
	if got, err := indexPaths(buildIndex(2, 20, nil, nil), 20); err != nil || len(got) != 0 {
		t.Errorf("empty index: %q, %v; want no paths and no error, leaving the guard's floor to fail", got, err)
	}
}
