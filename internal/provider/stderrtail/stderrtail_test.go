package stderrtail

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// numbered returns n lines, each "line <i> " padded with x to width bytes and a
// newline, so a tail's first line says where it starts.
func numbered(n, width int) string {
	var b strings.Builder
	for i := range n {
		l := fmt.Sprintf("line %06d ", i)
		b.WriteString(l + strings.Repeat("x", width-len(l)-1) + "\n")
	}
	return b.String()
}

// TestWriterCaps is MADR 0216 D24: the byte cap governs and the line cap backs
// it up, and the tail starts at a line's beginning whichever applies.
func TestWriterCaps(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		chunk     int    // bytes per Write; 0 writes all at once
		wantFirst string // the tail's first line starts so
		wantLines int    // 0 skips the count
	}{
		{"70 short lines keep the last 64", numbered(70, 20), 0, "line 000006 ", 64},
		{"1 MiB of 40-byte lines keeps the last 64", numbered(1<<20/40, 40), 4096, fmt.Sprintf("line %06d ", 1<<20/40-64), 64},
		{"100-byte lines are cut by bytes, from a line's beginning", numbered(100, 100), 0, "line 000060 ", 40},
		{"a short tail is kept whole", "one\ntwo\n", 1, "one", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := New(nil)
			in := []byte(tc.in)
			for len(in) > 0 {
				n := len(in)
				if tc.chunk > 0 {
					n = min(n, tc.chunk)
				}
				if _, err := w.Write(in[:n]); err != nil {
					t.Fatal(err)
				}
				in = in[n:]
			}
			tail := w.Tail()
			if len(tail) > DefaultBytes {
				t.Fatalf("tail is %d bytes, over the cap of %d", len(tail), DefaultBytes)
			}
			if !strings.HasPrefix(tail, tc.wantFirst) {
				t.Fatalf("tail starts %.40q, want %q", tail, tc.wantFirst)
			}
			if got := strings.Count(tail, "\n") + 1; tc.wantLines > 0 && got != tc.wantLines {
				t.Fatalf("tail has %d lines, want %d", got, tc.wantLines)
			}
		})
	}
}

// TestWriterKeepsTheEndOfALongLine: a last line longer than the byte cap keeps
// its end, from a whole rune, so the tail is valid UTF-8.
func TestWriterKeepsTheEndOfALongLine(t *testing.T) {
	w := New(nil)
	// 64 KiB of two-byte runes after an odd prefix, so a byte cut lands mid-rune.
	line := "x" + strings.Repeat("é", 32<<10) + "END"
	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	tail := w.Tail()
	if len(tail) > DefaultBytes || len(tail) < DefaultBytes-1 {
		t.Fatalf("tail is %d bytes, want the line's last %d (or one less, at a rune's start)", len(tail), DefaultBytes)
	}
	if !strings.HasSuffix(tail, "END") || !utf8.ValidString(tail) {
		t.Fatalf("tail does not end the line, or is not valid UTF-8: ...%q", tail[len(tail)-8:])
	}
}

// TestWriterJoinsALineAcrossWrites: a line written in pieces is one line.
func TestWriterJoinsALineAcrossWrites(t *testing.T) {
	w := New(nil)
	for _, p := range []string{"pan", "ic: boo", "m\nnext"} {
		if _, err := w.Write([]byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	if got := w.Tail(); got != "panic: boom\nnext" {
		t.Fatalf("tail = %q", got)
	}
}

// TestWriterForwardsEveryByte: the writer behind it, which logs and scans the
// lines, sees every byte in order, cap or no cap (D1).
func TestWriterForwardsEveryByte(t *testing.T) {
	var next bytes.Buffer
	w := New(&next)
	in := numbered(500, 50)
	for i := 0; i < len(in); i += 333 {
		if _, err := w.Write([]byte(in[i:min(i+333, len(in))])); err != nil {
			t.Fatal(err)
		}
	}
	if next.String() != in {
		t.Fatalf("next got %d bytes, want all %d in order", next.Len(), len(in))
	}
}

// errWriter fails every write.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("next failed") }

// TestWriterReturnsNextsResult: os/exec sees the writer behind it as it would
// without the tail; with none, the bytes are discarded.
func TestWriterReturnsNextsResult(t *testing.T) {
	if n, err := New(errWriter{}).Write([]byte("x\n")); n != 0 || err == nil {
		t.Fatalf("Write = %d, %v; want next's 0 and its error", n, err)
	}
	if n, err := New(nil).Write([]byte("x\n")); n != 2 || err != nil {
		t.Fatalf("Write with no next = %d, %v; want 2, nil", n, err)
	}
}

// TestWriterIsSafeForConcurrentUse is run under -race by make race.
func TestWriterIsSafeForConcurrentUse(t *testing.T) {
	w := New(nil)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				_, _ = fmt.Fprintf(w, "goroutine %d line %d %s\n", g, i, strings.Repeat("y", 30))
				_ = w.Tail()
			}
		})
	}
	wg.Wait()
	if n := len(w.Tail()); n > DefaultBytes {
		t.Fatalf("tail is %d bytes after concurrent writes, over %d", n, DefaultBytes)
	}
}

// TestSecrets is D27: credential-shaped names, named extras and given values,
// each at least minSecret bytes, once, longest first.
func TestSecrets(t *testing.T) {
	env := []string{
		"PATH=/usr/bin:/bin:/usr/local/bin",
		"XAI_API_KEY=xai-0123456789",
		"gh_token=ghp_abcdefgh",
		"SHORT_TOKEN=1234567", // 7 bytes: under the floor
		"DB_PASSWORD=hunter2hunter2",
		"MY_ODD_CRED=odd-credential",
		"=C:=C:\\work", // Windows' per-drive entry: no name
		"NOEQUALS",
		"DUP_SECRET=xai-0123456789",
	}
	got := Secrets(env, []string{"my_odd_cred"}, []string{"Bearer header-value", "tiny"})
	// Longest first; values of one length keep the order they were found in.
	want := []string{"Bearer header-value", "xai-0123456789", "hunter2hunter2", "odd-credential", "ghp_abcdefgh"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Secrets = %q\n want %q", got, want)
	}
}

// TestScrub is D4: a value is replaced as written and as JSON encodes it, and a
// value under the floor is left.
func TestScrub(t *testing.T) {
	secret := `k3y"with\slash`
	enc, err := json.Marshal(map[string]string{"key": secret})
	if err != nil {
		t.Fatal(err)
	}
	tail := "raw " + secret + "\njson " + string(enc) + "\nshort abc1234"
	got := Scrub(tail, []string{secret, "abc1234"})
	if strings.Contains(got, `k3y`) {
		t.Fatalf("a form of the secret is left: %q", got)
	}
	if strings.Count(got, Redacted) != 2 {
		t.Fatalf("want 2 replacements, raw and JSON: %q", got)
	}
	if !strings.Contains(got, "short abc1234") {
		t.Fatalf("a 7-byte value was replaced: %q", got)
	}
}

// TestErrorKeepsItsChain is D28: the tail joins the error's text once, after
// Separator, and errors.Is and errors.As still see the error it wraps.
func TestErrorKeepsItsChain(t *testing.T) {
	base := errors.New("acp initialize: connection closed")
	err := fmt.Errorf("start grok: %w", Wrap(base, "Error: not logged in"))
	if !errors.Is(err, base) {
		t.Fatal("errors.Is lost the wrapped error")
	}
	var te *Error
	if !errors.As(err, &te) || te.Tail != "Error: not logged in" {
		t.Fatalf("errors.As found %#v", te)
	}
	if want := "start grok: acp initialize: connection closed" + Separator + "Error: not logged in"; err.Error() != want {
		t.Fatalf("text = %q\n want %q", err.Error(), want)
	}
	if Wrap(base, "") != base || Wrap(nil, "tail") != nil {
		t.Fatal("Wrap with no tail, or of a nil error, must return the error itself")
	}
}

// TestMessage is D2 and D26: a headline is unchanged without a tail.
func TestMessage(t *testing.T) {
	if got := Message("grok process exited: exit status 1", ""); got != "grok process exited: exit status 1" {
		t.Fatalf("Message with no tail = %q", got)
	}
	if got := Message("h", "t"); got != "h"+Separator+"t" {
		t.Fatalf("Message = %q", got)
	}
}
