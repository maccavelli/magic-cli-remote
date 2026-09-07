package event

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// declaredTypes reads every `Type… Type = "…"` constant out of this package's
// own source. Reading the source rather than keeping a hand-written list is the
// point: a list would need updating by the same person who forgot to classify
// the new type, so it would agree with the mistake.
func declaredTypes(t *testing.T) map[Type]string {
	t.Helper()
	out := map[Type]string{}
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Type == nil {
					continue
				}
				id, ok := vs.Type.(*ast.Ident)
				if !ok || id.Name != "Type" {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					out[Type(v)] = name.Name
				}
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("only found %d event types; the source scan is broken, not the classifier", len(out))
	}
	return out
}

func TestClassOfIsExhaustive(t *testing.T) {
	// ClassOf's default is ClassTelemetry — evicted first. A new type that
	// nobody classified would silently land there, which is the wrong answer
	// for anything worth adding to a transcript. This test is what makes that
	// impossible: it compares the switch's own case list against every declared
	// constant.
	declared := declaredTypes(t)

	src, err := os.ReadFile("retention.go")
	if err != nil {
		t.Fatalf("read retention.go: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "func ClassOf(")
	if start < 0 {
		t.Fatal("ClassOf not found")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("ClassOf body not delimited")
	}
	switchBody := body[start : start+end]

	var missing []string
	for _, constName := range declared {
		if !strings.Contains(switchBody, constName+",") && !strings.Contains(switchBody, constName+":") {
			missing = append(missing, constName)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("event types with no explicit class (they would fall to ClassTelemetry and be evicted first): %v", missing)
	}
}

func TestAnchorsOutrankEverythingElse(t *testing.T) {
	anchors := []Type{
		TypeUserMessage, TypeTurnComplete, TypeError,
		TypePermission, TypePermissionResolved,
		TypeQuestion, TypeQuestionResolved, TypeTranscriptRemove,
	}
	for _, tp := range anchors {
		if got := ClassOf(tp); got != ClassAnchor {
			t.Errorf("%s is %s, want anchor: it is part of what a conversation is", tp, got)
		}
	}
	// The three types that filled 73% of the operator's transcript bytes must
	// all rank below content (MADR 0138 F16).
	for _, tp := range []Type{TypeAvailableCommands, TypeToolUpdate, TypeNotice} {
		if ClassOf(tp) >= ClassContent {
			t.Errorf("%s ranks %s; the bulk telemetry types must be evicted before content", tp, ClassOf(tp))
		}
	}
}

func TestBytesTracksStructGrowth(t *testing.T) {
	// A 45th field the budget does not count is how a budget silently stops
	// bounding anything. eventHeaderBytes is derived from the type, so this
	// asserts that derivation rather than a written-down number.
	if got, want := eventHeaderBytes, int(unsafe.Sizeof(Event{})); got != want {
		t.Fatalf("eventHeaderBytes = %d, want %d", got, want)
	}

	// Every string field on Event must be summed by Bytes. Fill each one and
	// check the total moves; a field Bytes forgets shows up as a zero delta.
	base := Event{}
	baseline := Bytes(&base)
	rt := reflect.TypeOf(Event{})
	var forgotten []string
	for i := range rt.NumField() {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.String || f.Name == "Type" {
			continue
		}
		ev := Event{}
		reflect.ValueOf(&ev).Elem().Field(i).SetString(strings.Repeat("q", 64))
		if Bytes(&ev)-baseline != 64 {
			forgotten = append(forgotten, f.Name)
		}
	}
	if len(forgotten) > 0 {
		t.Fatalf("string fields Bytes does not count: %v", forgotten)
	}
}

func TestBytesDoesNotUnderReportRetainedSize(t *testing.T) {
	// The budget's whole job is to bound memory, so under-reporting is the
	// failure that matters. Compare the accounting against what the allocator
	// actually retains for the same events.
	const n = 20000
	evs := make([]Event, 0, n)
	accounted := 0

	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for i := range n {
		ev := Event{
			Type:      TypeAssistantChunk,
			SessionID: "session-0000-1111-2222",
			Text:      strings.Repeat("t", 700+i%64),
			ToolID:    "tool-abcdef",
		}
		accounted += Bytes(&ev)
		evs = append(evs, ev)
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	runtime.KeepAlive(evs)

	retained := int(m1.HeapAlloc - m0.HeapAlloc)
	if retained <= 0 {
		t.Skip("heap delta not measurable in this environment")
	}
	// Under-reporting by more than 25% means the budget does not bound what it
	// claims to (0138-PLAN, "what would falsify this plan").
	if accounted < retained*3/4 {
		t.Fatalf("Bytes accounted %d for %d retained (%.0f%%); it must not under-report by more than 25%%",
			accounted, retained, 100*float64(accounted)/float64(retained))
	}
	t.Logf("accounted=%d retained=%d ratio=%.2f mean=%d B/event",
		accounted, retained, float64(accounted)/float64(retained), accounted/n)
}
