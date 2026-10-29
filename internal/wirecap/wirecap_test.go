package wirecap

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestForNilWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvDir, "")
	if c := For("codex"); c != nil {
		t.Fatalf("For = %#v, want nil when %s is unset", c, EnvDir)
	}
}

func TestForNilWhenProviderBlank(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	if c := For("  "); c != nil {
		t.Fatalf("For = %#v, want nil when provider is blank", c)
	}
	if c := For(""); c != nil {
		t.Fatalf("For = %#v, want nil when provider is empty", c)
	}
}

func TestForWritesFramesWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	c := For("codex")
	if c == nil {
		t.Fatal("For returned nil with capture enabled")
	}
	defer c.Close()
	c.Frame([]byte(`{"ok":true}`))
	got, err := os.ReadFile(filepath.Join(dir, "codex", "frames.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"ok\":true}\n"; string(got) != want {
		t.Fatalf("frames = %q, want %q", got, want)
	}
}

func TestNilCaptureMethodsNoop(t *testing.T) {
	var c *Capture
	c.Frame([]byte("x"))
	c.Close()
}

func TestFrameEscapesEmbeddedNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c := &Capture{f: f, home: "/does/not/appear"}
	defer c.Close()
	c.Frame([]byte("a\nb\nc"))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\\nb\\nc\n"; string(got) != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
	if bytes.Count(got, []byte{'\n'}) != 1 {
		t.Fatalf("want exactly one record newline, got %q", got)
	}
}

// TestRedactAbsoluteAndRelativeHome pins redaction for every home shape a host
// can hand us, on every host.
//
// The earlier version of this test passed a POSIX home unconditionally and
// failed on Go (windows/amd64) (run 34011907466), because redact derived its
// stripped form from os.PathSeparator. MADR 0144's amendment records the
// analysis; the fix made redact depend only on its inputs, so this table needs
// no runtime.GOOS branch and no t.Skip. If a case here ever has to be gated by
// platform, redact has regressed to consulting the host again.
func TestRedactAbsoluteAndRelativeHome(t *testing.T) {
	const user = "alice"

	cases := []struct {
		name string
		home string
		in   string
		want string
	}{
		{
			name: "posix home, absolute and stripped forms",
			home: "/Users/alice",
			in:   "cwd=/Users/alice/proj and also Users/alice/proj",
			want: "cwd=/home/user/proj and also home/user/proj",
		},
		{
			name: "windows home, absolute and drive-stripped forms",
			home: `C:\Users\alice`,
			in:   `cwd=C:\Users\alice\proj and also Users\alice\proj`,
			want: `cwd=/home/user\proj and also home/user\proj`,
		},
		{
			name: "unc home, absolute and share-stripped forms",
			home: `\\srv\home\alice`,
			in:   `cwd=\\srv\home\alice\p and also srv\home\alice\p`,
			want: `cwd=/home/user\p and also home/user\p`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (&Capture{home: tc.home}).redact(tc.in)
			if strings.Contains(got, user) {
				t.Errorf("username leaked after redact: %q", got)
			}
			if got != tc.want {
				t.Errorf("redact = %q, want %q", got, tc.want)
			}
		})
	}

	// The forms above are what a home looks like in isolation. What reaches
	// frames.jsonl is a JSON document, and encoding/json escapes a backslash, so
	// a Windows home arrives doubled and matches neither needle above.
	// Redaction was a complete no-op on Windows until MADR 0151 D1; its F3
	// records that every capture site hands Frame a JSON document, so this is
	// the only form that reaches the file, not an edge case.
	//
	// The inputs come from json.Marshal rather than from hand-written literals
	// on purpose: a literal could drift from what the encoder actually
	// produces, and the gap between the two is this test's whole subject.
	t.Run("json-escaped homes", func(t *testing.T) {
		for _, home := range []string{"/Users/alice", `C:\Users\alice`, `\srv\home\alice`} {
			frame, err := json.Marshal(map[string]string{"cwd": home + `\proj`, "alt": home + "/proj"})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := (&Capture{home: home}).redact(string(frame))
			if strings.Contains(got, user) {
				t.Errorf("home %q: username leaked after redact: %s", home, got)
			}
		}
	})

	// Disabled states stay byte-for-byte inert: redaction that fires when it was
	// not configured would corrupt a fixture rather than protect one.
	t.Run("empty home is inert", func(t *testing.T) {
		if g := (&Capture{home: ""}).redact("/Users/alice"); g != "/Users/alice" {
			t.Fatalf("empty home should not redact, got %q", g)
		}
	})
	t.Run("root home is inert", func(t *testing.T) {
		if g := (&Capture{home: "/"}).redact("/tmp/x"); g != "/tmp/x" {
			t.Fatalf("root home should not redact, got %q", g)
		}
	})
}

// TestRedactHostName is MADR 0151 amendment of 2026-10-28, D8: the capturing
// host's name, which grok sends as _meta.hostname and codex as serverName, is
// replaced with <host> in every form an engine may carry it: the full name, the
// short one before the first dot, and either lowercased. The longest is replaced
// first, so a full name never leaves its domain behind.
func TestRedactHostName(t *testing.T) {
	c := &Capture{hosts: hostNames("Build-Box-7.corp.example")}
	frame, err := json.Marshal(map[string]string{
		"hostname": "Build-Box-7", "fqdn": "Build-Box-7.corp.example", "lower": "build-box-7.corp.example",
		"serverName": "build-box-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := c.redact(string(frame))
	if strings.Contains(strings.ToLower(got), "build-box-7") || strings.Contains(got, "corp.example") {
		t.Fatalf("the host's name leaked after redact: %s", got)
	}
	if n := strings.Count(got, "<host>"); n != 4 {
		t.Fatalf("redact = %s, want <host> 4 times", got)
	}

	// A name that short would be replaced inside unrelated words, so it is not.
	if names := hostNames("ab"); len(names) != 0 {
		t.Fatalf("hostNames(\"ab\") = %q, want none", names)
	}
	if g := (&Capture{hosts: hostNames("ab")}).redact(`{"x":"abc"}`); g != `{"x":"abc"}` {
		t.Fatalf("a short host name changed a frame: %q", g)
	}
	// No name configured, no change.
	if g := (&Capture{}).redact(`{"hostname":"Build-Box-7"}`); g != `{"hostname":"Build-Box-7"}` {
		t.Fatalf("an unconfigured capture changed a frame: %q", g)
	}
}

// TestForRecordsTheHostName: a capture redacts the host it runs on, without
// being told (D8).
func TestForRecordsTheHostName(t *testing.T) {
	t.Setenv(EnvDir, t.TempDir())
	c := For("p")
	if c == nil {
		t.Fatal("For returned nil with capture enabled")
	}
	defer c.Close()
	host, _ := os.Hostname()
	if want := hostNames(host); !slices.Equal(c.hosts, want) {
		t.Fatalf("hosts = %d name(s), want hostNames of this host, %d", len(c.hosts), len(want))
	}
}

// TestTapOfNilCaptureIsNilWriter: a disabled capture must hand agentproc no
// tap at all. A typed nil in the io.Writer would be a set tap (MADR 0207 G1).
func TestTapOfNilCaptureIsNilWriter(t *testing.T) {
	var c *Capture
	if w := c.Tap(); w != nil {
		t.Fatalf("nil Capture.Tap() = %#v, want a nil io.Writer", w)
	}
}

// testCapture is a capture writing to a file in a temp dir, and that file.
func testCapture(t *testing.T) (*Capture, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frames.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c := &Capture{f: f}
	t.Cleanup(c.Close)
	return c, path
}

// TestTapFramesLinesAcrossWrites: bytes written to Tap in uneven chunks are
// recorded one frame per line, as the reader wrapper it replaced recorded them
// (MADR 0207 D10). That includes a line split across writes, and a last line
// with no newline, which is held.
func TestTapFramesLinesAcrossWrites(t *testing.T) {
	chunks := [][]byte{
		[]byte("hel"),
		[]byte("lo\nne"),
		[]byte("xt\n{\"a\":1}\n"),
		[]byte("tail without newline"),
	}

	tap, tapPath := testCapture(t)
	w := tap.Tap()
	for _, c := range chunks {
		if n, err := w.Write(c); n != len(c) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", c, n, err, len(c))
		}
	}

	got, err := os.ReadFile(tapPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := "hello\nnext\n{\"a\":1}\n"; string(got) != want {
		t.Errorf("frames = %q, want %q", got, want)
	}
}

// TestTapDoesNotRetainTheCallersSlice: agentproc reuses the slice it passes a
// tap, which "must not retain p". A held partial line must survive the caller
// overwriting it.
func TestTapDoesNotRetainTheCallersSlice(t *testing.T) {
	c, path := testCapture(t)
	w := c.Tap()
	p := []byte("abc")
	if _, err := w.Write(p); err != nil {
		t.Fatal(err)
	}
	copy(p, "xyz")
	if _, err := w.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "abc\n"; string(got) != want {
		t.Fatalf("frames = %q, want %q", got, want)
	}
}

func TestFrameNoopOnWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c := &Capture{f: f}
	defer c.Close()
	c.Frame(nil)
	c.Frame([]byte("   \n\t"))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("whitespace-only frames should not write, got %q", got)
	}
}
