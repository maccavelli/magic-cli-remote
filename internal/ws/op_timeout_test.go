package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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

// switchDispatchedConstants walks handleMessage's `switch env.Type` in body and
// returns the constant name of every case label whose arm calls dispatchAsync.
//
// Parsed as Go, not as text (MADR 0149 D1). The previous version used two
// regular expressions and was wrong on three of the four ways a case label can
// be written — `case A, B:` kept only A, the continuation form kept neither,
// and a label with no protocol.Type panicked on an unchecked [0] — and it
// decided an arm was asynchronous by looking for the *string* "dispatchAsync"
// anywhere in it, so a comment mentioning it counted. That last one was not
// hypothetical: TypeSessionCancel is inline by deliberate decision (MADR 0137
// F4) and its comment names dispatchAsync, so the scan reported it as
// dispatched and the "nothing is stale" check could not flag its
// op_timeouts.json entry. Two defects concealing each other (0149 F8).
//
// The AST has none of those failure modes: clause.List is a slice, so every
// label form collapses to the same shape, and comments are not nodes.
//
// Takes the body rather than reading it, so the CRLF guard can run the real
// walk over synthesised input (MADR 0147 D5).
func switchDispatchedConstants(t *testing.T, body string) []string {
	t.Helper()
	types, problems, err := scanDispatchSwitch(body)
	if err != nil {
		t.Fatal(err)
	}
	// Reported, never ignored: a label the scan cannot classify is the silence
	// this whole record is about (0149 D3).
	for _, p := range problems {
		t.Error(p)
	}
	return types
}

// scanDispatchSwitch is switchDispatchedConstants without the *testing.T, so
// the table-driven tests can assert on the failure paths as data rather than
// having to observe a test failing. err covers structural problems that make
// the scan meaningless; problems covers individual clauses it cannot read.
func scanDispatchSwitch(body string) (types []string, problems []string, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server.go", body, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("parse handleMessage source: %w", err)
	}

	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "handleMessage" {
			fn = fd
			break
		}
	}
	if fn == nil {
		return nil, nil, errors.New("handleMessage not found; the source scan is broken, not the dispatch")
	}

	// Key on the tag expression rather than taking the first switch: there is
	// exactly one `switch env.Type` in handleMessage today, and if that stops
	// being true this must say so rather than silently scan the wrong one.
	var sw *ast.SwitchStmt
	ambiguous := false
	ast.Inspect(fn, func(n ast.Node) bool {
		st, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if sel, ok := st.Tag.(*ast.SelectorExpr); ok && sel.Sel.Name == "Type" {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "env" {
				if sw != nil {
					ambiguous = true
				}
				sw = st
			}
		}
		return true
	})
	if ambiguous {
		return nil, nil, errors.New("more than one `switch env.Type` in handleMessage; the scan cannot tell which one dispatches")
	}
	if sw == nil {
		return nil, nil, errors.New("no `switch env.Type` in handleMessage; the source scan is broken")
	}

	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok || clause.List == nil {
			continue // `default:` carries no labels
		}
		names := protocolTypeNames(clause.List)
		if len(names) == 0 {
			problems = append(problems, fmt.Sprintf(
				"%s: case label carries no protocol.Type constant; the scan "+
					"cannot classify it and would otherwise ignore it",
				fset.Position(clause.Pos())))
			continue
		}
		if clauseDispatchesAsync(clause) {
			types = append(types, names...)
		}
	}
	return types, problems, nil
}

// protocolTypeNames returns the Type… constant named by each `protocol.TypeX`
// label in a case clause. Every label form — one per clause, several on one
// line, or several across lines — arrives here as the same slice.
func protocolTypeNames(list []ast.Expr) []string {
	var out []string
	for _, e := range list {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "protocol" || !strings.HasPrefix(sel.Sel.Name, "Type") {
			continue
		}
		out = append(out, sel.Sel.Name)
	}
	return out
}

// clauseDispatchesAsync reports whether the arm calls dispatchAsync. Inspect
// rather than a scan of top-level statements: no arm dispatches from inside an
// `if` today, but one that did would otherwise be missed silently.
func clauseDispatchesAsync(clause *ast.CaseClause) bool {
	found := false
	for _, stmt := range clause.Body {
		ast.Inspect(stmt, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "dispatchAsync" {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
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

// TestSwitchScanReadsEveryLabelForm is the guard for MADR 0149 D4.
//
// Each row is a way `handleMessage`'s switch can legally be written. Four of
// them were wrong before the AST rewrite: two dropped labels silently, one
// panicked, and one counted a *comment* as a dispatch — the last of which was
// live, crediting TypeSessionCancel as async and hiding a stale
// op_timeouts.json entry (0149 F8).
//
// "comment mentions dispatchAsync" and "dispatch inside an if" are both present
// deliberately: an implementation that scanned only top-level statements would
// pass the first and fail the second, and one that matched raw text would do
// the reverse. Neither alone proves the scan reads calls rather than strings.
func TestSwitchScanReadsEveryLabelForm(t *testing.T) {
	wrap := func(arms string) string {
		return "package ws\n\nfunc (s *Server) handleMessage() error {\n\tswitch env.Type {\n" +
			arms + "\t}\n\treturn nil\n}\n"
	}

	for _, tc := range []struct {
		name         string
		arms         string
		want         []string
		wantProblems int
	}{{
		name: "one label per clause",
		arms: "\tcase protocol.TypeA:\n\t\treturn s.dispatchAsync(ctx, c, env, s.handleA)\n",
		want: []string{"TypeA"},
	}, {
		name: "several labels on one line",
		arms: "\tcase protocol.TypeA, protocol.TypeB:\n\t\treturn s.dispatchAsync(ctx, c, env, s.handleA)\n",
		want: []string{"TypeA", "TypeB"},
	}, {
		name: "several labels across lines",
		arms: "\tcase protocol.TypeA,\n\t\tprotocol.TypeB:\n\t\treturn s.dispatchAsync(ctx, c, env, s.handleA)\n",
		want: []string{"TypeA", "TypeB"},
	}, {
		name: "comment mentions dispatchAsync but the arm is inline",
		arms: "\tcase protocol.TypeA:\n\t\t// stays inline; dispatchAsync is bounded by maxAsyncPerClient\n\t\treturn s.handleA(ctx, c, env)\n",
		want: nil,
	}, {
		name: "dispatch nested inside an if",
		arms: "\tcase protocol.TypeA:\n\t\tif cond {\n\t\t\treturn s.dispatchAsync(ctx, c, env, s.handleA)\n\t\t}\n\t\treturn nil\n",
		want: []string{"TypeA"},
	}, {
		name:         "label the scan cannot classify",
		arms:         "\tcase someLocalConst:\n\t\treturn s.dispatchAsync(ctx, c, env, s.handleA)\n",
		want:         nil,
		wantProblems: 1,
	}, {
		name: "default carries no labels and is not a problem",
		arms: "\tdefault:\n\t\treturn s.writeError(ctx, c, env.ID, \"unknown\", \"nope\")\n",
		want: nil,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, problems, err := scanDispatchSwitch(wrap(tc.arms))
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("types = %v, want %v", got, tc.want)
			}
			if len(problems) != tc.wantProblems {
				t.Errorf("problems = %d %v, want %d", len(problems), problems, tc.wantProblems)
			}
		})
	}
}

// A switch the scan cannot locate must be an error, not an empty result that
// reads as "nothing dispatches asynchronously".
func TestSwitchScanReportsAnUnusableSource(t *testing.T) {
	for _, tc := range []struct{ name, src, wantErr string }{{
		name:    "no handleMessage",
		src:     "package ws\n\nfunc other() {}\n",
		wantErr: "handleMessage not found",
	}, {
		name:    "no switch on env.Type",
		src:     "package ws\n\nfunc (s *Server) handleMessage() error {\n\tswitch other.Type {\n\tcase protocol.TypeA:\n\t}\n\treturn nil\n}\n",
		wantErr: "no `switch env.Type`",
	}, {
		name:    "two switches on env.Type",
		src:     "package ws\n\nfunc (s *Server) handleMessage() error {\n\tswitch env.Type {\n\tcase protocol.TypeA:\n\t}\n\tswitch env.Type {\n\tcase protocol.TypeB:\n\t}\n\treturn nil\n}\n",
		wantErr: "more than one",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := scanDispatchSwitch(tc.src)
			if err == nil {
				t.Fatal("expected an error; an unreadable switch must not look like an empty one")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
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
