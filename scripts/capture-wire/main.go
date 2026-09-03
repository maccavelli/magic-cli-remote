// Command capture-wire records a provider engine's raw event frames to a
// fixture directory, so a version pin can cite evidence that the wire shape
// was re-checked at that version (MADR 0137 Phase 1).
//
// A pin bump with no fixture is a claim that nothing changed. kilo 7.4.23 ->
// 7.5.6 is the counter-example that motivated this tool: the daemon warned
// that wire shapes were probed on the pinned version and proceeded anyway.
//
// Usage:
//
//	capture-wire -kind sse -url http://127.0.0.1:PORT/global/event \
//	  -provider kilo -version 7.5.6 -out internal/provider/kilo/testdata/wire
//
// The command exits non-zero when it captures nothing, so a fixture is never
// silently empty.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// meta is written beside frames.jsonl so a fixture always names the binary
// version it came from. A fixture without one cannot justify a pin.
type meta struct {
	Provider   string `json:"provider"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	CapturedAt string `json:"captured_at"`
	Frames     int    `json:"frames"`
	// Redacted names the substitution applied to the captured bytes. Fixtures
	// are committed to a public repository, so the operator's home path is
	// rewritten; everything else is verbatim. Empty means no substitution.
	Redacted string `json:"redacted,omitempty"`
}

// redactHome rewrites the operator's home directory to a stable placeholder.
// Engine frames carry the session directory and project paths, which would
// otherwise commit the operator's username to a public repository. Applied at
// capture time so a fixture cannot be created unredacted and then forgotten.
func redactHome(frames []string) ([]string, string) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || home == "/" {
		return frames, ""
	}
	const placeholder = "/home/user"
	// Engines emit the home path in more than one form. kilo and opencode both
	// carry a `path` field with the leading separator stripped
	// ("Users/alice"), which a naive replacement of the absolute path misses —
	// found by grepping a fixture that had already been "redacted".
	// Longest-first so the absolute form is replaced before the relative one.
	subs := []struct{ from, to string }{
		{home, placeholder},
		{strings.TrimPrefix(home, string(os.PathSeparator)), strings.TrimPrefix(placeholder, "/")},
	}
	hit := false
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		for _, s := range subs {
			if s.from == "" || !strings.Contains(f, s.from) {
				continue
			}
			hit = true
			f = strings.ReplaceAll(f, s.from, s.to)
		}
		out = append(out, f)
	}
	if !hit {
		return out, ""
	}
	return out, home + " -> " + placeholder + " (absolute and separator-stripped forms)"
}

func main() {
	kind := flag.String("kind", "sse", "transport: sse")
	url := flag.String("url", "", "sse: event stream URL")
	provider := flag.String("provider", "", "provider id (kilo, opencode, …)")
	version := flag.String("version", "", "installed binary version, e.g. 7.5.6")
	out := flag.String("out", "", "fixture directory root")
	dur := flag.Duration("duration", 25*time.Second, "how long to record")
	minFrames := flag.Int("min-frames", 1, "fail unless at least this many frames are captured")
	flag.Parse()

	if err := run(*kind, *url, *provider, *version, *out, *dur, *minFrames); err != nil {
		fmt.Fprintf(os.Stderr, "capture-wire: %v\n", err)
		os.Exit(1)
	}
}

func run(kind, url, provider, version, out string, dur time.Duration, minFrames int) error {
	for name, v := range map[string]string{
		"-provider": provider, "-version": version, "-out": out,
	} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if kind != "sse" {
		return fmt.Errorf("unsupported -kind %q (only sse today; see MADR 0137 Phase 1 deviation)", kind)
	}
	if strings.TrimSpace(url) == "" {
		return errors.New("-url is required for -kind sse")
	}

	// Capture into memory first. Creating the fixture file up front left an
	// empty frames.jsonl behind whenever the capture failed, which is exactly
	// the "silently empty fixture" this tool exists to prevent — found by the
	// tool's own failure test (MADR 0137 Phase 1 step 1.4).
	frames, err := captureSSE(url, dur)
	if err != nil && len(frames) == 0 {
		return err
	}
	if len(frames) < minFrames {
		return fmt.Errorf("captured %d frames, want at least %d — refusing to write an empty fixture",
			len(frames), minFrames)
	}

	frames, redacted := redactHome(frames)

	dir := filepath.Join(out, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	framesPath := filepath.Join(dir, "frames.jsonl")
	if err := os.WriteFile(framesPath, []byte(strings.Join(frames, "\n")+"\n"), 0o644); err != nil { //nolint:gosec // fixture
		return err
	}
	n := len(frames)

	m := meta{
		Provider: provider, Version: version, Kind: kind, Source: url,
		CapturedAt: time.Now().UTC().Format(time.RFC3339), Frames: n,
		Redacted: redacted,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), append(b, '\n'), 0o644); err != nil { //nolint:gosec // fixture
		return err
	}
	fmt.Printf("captured %d frames -> %s\n", n, framesPath)
	return nil
}

// captureSSE returns every `data:` payload verbatim, one JSON object per entry.
// Payloads are kept as-is rather than re-encoded, so a fixture reproduces the
// engine's own bytes and not this tool's idea of them.
func captureSSE(url string, dur time.Duration) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: dur + 10*time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sse status %d from %s", res.StatusCode, url)
	}

	deadline := time.Now().Add(dur)
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var frames []string
	for sc.Scan() {
		if time.Now().After(deadline) {
			break
		}
		line := strings.TrimSpace(sc.Text())
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" {
			continue
		}
		frames = append(frames, payload)
	}
	return frames, sc.Err()
}
