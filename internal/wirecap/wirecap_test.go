package wirecap

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	r := bytes.NewReader([]byte("line\n"))
	tr := c.TeeReader(r)
	if tr != r {
		t.Fatal("nil Capture.TeeReader should return the original reader")
	}
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

func TestTeeReaderEmitsOnNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c := &Capture{f: f}
	defer c.Close()

	r := &chunkReader{chunks: [][]byte{
		[]byte("hel"),
		[]byte("lo\n"),
		[]byte("next\n"),
	}}
	tr := c.TeeReader(r)
	buf := make([]byte, 8)
	for {
		_, err := tr.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "next" {
		t.Fatalf("frames = %#v, want [hello next]", lines)
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

type chunkReader struct {
	chunks [][]byte
	i      int
}

func (s *chunkReader) Read(p []byte) (int, error) {
	if s.i >= len(s.chunks) {
		return 0, io.EOF
	}
	n := copy(p, s.chunks[s.i])
	s.i++
	return n, nil
}
