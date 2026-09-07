package httpagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// sessionPayload is the shape both engines publish for a session, taken from
// their own OpenAPI documents (kilo 7.5.6 and opencode, read live). The two
// schemas are identical, which is why one implementation serves both.
const sessionPayload = `{
	"id":"ses_abc","title":"a session",
	"cost": 0.0234,
	"tokens":{"input":12000,"output":800,"reasoning":150,"cache":{"read":9000,"write":400}}
}`

// TestSessionTotalsDecodeFromTheSessionObject is Q3's assertion.
//
// `cost` is a JSON number in USD. Not cents, as grok's billing uses, and not
// 1e10 ticks, as grok's session usage uses. Three transports, three money
// units.
// decodeTotals decodes a payload, reporting a decode failure as a test failure
// rather than a panic — an integer `cost` field makes encoding/json return an
// UnmarshalTypeError, which is exactly the regression this guards.
func decodeTotals(t *testing.T, payload string) sessionTotals {
	t.Helper()
	var got sessionTotals
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("decode %s: %v\n\nThe engines send `cost` as a fractional JSON number; "+
			"an integer field cannot hold one and encoding/json refuses it outright.", payload, err)
	}
	return got
}

func TestSessionTotalsDecodeFromTheSessionObject(t *testing.T) {
	got := decodeTotals(t, sessionPayload)
	// float64() so this comparison compiles whatever the field's type is: a
	// build break is not the assertion firing, and a fail-first that cannot
	// compile has demonstrated nothing.
	if float64(got.Cost) != 0.0234 {
		t.Errorf("cost = %v, want 0.0234. A zero here means the `cost` tag no longer "+
			"matches the wire, which is silent: every session then reports $0.00", got.Cost)
	}
	if got.Tokens.Input != 12000 || got.Tokens.Output != 800 {
		t.Errorf("tokens = %v in / %v out, want 12000/800", got.Tokens.Input, got.Tokens.Output)
	}
	if got.Tokens.Cache.Read != 9000 {
		t.Errorf("cache.read = %v, want 9000", got.Tokens.Cache.Read)
	}
	if got.Tokens.Reasoning != 150 {
		t.Errorf("reasoning = %v, want 150", got.Tokens.Reasoning)
	}
}

// TestFormatSessionTotalsStatesSubCentCosts.
//
// A real session commonly costs a fraction of a cent, and two decimal places
// would render it as $0.00 — the same wrong number Phase 10 went out of its way
// to avoid on grok.
func TestFormatSessionTotalsStatesSubCentCosts(t *testing.T) {
	var totals sessionTotals
	if err := json.Unmarshal([]byte(sessionPayload), &totals); err != nil {
		t.Fatal(err)
	}
	got := formatSessionTotals(totals)
	for _, want := range []string{"12000 input", "800 output", "9000 cached", "150 reasoning", "$0.0234"} {
		if !strings.Contains(got, want) {
			t.Errorf("usage = %q, want %q", got, want)
		}
	}

	// Decoded rather than composed from literals: a struct literal with a
	// fractional cost stops compiling if the field's type is wrong, and a test
	// that fails to build has not exercised its assertion.
	small := decodeTotals(t, `{"cost":0.0004}`)
	if got := formatSessionTotals(small); !strings.Contains(got, "$0.0004") {
		t.Errorf("usage = %q — a sub-cent cost was rounded away", got)
	}
	big := decodeTotals(t, `{"cost":12.5}`)
	if got := formatSessionTotals(big); !strings.Contains(got, "$12.50") {
		t.Errorf("usage = %q, want two decimals above a dollar", got)
	}
	// Zero here is a *reported* zero: both engines make `cost` required, so
	// unlike grok there is no absent-versus-zero distinction to preserve.
	if got := formatSessionTotals(sessionTotals{}); !strings.Contains(got, "$0.00") {
		t.Errorf("usage = %q, want a reported zero shown as zero", got)
	}
}

// TestHTTPEnginesImplementRuntimeSession is acceptance 2 and 3's precondition.
func TestHTTPEnginesImplementRuntimeSession(t *testing.T) {
	var s any = &session{}
	if _, ok := s.(provider.RuntimeSession); !ok {
		t.Fatal("the httpagent session does not implement provider.RuntimeSession; " +
			"/status and /usage would stay dead on kilo and opencode")
	}
}

// TestTitleIDIsSafeOnAnEmptyID guards the display helper against a dialect that
// reports no id.
func TestTitleIDIsSafeOnAnEmptyID(t *testing.T) {
	if got := titleID(""); got != "Engine" {
		t.Errorf("titleID(\"\") = %q", got)
	}
	if got := titleID(provider.IDKilo); got != "Kilo" {
		t.Errorf("titleID(kilo) = %q", got)
	}
}

// stubDialect is the minimum Dialect. It deliberately does NOT implement
// RuntimeDialect, which is the case Q5 exercises.
type stubDialect struct{ id provider.ID }

func (d stubDialect) ID() provider.ID                { return d.id }
func (d stubDialect) DefaultBin() string             { return "stub" }
func (d stubDialect) ServeArgs(int) []string         { return nil }
func (d stubDialect) HealthPath() string             { return "/health" }
func (d stubDialect) EventsPath() string             { return "/event" }
func (d stubDialect) AfterBoot(context.Context, API) {}
func (d stubDialect) DecodeFrame([]byte) (string, json.RawMessage, string, bool) {
	return "", nil, "", false
}
func (d stubDialect) NewSession(Host) DialectSession { return nil }

// runtimeDialect wraps stubDialect with a status the test controls.
type runtimeDialect struct {
	stubDialect
	msg string
	err error
}

func (d runtimeDialect) RuntimeStatus(context.Context, API) (string, error) {
	return d.msg, d.err
}

// TestSessionUsageReadsTheSessionObjectAndNothingElse is Q4.
//
// One source of truth. kilo publishes the same totals a second time under
// /session/{id}/model-usage, and reading both is how two numbers for one
// session start to disagree.
func TestSessionUsageReadsTheSessionObjectAndNothingElse(t *testing.T) {
	var paths []string
	api := func(_ context.Context, method, path string, _, out any) error {
		paths = append(paths, method+" "+path)
		return json.Unmarshal([]byte(sessionPayload), out)
	}

	totals, err := fetchSessionTotals(context.Background(), api, "ses_abc")
	if err != nil {
		t.Fatalf("fetchSessionTotals: %v", err)
	}
	if len(paths) != 1 || paths[0] != "GET /session/ses_abc" {
		t.Fatalf("engine calls = %v, want exactly [GET /session/ses_abc]. The session object "+
			"already carries the totals; model-usage is a second source for the same figure", paths)
	}
	if float64(totals.Cost) != 0.0234 {
		t.Errorf("cost = %v", totals.Cost)
	}
}

// TestStatusWithoutARuntimeDialectStillAnswers is Q5.
//
// Same claim as Phase 10's P4 for grok: cmdRuntime swallows a returned error,
// so a dialect with nothing to report must still produce a line.
func TestStatusWithoutARuntimeDialectStillAnswers(t *testing.T) {
	api := func(context.Context, string, string, any, any) error {
		t.Fatal("a dialect with no RuntimeDialect must not call the engine")
		return nil
	}
	got := runtimeStatus(context.Background(), stubDialect{id: provider.IDOpencode}, api, "Opencode")
	if !strings.Contains(got, "no plan usage") {
		t.Errorf("status = %q, want it to say the engine publishes none", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Error("status was empty; /status would show nothing at all")
	}
}

// TestStatusReportsADialectFailureAsText keeps a broken probe visible.
func TestStatusReportsADialectFailureAsText(t *testing.T) {
	d := runtimeDialect{stubDialect: stubDialect{id: provider.IDKilo},
		err: errors.New("engine refused: 503")}
	got := runtimeStatus(context.Background(), d, nil, "Kilo")
	if !strings.Contains(got, "status unavailable") || !strings.Contains(got, "503") {
		t.Errorf("status = %q, want the failure named", got)
	}

	// An empty answer is treated as "nothing to say", not as an answer.
	blank := runtimeDialect{stubDialect: stubDialect{id: provider.IDKilo}, msg: "   "}
	if got := runtimeStatus(context.Background(), blank, nil, "Kilo"); !strings.Contains(got, "no plan usage") {
		t.Errorf("status = %q, want the fallback line for an empty dialect answer", got)
	}

	ok := runtimeDialect{stubDialect: stubDialect{id: provider.IDKilo}, msg: "Kilo · credits 3/50"}
	if got := runtimeStatus(context.Background(), ok, nil, "Kilo"); got != "Kilo · credits 3/50" {
		t.Errorf("status = %q, want the dialect's own line", got)
	}
}

// Payloads read from the running engines on 2026-09-04, read-only, against
// real sessions. Recorded here so a one-off observation becomes a regression
// guard rather than a note in a document.
//
// They are trimmed to the two fields this code reads. The full session object
// carries a dozen more, which is why nothing here decodes strictly: this is a
// deliberately partial view, and DisallowUnknownFields would reject every field
// the daemon does not care about.
const (
	liveKiloSession = `{"cost":0.043495776,
		"tokens":{"input":15551,"output":47,"reasoning":32,"cache":{"read":30720,"write":0}}}`
	liveOpencodeSession = `{"cost":0,
		"tokens":{"input":220,"output":146,"reasoning":0,"cache":{"read":39040,"write":0}}}`
)

// TestLivePayloadsFromBothEnginesDecodeIdentically is acceptance 6.
//
// The two engines publish the same schema, which is the finding that let one
// implementation serve both. This pins it against what they actually sent, not
// against what their OpenAPI documents promise.
func TestLivePayloadsFromBothEnginesDecodeIdentically(t *testing.T) {
	kilo := decodeTotals(t, liveKiloSession)
	if float64(kilo.Cost) != 0.043495776 {
		t.Errorf("kilo cost = %v, want 0.043495776", kilo.Cost)
	}
	// The formatter's four decimals earn their place here: this real session
	// cost 4.3 cents, and "$0.04" would have thrown away the third digit on a
	// figure that is already small.
	if got := formatSessionTotals(kilo); !strings.Contains(got, "$0.0435") {
		t.Errorf("kilo usage = %q, want $0.0435", got)
	}
	if kilo.Tokens.Cache.Read != 30720 {
		t.Errorf("kilo cache.read = %v", kilo.Tokens.Cache.Read)
	}

	// opencode sent `0` — a JSON integer, not `0.0`. A float64 field takes
	// both, which an integer field would not have done in reverse.
	oc := decodeTotals(t, liveOpencodeSession)
	if float64(oc.Cost) != 0 {
		t.Errorf("opencode cost = %v, want 0", oc.Cost)
	}
	if oc.Tokens.Input != 220 || oc.Tokens.Cache.Read != 39040 {
		t.Errorf("opencode tokens = %+v", oc.Tokens)
	}
	if got := formatSessionTotals(oc); !strings.Contains(got, "220 input") {
		t.Errorf("opencode usage = %q", got)
	}
}
