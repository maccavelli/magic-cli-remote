// Package stderrtail keeps the end of an agent engine's standard error, so
// that a failure to start and a death while running can say what the engine
// last wrote (MADR 0216 D22).
//
// Every transport uses it the same way: acpagent for grok and gobble, codex's
// app-server connection, and httpagent for kilo and opencode. Each sets a
// Writer as its command's Stderr, in front of the writer that logs and scans
// the lines, and after the process is reaped reads Tail, removes the secrets
// it knows with Scrub, and reports the result through Wrap or Message.
//
// What Scrub removes is what Magic holds: values from the engine's own
// environment and configuration (D27). An engine can print a secret Magic
// never held, and nothing here can find it.
package stderrtail

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// DefaultBytes bounds what a Writer holds, and so the tail a message
	// carries (MADR 0216 D1, D24).
	DefaultBytes = 4096
	// DefaultLines is a backstop behind DefaultBytes, not the bound that
	// governs: ten lines dropped a Go panic's message at a stack depth of three,
	// where its whole text was 283 bytes (D24, A-5).
	DefaultLines = 64
	// Separator comes between a message and its tail (D2, D28).
	Separator = "\nlast stderr:\n"
	// Redacted replaces a secret Scrub removes (D4).
	Redacted = "<redacted>"

	// minSecret is the shortest value Scrub replaces. A shorter one would be
	// replaced inside other words, and is not a credential's shape (D4).
	minSecret = 8
)

// credentialSuffixes are the endings of an environment variable's name, in
// upper case, whose value Secrets treats as a credential (D27). They cover
// grok's XAI_API_KEY, opencode's six variables and gobble's per-provider ones
// without a list Magic would have to keep up.
var credentialSuffixes = []string{"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD"}

// Writer keeps the end of what is written to it: at most DefaultBytes bytes and
// DefaultLines lines, starting at a line's beginning, unless the last line
// alone is longer than the byte cap, in which case it keeps that line's end
// from a rune's beginning. It forwards every byte to next, so the writer that
// logs and scans the lines sees all of them. It is safe for concurrent use.
//
// Its rules are those of acp-go-sdk2's agentproc tail, which only a process
// started through agentproc gets; codex and httpagent start theirs with
// os/exec, so every transport uses this one instead (D23).
type Writer struct {
	next  io.Writer
	bytes int
	lines int

	mu  sync.Mutex
	buf []byte
}

// New returns a Writer with the default caps that forwards to next. A nil next
// discards, as a nil Cmd.Stderr does.
func New(next io.Writer) *Writer {
	return &Writer{next: next, bytes: DefaultBytes, lines: DefaultLines}
}

// Write keeps the end of p in the tail and forwards p to next, returning next's
// result, so that os/exec sees next as it would without the tail.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	// One byte more than the cap, so trim can see whether its cut falls
	// mid-line.
	keep := p
	if len(keep) > w.bytes+1 {
		keep = keep[len(keep)-w.bytes-1:]
	}
	w.buf = append(w.buf, keep...)
	w.trim()
	w.mu.Unlock()
	if w.next == nil {
		return len(p), nil
	}
	return w.next.Write(p)
}

// trim applies the caps to buf, under mu.
func (w *Writer) trim() {
	count := bytes.Count(w.buf, []byte{'\n'})
	if len(w.buf) > 0 && w.buf[len(w.buf)-1] != '\n' {
		count++ // an unended last line
	}
	for ; count > w.lines; count-- {
		w.buf = w.buf[bytes.IndexByte(w.buf, '\n')+1:]
	}
	if len(w.buf) > w.bytes {
		cut := len(w.buf) - w.bytes
		midLine := w.buf[cut-1] != '\n'
		w.buf = w.buf[cut:]
		// A cut that fell on a line's beginning needs nothing more.
		if midLine {
			if i := bytes.IndexByte(w.buf, '\n'); i >= 0 && i < len(w.buf)-1 {
				w.buf = w.buf[i+1:] // from the next line's beginning
			} else {
				// The last line is all there is: keep its end, from a whole rune.
				for len(w.buf) > 0 && !utf8.RuneStart(w.buf[0]) {
					w.buf = w.buf[1:]
				}
			}
		}
	}
	if cap(w.buf) > 2*max(len(w.buf), 4096) {
		w.buf = bytes.Clone(w.buf) // let go of what was cut
	}
}

// Tail returns what is kept, its final line ending trimmed. It is final once
// the process is reaped: os/exec's copy into the Writer has returned by then.
func (w *Writer) Tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimRight(string(w.buf), "\r\n")
}

// Secrets returns the values Scrub removes, each of at least minSecret bytes,
// once, longest first so that no value is left half replaced inside a longer
// one (D27):
//
//   - the value of each NAME=value in env whose name ends in _API_KEY, _TOKEN,
//     _SECRET or _PASSWORD, in any case;
//   - the value of each variable in env that names lists, in any case;
//   - each of values, such as configured MCP header values.
//
// env is the environment the engine was started with: its command's Env, or
// os.Environ() when that is nil.
func Secrets(env, names, values []string) []string {
	var out []string
	add := func(v string) {
		if len(v) >= minSecret && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		if credentialName(name) || slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) }) {
			add(value)
		}
	}
	for _, v := range values {
		add(v)
	}
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return out
}

// credentialName reports whether an environment variable's name ends in one of
// credentialSuffixes.
func credentialName(name string) bool {
	upper := strings.ToUpper(name)
	return slices.ContainsFunc(credentialSuffixes, func(s string) bool { return strings.HasSuffix(upper, s) })
}

// Scrub replaces each of secrets in tail with Redacted, as written and as JSON
// encodes it, since an engine may log a value inside a JSON document, with its
// backslashes and quotes escaped (D4; the reason wirecap replaces both forms,
// MADR 0151 D1). A value shorter than minSecret is left alone.
func Scrub(tail string, secrets []string) string {
	for _, s := range secrets {
		if len(s) < minSecret {
			continue
		}
		tail = strings.ReplaceAll(tail, s, Redacted)
		if esc := jsonForm(s); esc != s {
			tail = strings.ReplaceAll(tail, esc, Redacted)
		}
	}
	return tail
}

// jsonForm is s as a JSON string's contents, without HTML escaping, which
// engines do not apply.
func jsonForm(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return s // a string always encodes
	}
	out := strings.TrimSuffix(b.String(), "\n")
	return out[1 : len(out)-1]
}

// Error is an error that carries the end of its engine's standard error. Its
// text is Err's, then Separator, then Tail. errors.As finds it, so a layer
// that clips error text for the phone can clip Err's part and keep the tail
// (D3, D28).
type Error struct {
	Err  error
	Tail string
}

// Error returns Err's text, then Separator, then the tail.
func (e *Error) Error() string { return e.Err.Error() + Separator + e.Tail }

// Unwrap returns Err.
func (e *Error) Unwrap() error { return e.Err }

// Wrap returns err with tail joined as an *Error, or err itself when tail is
// empty or err is nil.
func Wrap(err error, tail string) error {
	if err == nil || tail == "" {
		return err
	}
	return &Error{Err: err, Tail: tail}
}

// Message returns headline, then Separator and tail when tail is not empty,
// for a death reported as event text rather than as an error value (D2, D26).
func Message(headline, tail string) string {
	if tail == "" {
		return headline
	}
	return headline + Separator + tail
}
