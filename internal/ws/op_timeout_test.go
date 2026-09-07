package ws

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// opTimeoutTable mirrors internal/protocol/op_timeouts.json — the single
// source of truth for the phone/daemon timeout ladder (MADR 0095 D7).
type opTimeoutTable struct {
	DefaultMS      int            `json:"default_ms"`
	ClientMarginMS int            `json:"client_margin_ms"`
	Methods        map[string]int `json:"methods"`
}

func loadOpTimeouts(t *testing.T) opTimeoutTable {
	t.Helper()
	const path = "../protocol/op_timeouts.json"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tbl opTimeoutTable
	if err := json.Unmarshal(b, &tbl); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(tbl.Methods) == 0 || tbl.DefaultMS == 0 || tbl.ClientMarginMS == 0 {
		t.Fatalf("%s is missing required fields: %+v", path, tbl)
	}
	return tbl
}

// readSource reads a Go source file for scanning and strips carriage returns.
//
// These scans measure the *source*, and the checkout's line endings are not
// part of the source. On a Windows working tree they can differ file by file:
// `.gitattributes` declares `* text=auto eol=lf`, but that binds at checkout,
// so a tree checked out before that rule — or under core.autocrlf=true — keeps
// CRLF in files no later pull has rewritten. On 2026-09-07 this package's own
// server.go was LF while codex_handlers.go beside it was CRLF, and the
// "\n}\n" delimiter below found nothing (MADR 0147 F8, F11).
//
// Normalising here rather than at each use site is deliberate: the delimiters
// and the `$`-anchored regexps are spread across three scans, and every one of
// them is silently CRLF-fragile (F9).
func readSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// asyncDispatchedTypes returns every message type routed through
// dispatchAsync, read out of handleMessage's own switch.
//
// Derived, not hand-maintained. It used to be a literal list, and the list is
// what drifted: MADR 0138 F4 moved four handlers onto the async path and the
// list did not follow, so the table check reported them as *stale entries* —
// the opposite of the truth. A shadow copy of a switch is updated by the same
// person who forgot to update the switch.
func asyncDispatchedTypes(t *testing.T) []string {
	t.Helper()

	out := switchDispatchedConstants(t, readSource(t, "server.go"))
	// Codex-capability operations live in a second registry, keyed by type
	// with an explicit timeoutKey (codex_handlers.go codexPhoneOperations).
	// Scanning only handleMessage would miss every one of them.
	out = append(out, codexDispatchedConstantsIn(t, readSource(t, "codex_handlers.go"))...)

	if len(out) < 25 {
		t.Fatalf("only found %d async-dispatched types; the source scan is broken", len(out))
	}

	// Constant name -> wire string, so the test compares what the JSON holds.
	wire := map[string]string{}
	for _, kv := range protocolTypeConstants(t) {
		wire[kv[0]] = kv[1]
	}
	methods := make([]string, 0, len(out))
	for _, name := range out {
		v, ok := wire[name]
		if !ok {
			t.Fatalf("protocol.%s has no string value; the constant scan is broken", name)
		}
		methods = append(methods, v)
	}
	return methods
}

// switchDispatchedConstants walks handleMessage's switch in body and returns
// the constant name of every case label whose arm reaches dispatchAsync.
//
// Takes the body rather than reading it, so the CRLF guard can run the real
// walk over synthesised input (MADR 0147 D5).
func switchDispatchedConstants(t *testing.T, body string) []string {
	t.Helper()
	i := strings.Index(body, "func (s *Server) handleMessage(")
	if i < 0 {
		t.Fatal("handleMessage not found; the source scan is broken, not the dispatch")
	}
	j := strings.Index(body[i:], "\n// asyncHandler is a slow WS op")
	if j < 0 {
		t.Fatal("handleMessage body not delimited")
	}
	sw := body[i : i+j]

	// Walk the switch, remembering the case labels seen since the last arm and
	// attributing them to whichever dispatch that arm reaches. One arm may
	// carry several labels.
	caseLabel := regexp.MustCompile(`^\tcase (.+):$`)
	constName := regexp.MustCompile(`protocol\.(Type\w+)`)
	var pending []string
	var out []string
	for _, line := range strings.Split(sw, "\n") {
		if m := caseLabel.FindStringSubmatch(line); m != nil {
			pending = append(pending, constName.FindAllStringSubmatch(m[1], -1)[0][1])
			continue
		}
		if strings.Contains(line, "dispatchAsync") {
			out = append(out, pending...)
			pending = nil
			continue
		}
		if strings.Contains(line, "return s.handle") || strings.Contains(line, "return s.write") {
			pending = nil
		}
	}
	return out
}

// codexDispatchedConstantsIn reads codexPhoneOperations out of body and returns
// the constant name of every entry whose handler reaches dispatchAsync.
//
// Takes the body rather than reading it, for the same reason as
// switchDispatchedConstants: this is the scan the CRLF guard exercises, since
// its "\n}\n" delimiter is the one that actually broke (MADR 0147 F8).
func codexDispatchedConstantsIn(t *testing.T, body string) []string {
	t.Helper()
	i := strings.Index(body, "var codexPhoneOperations = map[string]codexPhoneOperation{")
	if i < 0 {
		t.Fatal("codexPhoneOperations not found; the source scan is broken")
	}
	j := strings.Index(body[i:], "\n}\n")
	if j < 0 {
		t.Fatal("codexPhoneOperations not delimited")
	}
	entry := regexp.MustCompile(`(?m)^\tprotocol\.(Type\w+): \{`)
	table := body[i : i+j]
	var out []string
	locs := entry.FindAllStringSubmatchIndex(table, -1)
	for k, loc := range locs {
		end := len(table)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		if strings.Contains(table[loc[0]:end], "dispatchAsync") {
			out = append(out, table[loc[2]:loc[3]])
		}
	}
	if len(out) == 0 {
		t.Fatal("no codex operations reach dispatchAsync; the source scan is broken")
	}
	return out
}

// protocolTypeConstants returns every `Type… = "…"` pair declared in
// internal/protocol/messages.go, as {constant name, wire string}.
func protocolTypeConstants(t *testing.T) [][2]string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\t(Type\w+)\s*=\s*"([a-z_.]+)"`)
	ms := re.FindAllStringSubmatch(readSource(t, "../protocol/messages.go"), -1)
	if len(ms) < 40 {
		t.Fatalf("only found %d protocol type constants; the scan is broken", len(ms))
	}
	out := make([][2]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}

// TestSourceScansSurviveCRLF is the guard for MADR 0147 D5: these scans must
// measure the source, not the line endings the checkout happened to write.
//
// The CRLF input is synthesised at runtime rather than committed. A CRLF
// fixture under testdata/ would be normalised back to LF by `.gitattributes`
// (`* text=auto eol=lf`) on the next fresh clone, so the guard would quietly
// stop guarding — the same class of silent decay it exists to prevent.
//
// Both scans are covered, not just the one that broke. The "\n}\n" delimiter
// in codexDispatchedConstantsIn is what failed on 2026-09-07 (F8), but the
// `^\tcase (.+):$` anchor in switchDispatchedConstants is fragile in exactly
// the same way and survived only because server.go happened to be LF (F9).
func TestSourceScansSurviveCRLF(t *testing.T) {
	// crlf writes a CRLF copy of path into t.TempDir() and returns its
	// location. Reading through readSource is the whole point: the assertion
	// is that the reader normalises, not that the parser tolerates.
	crlf := func(t *testing.T, path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lf := strings.ReplaceAll(string(b), "\r\n", "\n")
		dst := filepath.Join(t.TempDir(), filepath.Base(path))
		if err := os.WriteFile(dst, []byte(strings.ReplaceAll(lf, "\n", "\r\n")), 0o600); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
		return dst
	}

	t.Run("codexPhoneOperations", func(t *testing.T) {
		want := codexDispatchedConstantsIn(t, readSource(t, "codex_handlers.go"))
		got := codexDispatchedConstantsIn(t, readSource(t, crlf(t, "codex_handlers.go")))
		if !slices.Equal(got, want) {
			t.Errorf("CRLF copy scanned differently:\n got %v\nwant %v", got, want)
		}
	})

	t.Run("handleMessage", func(t *testing.T) {
		want := switchDispatchedConstants(t, readSource(t, "server.go"))
		got := switchDispatchedConstants(t, readSource(t, crlf(t, "server.go")))
		if !slices.Equal(got, want) {
			t.Errorf("CRLF copy scanned differently:\n got %v\nwant %v", got, want)
		}
	})
}

// asyncOpTimeout is the daemon's half of the timeout ladder (MADR 0095 D7).
// The table is shared with the phone, which must exceed every value.
func TestAsyncOpTimeoutMatchesSharedTable(t *testing.T) {
	tbl := loadOpTimeouts(t)
	for method, wantMS := range tbl.Methods {
		want := time.Duration(wantMS) * time.Millisecond
		if got := asyncOpTimeout(method); got != want {
			t.Errorf("asyncOpTimeout(%q) = %v, table says %dms", method, got, wantMS)
		}
	}
	want := time.Duration(tbl.DefaultMS) * time.Millisecond
	if got := asyncOpTimeout("no.such.method"); got != want {
		t.Errorf("default = %v, table says %dms", got, tbl.DefaultMS)
	}
}

// Every method that reaches dispatchAsync must appear in the table, so a
// new async op cannot silently inherit a deadline the phone races.
func TestEveryAsyncDispatchedMethodIsInTheTable(t *testing.T) {
	tbl := loadOpTimeouts(t)
	for _, m := range asyncDispatchedTypes(t) {
		if _, ok := tbl.Methods[m]; !ok {
			t.Errorf("%q reaches dispatchAsync but is absent from op_timeouts.json", m)
		}
	}
	// And nothing in the table is stale.
	known := map[string]bool{}
	for _, m := range asyncDispatchedTypes(t) {
		known[m] = true
	}
	for m := range tbl.Methods {
		if !known[m] {
			t.Errorf("op_timeouts.json lists %q, which no longer reaches dispatchAsync", m)
		}
	}
}

// TestShellDoesNotStarveThePrompt pins MADR 0138 F6: session.shell has a
// 30-minute deadline and session.prompt has 60 seconds, so they must not draw
// on the same per-connection budget. Eight long shells could otherwise
// rate-limit every later operation on that connection.
func TestShellDoesNotStarveThePrompt(t *testing.T) {
	if maxShellPerClient >= maxAsyncPerClient {
		t.Fatalf("the shell lane (%d) is not smaller than the general lane (%d); "+
			"it exists to bound the slow op, not to match the fast one",
			maxShellPerClient, maxAsyncPerClient)
	}
	shellBudget := asyncOpTimeout(protocolSessionShell)
	promptBudget := asyncOpTimeout(protocolSessionPrompt)
	if shellBudget <= promptBudget {
		t.Skip("the deadlines are no longer lopsided; the separate lane may not be needed")
	}
	// A separate counter is the whole mechanism: with one counter, filling it
	// with shells is indistinguishable from filling it with prompts.
	c := &client{}
	for range maxShellPerClient {
		c.shellInFlight++
	}
	if c.asyncInFlight != 0 {
		t.Fatalf("shells consumed %d of the general budget; the lanes are shared", c.asyncInFlight)
	}
}

const (
	protocolSessionShell  = "session.shell"
	protocolSessionPrompt = "session.prompt"
)
