// Package wirecap records the raw frames a provider engine sends, so a version
// pin can cite evidence that the wire shape was re-checked at that version
// (MADR 0137 Phase 1).
//
// It exists because the five providers do not share a transport. kilo and
// opencode stream SSE over HTTP and can be captured by an external tool; grok
// speaks ACP over stdio through a third-party SDK that owns its read loop,
// goose speaks ACP over a websocket, and codex speaks JSON-RPC over an
// app-server proxy. Capturing those from outside would mean reimplementing
// each client handshake in a script. Capturing them from inside is one hook per
// transport at the point the bytes arrive.
//
// It is inert unless MCREMOTE_WIRE_CAPTURE_DIR is set, and is intended for a
// developer capturing fixtures — never for production. Frames are written
// verbatim, so a fixture reproduces the engine's own bytes.
package wirecap

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// EnvDir names the directory to write fixtures into. Unset disables capture
// entirely, which is the only state a shipped daemon is ever in.
const EnvDir = "MCREMOTE_WIRE_CAPTURE_DIR"

// Capture appends raw frames for one provider. The zero value and a nil
// *Capture are both safe and do nothing, so call sites need no branch.
type Capture struct {
	mu   sync.Mutex
	f    *os.File
	home string
}

// For returns a capture for provider, or nil when capture is disabled.
//
// Returning nil rather than a no-op object is deliberate: the hot path in each
// transport then costs one nil check, and a reader can see at the call site
// that nothing happens in production.
func For(provider string) *Capture {
	dir := strings.TrimSpace(os.Getenv(EnvDir))
	if dir == "" || strings.TrimSpace(provider) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, provider), 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, provider, "frames.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	return &Capture{f: f, home: home}
}

// Frame records one raw frame. Newlines inside the frame are escaped so the
// file stays one-frame-per-line; everything else is byte-verbatim apart from
// the home-path redaction below.
func (c *Capture) Frame(b []byte) {
	if c == nil || len(b) == 0 {
		return
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return
	}
	s = c.redact(s)
	s = strings.ReplaceAll(s, "\n", "\\n")
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.f.WriteString(s + "\n")
}

// redact rewrites the operator's home directory before it reaches a file bound
// for a public repository. Engines carry the session directory in both the
// absolute form and with the leading separator stripped ("Users/alice"), and
// replacing only the absolute one leaves the username behind — found by
// grepping a fixture that had already been "redacted" (MADR 0137 Phase 1).
func (c *Capture) redact(s string) string {
	if c.home == "" || c.home == "/" {
		return s
	}
	s = strings.ReplaceAll(s, c.home, "/home/user")
	if rel := strings.TrimPrefix(c.home, string(os.PathSeparator)); rel != "" && rel != c.home {
		s = strings.ReplaceAll(s, rel, "home/user")
	}
	return s
}

// Close releases the file. Safe on nil.
func (c *Capture) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.f.Close()
}

// TeeReader returns r wrapped so every line read is recorded. Used where a
// third-party SDK owns the read loop and the only seam is the io.Reader handed
// to it — grok's ACP stdio connection.
func (c *Capture) TeeReader(r interface{ Read([]byte) (int, error) }) interface {
	Read([]byte) (int, error)
} {
	if c == nil {
		return r
	}
	return &teeReader{src: r, cap: c}
}

type teeReader struct {
	src interface{ Read([]byte) (int, error) }
	cap *Capture
	buf []byte
}

func (t *teeReader) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 {
		t.buf = append(t.buf, p[:n]...)
		for {
			i := strings.IndexByte(string(t.buf), '\n')
			if i < 0 {
				break
			}
			t.cap.Frame(t.buf[:i])
			t.buf = t.buf[i+1:]
		}
	}
	return n, err
}
