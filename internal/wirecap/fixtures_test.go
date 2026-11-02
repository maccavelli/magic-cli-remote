package wirecap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The committed wire fixtures are the artefact this package's redaction exists
// to protect, and every miss so far was found by looking at one rather than at
// the rule: the stripped form in MADR 0137 Phase 1, the host separator in
// 0144's amendment, JSON escaping in 0151. This test makes that a check.
//
// It guards the two shapes known to have reached a fixture, and it is honest
// about the rest: it cannot know what identifier an engine will send next
// (0151 F8), so a green run here means "neither known shape is present", not
// "this fixture carries nothing identifying".

// addressRE matches an email address rather than a bare "@".
//
// MADR 0151 open question 2 chose the strict bare-@ form, and executing this
// phase showed why that is wrong: grok's help text uses @ as file-reference
// syntax ("@src/main.rs", "@!.github/workflows"), which put ten @ on ten
// legitimate lines of the very fixture this guard exists for. A guard that
// fires on those gets weakened or deleted, which is worse than one that is
// narrower on purpose.
var addressRE = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// docDomains are reserved for documentation by RFC 2606, so an address in one
// is a deliberate placeholder rather than a leak. The scrubbed grok fixture
// uses example.com, and a guard that rejected its own placeholder would be
// unusable.
var docDomains = []string{"@example.com", "@example.org", "@example.net"}

// driveAbsolute matches a Windows absolute path in both the raw form and the
// JSON-escaped form that is what actually reaches a fixture (0151 F1). Any
// occurrence means a capture was taken on a Windows host and the home was not
// redacted — which is the exact defect 0151 P1 fixed, seen from the other end.
//
// It is for a line that is not JSON. In a JSON line its optional second
// backslash also matches an escape: "scope:\n" reads as a path starting
// "e:\n" (MADR 0151 F10).
var driveAbsolute = regexp.MustCompile(`[A-Za-z]:\\\\?[A-Za-z]`)

// escapedDrive matches a Windows absolute path inside a JSON document, where a
// backslash can only appear escaped: as `\\`, which every encoder writes, or as
// `\u005c`, which an encoder that escapes more may.
var escapedDrive = regexp.MustCompile(`[A-Za-z]:(\\\\|\\u005[cC])[A-Za-z]`)

// windowsPath returns the start of a Windows absolute path in one fixture line,
// or "" (MADR 0151 D11). A JSON line is searched for the escaped forms only. Any
// other line is searched with driveAbsolute, since Tap records every line an
// agent writes to stdout, JSON or not, and a raw path can reach a fixture there.
func windowsPath(line string) string {
	if json.Valid([]byte(line)) {
		return escapedDrive.FindString(line)
	}
	return driveAbsolute.FindString(line)
}

func TestCommittedFixturesCarryNoIdentifiers(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "provider", "*", "testdata", "wire", "*", "frames.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// A glob that silently stops matching turns this guard into a test that
	// asserts nothing and still passes — the failure mode MADR 0147 D11 pins.
	// Four fixtures exist today (one was removed by MADR 0160); the floor
	// is a floor, not a count, so adding a provider does not break it.
	if len(paths) < 4 {
		t.Fatalf("glob matched %d fixtures, want at least 4: the guard is not reading what it claims to", len(paths))
	}

	// The running host's name, in the forms redact replaces (MADR 0151
	// amendment of 2026-10-28, D10). On CI it is the runner's, which no fixture
	// holds, so this guards the host that captures, not CI. A fixture that
	// names its capture host is checked only on that host: elsewhere the name
	// can occur in public data, as an ordinary word (D12).
	host, _ := os.Hostname()
	local, err := HostTag(false)
	if err != nil {
		t.Fatalf("host tag: %v", err)
	}

	for _, path := range paths {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			hosts := hostNames(host)
			if !hostRuleApplies(t, filepath.Join(filepath.Dir(path), "meta.json"), local) {
				t.Logf("captured on another host: its host-name rule is that host's (MADR 0151 D12)")
				hosts = nil
			}
			for i, line := range strings.Split(string(b), "\n") {
				for _, hit := range addressRE.FindAllString(line, -1) {
					if isDocAddress(hit) {
						continue
					}
					t.Errorf("line %d: email address %q — redaction covers the home directory only (MADR 0151 F8)", i+1, hit)
				}
				if m := windowsPath(line); m != "" {
					t.Errorf("line %d: unredacted Windows path starting %q (MADR 0151 F1)", i+1, m)
				}
				for _, h := range hosts {
					if strings.Contains(strings.ToLower(line), strings.ToLower(h)) {
						t.Errorf("line %d: this host's name, unredacted (MADR 0151 F9)", i+1)
						break
					}
				}
			}
		})
	}
}

// hostRuleApplies reports whether the running host's name is checked in the
// fixture whose metadata is at metaPath: when the fixture was captured on this
// host, by its tag local, or names no capture host (MADR 0151 D12).
func hostRuleApplies(t *testing.T, metaPath, local string) bool {
	t.Helper()
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("fixture metadata: %v", err)
	}
	var meta struct {
		CaptureHost string `json:"capture_host"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("%s: %v", metaPath, err)
	}
	return meta.CaptureHost == "" || meta.CaptureHost == local
}

// TestHostRuleAppliesOnTheCaptureHost pins D12's choice, on metadata written
// for the test.
func TestHostRuleAppliesOnTheCaptureHost(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	none := write("none.json", `{"provider":"x"}`)
	tagged := write("tagged.json", `{"provider":"x","capture_host":"aaaa"}`)
	for _, c := range []struct {
		name, meta, local string
		want              bool
	}{
		{"no capture host, any host", none, "bbbb", true},
		{"no capture host, no key", none, "", true},
		{"captured here", tagged, "aaaa", true},
		{"captured elsewhere", tagged, "bbbb", false},
		{"captured elsewhere, no key here", tagged, "", false},
	} {
		if got := hostRuleApplies(t, c.meta, c.local); got != c.want {
			t.Errorf("%s: applies = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestHostTagIsAKeyNotAName pins D12's tag: no tag and no key file until one is
// asked for, then a stable tag that is not the host's name, from a key only its
// owner can read.
func TestHostTagIsAKeyNotAName(t *testing.T) {
	key := filepath.Join(t.TempDir(), "sub", "wirecap-host-key")
	t.Setenv(EnvHostKey, key)
	if tag, err := HostTag(false); err != nil || tag != "" {
		t.Fatalf("without a key: %q, %v; want no tag", tag, err)
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatalf("asking without create made the key: %v", err)
	}
	tag, err := HostTag(true)
	if err != nil || len(tag) != 64 {
		t.Fatalf("created tag %q, %v", tag, err)
	}
	again, _ := HostTag(false)
	if again != tag {
		t.Errorf("tag changed between calls: %q then %q", tag, again)
	}
	host, _ := os.Hostname()
	for _, h := range hostNames(host) {
		if strings.Contains(tag, strings.ToLower(h)) {
			t.Errorf("the tag carries the host's name")
		}
	}
	if fi, err := os.Stat(key); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("key file %v, %v; want mode 0600", fi, err)
	}
}

// TestCaptureWritesTheTagOnlyWhenAKeyExists: a capture names its host by tag
// when this host has a key, and makes none on its own.
func TestCaptureWritesTheTagOnlyWhenAKeyExists(t *testing.T) {
	key := filepath.Join(t.TempDir(), "wirecap-host-key")
	t.Setenv(EnvHostKey, key)
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	c := For("vendor")
	c.Close()
	if _, err := os.Stat(filepath.Join(dir, "vendor", "capture_host")); !os.IsNotExist(err) {
		t.Errorf("a capture without a key wrote a tag: %v", err)
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Errorf("a capture made the key: %v", err)
	}
	tag, err := HostTag(true)
	if err != nil {
		t.Fatal(err)
	}
	c = For("vendor")
	c.Close()
	got, err := os.ReadFile(filepath.Join(dir, "vendor", "capture_host"))
	if err != nil || strings.TrimSpace(string(got)) != tag {
		t.Errorf("capture_host = %q, %v; want the host's tag", got, err)
	}
}

// TestWindowsPathByEncoding pins D11: a Windows path is found in each encoding
// that can reach a fixture, and a JSON escape after a colon is not one (F10).
func TestWindowsPathByEncoding(t *testing.T) {
	marshal := func(s string) string {
		b, err := json.Marshal(map[string]string{"v": s})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"JSON, a path as json.Marshal writes it", marshal(`C:\Users\alice\proj`), true},
		{"JSON, a path escaped as \\u005c", `{"v":"C:\u005cUsers\u005calice"}`, true},
		{"JSON, a newline and a tab after colons", marshal("In scope:\nOut of scope:\tx"), false},
		{"not JSON, a raw path", `stderr: C:\Users\alice\proj`, true},
		{"not JSON, an escaped path", `partial {"v":"C:\\Users\\alice`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsPath(tc.line); (got != "") != tc.want {
				t.Fatalf("windowsPath(%s) = %q, want a path: %v", tc.line, got, tc.want)
			}
		})
	}
}

func isDocAddress(addr string) bool {
	lower := strings.ToLower(addr)
	for _, d := range docDomains {
		if strings.HasSuffix(lower, d) {
			return true
		}
	}
	return false
}
