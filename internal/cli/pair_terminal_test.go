package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/go-tui-lib/v2/termcap"
	"github.com/maccavelli/go-tui-lib/v2/termsvc"
)

// hostileNames are MADR 0219 F6's names, each with what the terminal must be
// shown in its place.
var hostileNames = []struct{ in, want string }{
	{"phone\u202eenohp", "phoneenohp"},
	{"ab\u2066cd\u2069ef", "abcdef"},
	{"pixel\u200b\u200dname", "pixel\u200dname"},
	{"red\x1b[31mtext\x1b[0m", "redtext"},
	{"c1\u009b31mtext", "c1text"},
}

// terminalResidue is what must not reach the operator's terminal from a
// device name: an escape sequence or a piece of one, and the bidi and
// invisible controls of hostileNames.
var terminalResidue = []string{
	"\x1b", "\u009b", "\u202e", "\u2066", "\u2069", "\u200b", "[31m", "[0m", "31mtext",
}

// writePairConfig points the pair commands at an isolated config and data
// directory with TLS off, and returns the data directory.
func writePairConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "data_dir: " + dataDir + "\ntls:\n  mode: off\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", cfgPath)
	t.Setenv("MCREMOTE_PAIR_HOST", "100.64.0.1:7531")
	return dataDir
}

// runPair runs newPairCmd with args and returns its output and error.
func runPair(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newPairCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func assertNoTerminalResidue(t *testing.T, what, out string) {
	t.Helper()
	for _, r := range terminalResidue {
		if strings.Contains(out, r) {
			t.Errorf("%s printed %+q of a device name:\n%+q", what, r, out)
		}
	}
}

// Names stored before the sanitiser existed are printed as stored, so every
// print of a name cleans it too (MADR 0219 D3). devices.json is written
// directly, as such a record would be.
func TestPairPrintsCleanNames(t *testing.T) {
	dataDir := writePairConfig(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	type record struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		TokenHash string    `json:"token_hash"`
		CreatedAt time.Time `json:"created_at"`
	}
	var file struct {
		Devices []record `json:"devices"`
	}
	ids := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
		"00000000-0000-4000-8000-000000000005",
	}
	for i, h := range hostileNames {
		file.Devices = append(file.Devices, record{
			ID: ids[i], Name: h.in, TokenHash: strings.Repeat("0", 63) + string(rune('1'+i)),
			CreatedAt: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
		})
	}
	b, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "devices.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runPair(t, "list")
	if err != nil {
		t.Fatalf("pair list: %v\n%s", err, out)
	}
	assertNoTerminalResidue(t, "pair list", out)
	for _, h := range hostileNames {
		if !strings.Contains(out, h.want) {
			t.Errorf("pair list is missing %+q:\n%+q", h.want, out)
		}
	}

	out, err = runPair(t, "revoke", ids[0])
	if err != nil {
		t.Fatalf("pair revoke: %v\n%s", err, out)
	}
	assertNoTerminalResidue(t, "pair revoke", out)
	if !strings.Contains(out, "Revoked device "+hostileNames[0].want+" ") {
		t.Errorf("pair revoke did not print %+q:\n%+q", hostileNames[0].want, out)
	}

	out, err = runPair(t, "prune", "--keyless")
	if err != nil {
		t.Fatalf("pair prune: %v\n%s", err, out)
	}
	assertNoTerminalResidue(t, "pair prune", out)
	for _, h := range hostileNames[1:] {
		if !strings.Contains(out, "Pruned "+h.want+" ") {
			t.Errorf("pair prune did not print %+q:\n%+q", h.want, out)
		}
	}

	// A pair code keeps the name it was given until a phone claims it, and
	// pair code prints that name.
	hostile := hostileNames[3]
	out, err = runPair(t, "code", "--qr=false", "--name", hostile.in)
	if err != nil {
		t.Fatalf("pair code: %v\n%s", err, out)
	}
	assertNoTerminalResidue(t, "pair code", out)
	if !strings.Contains(out, "Device name: "+hostile.want+"\n") {
		t.Errorf("pair code did not print %+q:\n%+q", hostile.want, out)
	}

	out, err = runPair(t, "create", "--qr=false", "--name", hostile.in)
	if err != nil {
		t.Fatalf("pair create: %v\n%s", err, out)
	}
	assertNoTerminalResidue(t, "pair create", out)
	if !strings.Contains(out, "Device: "+hostile.want+" ") {
		t.Errorf("pair create did not print %+q:\n%+q", hostile.want, out)
	}
}

// An OSC 52 sequence in a pipe or a file is noise there, and would put a live
// pair code in a log, so --copy needs a terminal and mints nothing without
// one (MADR 0219 D4).
func TestPairCodeCopyRefusesNonTerminal(t *testing.T) {
	dataDir := writePairConfig(t)
	out, err := runPair(t, "code", "--qr=false", "--copy")
	if err == nil {
		t.Fatalf("pair code --copy to a buffer should fail, got success:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--copy") || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("error = %v, want one naming --copy and the terminal", err)
	}
	if strings.Contains(out, "Pair code:") {
		t.Fatalf("a pair code was printed despite the refusal:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "pair_codes.json")); statErr == nil {
		t.Fatal("the pair code store was written despite the refusal")
	}
}

// pretendTerminal makes stdout a terminal for the test, and replaces the run
// of `tmux load-buffer` with load.
func pretendTerminal(t *testing.T, load func(text string) error) {
	t.Helper()
	origTerm, origLoad := stdoutIsTerminal, runTmuxLoadBuffer
	stdoutIsTerminal = func(io.Writer) bool { return true }
	runTmuxLoadBuffer = load
	t.Cleanup(func() { stdoutIsTerminal, runTmuxLoadBuffer = origTerm, origLoad })
}

// printedURI is the pair URI out printed, and the offset just past its line.
func printedURI(t *testing.T, out string) (uri string, end int) {
	t.Helper()
	const label = "Pair URI:    "
	i := strings.Index(out, label)
	if i < 0 {
		t.Fatalf("no pair URI line in:\n%s", out)
	}
	uri, _, _ = strings.Cut(out[i+len(label):], "\n")
	return uri, i + len(label) + len(uri) + 1
}

// copySequence is CopySequence's bytes for uri in the test's environment,
// checked to be OSC 52 carrying uri.
func copySequence(t *testing.T, uri string) string {
	t.Helper()
	seq, _, err := termsvc.CopySequence(termcap.EnvCaps(termcap.Env(os.Environ()), runtime.GOOS), uri)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seq, "\x1b]52;") || !strings.Contains(seq, base64.StdEncoding.EncodeToString([]byte(uri))) {
		t.Fatalf("CopySequence = %+q, want OSC 52 carrying the URI", seq)
	}
	return seq
}

// With a terminal, outside tmux, --copy writes the OSC 52 sequence for the
// URI it printed, after the URI line, and then says the terminal does not
// confirm the copy. Bare `pair` runs `pair code`, and takes the flag too
// (MADR 0219 D4).
func TestPairCodeCopyWritesOSC52(t *testing.T) {
	t.Setenv("TMUX", "")
	pretendTerminal(t, func(string) error {
		t.Error("tmux load-buffer ran outside tmux")
		return nil
	})

	for _, args := range [][]string{{"code", "--qr=false", "--copy"}, {"--qr=false", "--copy"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			writePairConfig(t)
			out, err := runPair(t, args...)
			if err != nil {
				t.Fatalf("pair %v: %v\n%s", args, err, out)
			}
			uri, uriEnd := printedURI(t, out)
			seq := copySequence(t, uri)
			j := strings.Index(out, seq)
			k := strings.Index(out, "the terminal does not confirm it")
			if j < uriEnd || k < j+len(seq) {
				t.Fatalf("want the URI line, then the sequence, then the unconfirmed line; "+
					"URI line ends at %d, sequence at %d, line at %d:\n%+q", uriEnd, j, k, out)
			}
		})
	}
}

// Inside tmux, whose defaults ignore a program's OSC 52, --copy hands the URI
// to `tmux load-buffer -w -`, which copies it to the outer terminal itself,
// and writes no OSC 52 (MADR 0219 D4, deviation 6).
func TestPairCodeCopyUsesTmuxBuffer(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	var loaded []string
	pretendTerminal(t, func(text string) error {
		loaded = append(loaded, text)
		return nil
	})
	writePairConfig(t)
	out, err := runPair(t, "code", "--qr=false", "--copy")
	if err != nil {
		t.Fatalf("pair code --copy: %v\n%s", err, out)
	}
	uri, uriEnd := printedURI(t, out)
	if len(loaded) != 1 || loaded[0] != uri {
		t.Fatalf("tmux load-buffer got %q, want the printed URI once", loaded)
	}
	if strings.Contains(out, "\x1b]52;") {
		t.Fatalf("an OSC 52 sequence was written inside tmux:\n%+q", out)
	}
	k := strings.Index(out, "Copied the pair URI through tmux")
	if k < uriEnd || !strings.Contains(out[k:], "the terminal does not confirm it") {
		t.Fatalf("want the URI line, then the tmux line saying the copy is unconfirmed:\n%+q", out)
	}
}

// When tmux load-buffer fails, --copy says why and falls back to the OSC 52
// sequence, so a tmux that passes OSC 52 still gets the copy.
func TestPairCodeCopyFallsBackWhenTmuxFails(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	pretendTerminal(t, func(string) error { return errors.New("no server running") })
	writePairConfig(t)
	out, err := runPair(t, "code", "--qr=false", "--copy")
	if err != nil {
		t.Fatalf("pair code --copy: %v\n%s", err, out)
	}
	uri, uriEnd := printedURI(t, out)
	w := strings.Index(out, "no server running")
	j := strings.Index(out, copySequence(t, uri))
	if w < uriEnd || j < w {
		t.Fatalf("want the URI line, then the warning naming tmux's error, then the sequence; "+
			"URI line ends at %d, warning at %d, sequence at %d:\n%+q", uriEnd, w, j, out)
	}
}

// pair create's URI carries the device's long-lived token, so it has no
// --copy (MADR 0219 Q1).
func TestPairCreateHasNoCopy(t *testing.T) {
	writePairConfig(t)
	out, err := runPair(t, "create", "--qr=false", "--copy")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --copy") {
		t.Fatalf("pair create --copy: err = %v, want an unknown flag\n%s", err, out)
	}
}
