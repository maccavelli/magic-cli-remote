package wirecap

import (
	"bytes"
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

func TestRedactAbsoluteAndRelativeHome(t *testing.T) {
	c := &Capture{home: "/Users/alice"}
	in := "cwd=/Users/alice/proj and also Users/alice/proj"
	got := c.redact(in)
	if strings.Contains(got, "alice") {
		t.Fatalf("username leaked after redact: %q", got)
	}
	if !strings.Contains(got, "/home/user/proj") {
		t.Fatalf("absolute home not rewritten: %q", got)
	}
	if !strings.Contains(got, "home/user/proj") {
		t.Fatalf("relative home not rewritten: %q", got)
	}

	if g := (&Capture{home: ""}).redact("/Users/alice"); g != "/Users/alice" {
		t.Fatalf("empty home should not redact, got %q", g)
	}
	if g := (&Capture{home: "/"}).redact("/tmp/x"); g != "/tmp/x" {
		t.Fatalf("root home should not redact, got %q", g)
	}
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
