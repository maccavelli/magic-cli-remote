package opencode

import (
	"context"
	"log/slog"
	"strconv"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, pat int
		ok            bool
	}{
		{"1.18.4", 1, 18, 4, true},
		{"v1.18.4", 1, 18, 4, true},
		{"1.18.4-beta", 1, 18, 4, true},
		{"1.18", 1, 18, 0, true},
		{"", 0, 0, 0, false},
		{"not-a-version", 0, 0, 0, false},
	}
	for _, c := range cases {
		maj, min, pat, ok := parseSemver(c.in)
		if ok != c.ok || maj != c.maj || min != c.min || pat != c.pat {
			t.Errorf("parseSemver(%q)=%d.%d.%d ok=%v want %d.%d.%d ok=%v",
				c.in, maj, min, pat, ok, c.maj, c.min, c.pat, c.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("1.18.4", "1.18.0") <= 0 {
		t.Fatal("1.18.4 should be > 1.18.0")
	}
	if CompareVersions("1.17.9", MinVersion) >= 0 {
		t.Fatal("1.17.9 should be < MinVersion")
	}
	if CompareVersions("1.18.0", MinVersion) != 0 {
		t.Fatal("1.18.0 should equal MinVersion")
	}
	if !VersionMeetsMin("1.18.4") {
		t.Fatal("1.18.4 should meet min")
	}
	if VersionMeetsMin("1.17.0") {
		t.Fatal("1.17.0 should not meet min")
	}
}

func TestOnHealthyAndVersionGate(t *testing.T) {
	d := &httpDialect{log: slogDefault()}
	if err := d.OnHealthy([]byte(`{"healthy":true,"version":"1.17.0"}`)); err != nil {
		t.Fatal(err)
	}
	if d.EngineVersion() != "1.17.0" {
		t.Fatalf("version=%q", d.EngineVersion())
	}
	// Tree on → refuse old engine.
	err := d.CheckMinVersion(httpagent.Config{}) // default tree on
	if err == nil {
		t.Fatal("expected version pin error for 1.17 with tree on")
	}
	// Tree off → allow.
	off := false
	if err := d.CheckMinVersion(httpagent.Config{SessionTree: &off}); err != nil {
		t.Fatalf("kill switch should allow old engine: %v", err)
	}
	// Empty version → allow (unknown).
	d2 := &httpDialect{log: slogDefault()}
	if err := d2.CheckMinVersion(httpagent.Config{}); err != nil {
		t.Fatalf("unknown version must not block: %v", err)
	}
}

func slogDefault() *slog.Logger {
	return slog.Default()
}

// ---------------------------------------------------------------------------
// MADR 0112 D1/D2 — known-good pin as a policy distinct from the hard floor
// ---------------------------------------------------------------------------

func TestCompareVersionsCompleteMatrix(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		{"equal exact", "1.18.21", "1.18.21", 0},
		{"equal with v prefix", "v1.18.21", "1.18.21", 0},
		{"equal with capital V", "V1.18.21", "1.18.21", 0},
		{"equal with surrounding space", "  1.18.21  ", "1.18.21", 0},
		{"equal despite prerelease", "1.18.21-rc.1", "1.18.21", 0},
		{"equal despite build metadata", "1.18.21+darwin", "1.18.21", 0},
		{"missing patch equals explicit zero", "1.18", "1.18.0", 0},
		{"major less", "0.18.21", "1.18.21", -1},
		{"major greater", "2.0.0", "1.18.21", 1},
		{"minor less", "1.17.99", "1.18.21", -1},
		{"minor greater", "1.19.0", "1.18.21", 1},
		{"patch less", "1.18.20", "1.18.21", -1},
		{"patch greater", "1.18.22", "1.18.21", 1},
		// An unparseable version sorts below a parseable one, so a garbage
		// health body can never read as "new enough".
		{"malformed versus valid", "nonsense", "1.18.21", -1},
		{"valid versus malformed", "1.18.21", "nonsense", 1},
		{"malformed versus malformed", "nonsense", "garbage", 0},
		{"empty versus valid", "", "1.18.21", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CompareVersions(c.a, c.b); got != c.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestVersionIsKnownGoodMatrix(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{KnownGoodVersion, true},
		{"1.18.26", true},
		{"v1.18.26", true},
		{"1.18.26+build.7", true},
		{"1.18.26-rc.1", true},
		{"1.18.25", false},
		{"1.18.27", false},
		{"1.19.0", false},
		{"1.18", false},
		{"", false},
		{"nonsense", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := VersionIsKnownGood(c.in); got != c.want {
				t.Errorf("VersionIsKnownGood(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// The two constants must stay different policies. If they were ever collapsed,
// every release below the assessed one would become a hard startup failure.
func TestKnownGoodIsDistinctFromMinimum(t *testing.T) {
	if KnownGoodVersion == MinVersion {
		t.Fatal("KnownGoodVersion and MinVersion must remain distinct policies")
	}
	if CompareVersions(KnownGoodVersion, MinVersion) <= 0 {
		t.Fatalf("KnownGoodVersion %q must be above MinVersion %q", KnownGoodVersion, MinVersion)
	}
	if !VersionMeetsMin(KnownGoodVersion) {
		t.Fatal("the known-good release must itself clear the hard floor")
	}
}

// captureLogs returns a logger writing structured records into recs.
func captureLogs(t *testing.T) (*slog.Logger, *[]slogRecord) {
	t.Helper()
	recs := &[]slogRecord{}
	h := &recordingHandler{recs: recs}
	return slog.New(h), recs
}

type slogRecord struct {
	Level slog.Level
	Msg   string
	Attrs map[string]string
}

type recordingHandler struct {
	recs *[]slogRecord
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	rec := slogRecord{Level: r.Level, Msg: r.Message, Attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	*h.recs = append(*h.recs, rec)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func TestKnownGoodHealthMatrix(t *testing.T) {
	const (
		msgBelowMin = "opencode engine below minimum version for session-tree features"
		msgDrift    = "opencode engine differs from the known-good release"
		msgExact    = "opencode engine version"
	)
	cases := []struct {
		name        string
		body        string
		wantVersion string
		wantMsg     string // "" means nothing is logged
		wantLevel   slog.Level
	}{
		{
			name: "exact known-good logs info", body: `{"healthy":true,"version":"1.18.26"}`,
			wantVersion: "1.18.26", wantMsg: msgExact, wantLevel: slog.LevelInfo,
		},
		{
			name: "newer release warns as drift", body: `{"healthy":true,"version":"1.18.27"}`,
			wantVersion: "1.18.27", wantMsg: msgDrift, wantLevel: slog.LevelWarn,
		},
		{
			name: "much newer release warns as drift", body: `{"healthy":true,"version":"2.0.0"}`,
			wantVersion: "2.0.0", wantMsg: msgDrift, wantLevel: slog.LevelWarn,
		},
		{
			// Above the floor but below the pin: still drift, not a floor error.
			name: "older but supported release warns as drift", body: `{"healthy":true,"version":"1.18.7"}`,
			wantVersion: "1.18.7", wantMsg: msgDrift, wantLevel: slog.LevelWarn,
		},
		{
			name: "exactly the minimum warns as drift", body: `{"healthy":true,"version":"1.18.0"}`,
			wantVersion: "1.18.0", wantMsg: msgDrift, wantLevel: slog.LevelWarn,
		},
		{
			name: "below the minimum warns about the floor", body: `{"healthy":true,"version":"1.17.9"}`,
			wantVersion: "1.17.9", wantMsg: msgBelowMin, wantLevel: slog.LevelWarn,
		},
		{
			// An unparseable version is below everything, so it reports the
			// floor rather than being silently treated as supported.
			name: "unparseable version warns about the floor", body: `{"healthy":true,"version":"nonsense"}`,
			wantVersion: "nonsense", wantMsg: msgBelowMin, wantLevel: slog.LevelWarn,
		},
		{
			name: "missing version records nothing", body: `{"healthy":true}`,
			wantVersion: "", wantMsg: "",
		},
		{
			name: "blank version records nothing", body: `{"healthy":true,"version":"   "}`,
			wantVersion: "", wantMsg: "",
		},
		{
			// Non-JSON health must not fail boot: the version gate is a
			// separate decision from whether the engine answered at all.
			name: "malformed body is tolerated", body: `not json`,
			wantVersion: "", wantMsg: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, recs := captureLogs(t)
			d := &httpDialect{log: log}
			if err := d.OnHealthy([]byte(c.body)); err != nil {
				t.Fatalf("OnHealthy returned %v; health must never fail boot here", err)
			}
			if got := d.EngineVersion(); got != c.wantVersion {
				t.Errorf("EngineVersion() = %q, want %q", got, c.wantVersion)
			}
			var matched []slogRecord
			for _, r := range *recs {
				if r.Msg == msgExact || r.Msg == msgDrift || r.Msg == msgBelowMin {
					matched = append(matched, r)
				}
			}
			if c.wantMsg == "" {
				if len(matched) != 0 {
					t.Errorf("expected no version log, got %+v", matched)
				}
				return
			}
			if len(matched) != 1 {
				t.Fatalf("expected exactly one version log, got %d: %+v", len(matched), matched)
			}
			got := matched[0]
			if got.Msg != c.wantMsg {
				t.Errorf("logged %q, want %q", got.Msg, c.wantMsg)
			}
			if got.Level != c.wantLevel {
				t.Errorf("level = %v, want %v", got.Level, c.wantLevel)
			}
			if got.Attrs["version"] != c.wantVersion {
				t.Errorf("logged version = %q, want %q", got.Attrs["version"], c.wantVersion)
			}
			if c.wantMsg == msgDrift && got.Attrs["known_good"] != KnownGoodVersion {
				t.Errorf("drift warning must name the known-good release, got %q", got.Attrs["known_good"])
			}
		})
	}
}

// Drift is warned once per engine boot. httpagent calls OnHealthy once inside
// ensureServer's startup probe, so a restart onto a still-drifted engine warns
// again — an operator who restarts to fix something must see it is still off
// the pin.
func TestKnownGoodDriftWarnsOncePerBoot(t *testing.T) {
	log, recs := captureLogs(t)
	d := &httpDialect{log: log}
	body := []byte(`{"healthy":true,"version":"1.18.22"}`)

	if err := d.OnHealthy(body); err != nil {
		t.Fatal(err)
	}
	if n := countMsg(*recs, "opencode engine differs from the known-good release"); n != 1 {
		t.Fatalf("first boot logged %d drift warnings, want 1", n)
	}
	// A second boot of the same dialect must warn again.
	if err := d.OnHealthy(body); err != nil {
		t.Fatal(err)
	}
	if n := countMsg(*recs, "opencode engine differs from the known-good release"); n != 2 {
		t.Fatalf("second boot brought the total to %d, want 2", n)
	}
}

func countMsg(recs []slogRecord, msg string) int {
	n := 0
	for _, r := range recs {
		if r.Msg == msg {
			n++
		}
	}
	return n
}

// Only the below-minimum, session-tree-enabled case is a startup error. Drift
// must never take a working engine offline.
func TestVersionGateOnlyBlocksBelowMinimum(t *testing.T) {
	cases := []struct {
		version   string
		treeOn    bool
		wantError bool
	}{
		{KnownGoodVersion, true, false},
		{"1.18.22", true, false}, // drift, still starts
		{"1.19.0", true, false},  // drift, still starts
		{"1.18.0", true, false},  // exactly the floor
		{"1.17.9", true, true},   // below the floor with tree on
		{"1.17.9", false, false}, // below the floor, kill switch off
		{"nonsense", true, true}, // unparseable sorts below the floor
		{"nonsense", false, false},
	}
	for _, c := range cases {
		name := c.version + "/tree=" + strconv.FormatBool(c.treeOn)
		t.Run(name, func(t *testing.T) {
			d := &httpDialect{log: slog.Default()}
			if err := d.OnHealthy([]byte(`{"healthy":true,"version":"` + c.version + `"}`)); err != nil {
				t.Fatal(err)
			}
			cfg := httpagent.Config{}
			if !c.treeOn {
				off := false
				cfg.SessionTree = &off
			}
			err := d.CheckMinVersion(cfg)
			if c.wantError && err == nil {
				t.Fatalf("version %q with tree=%v must be refused", c.version, c.treeOn)
			}
			if !c.wantError && err != nil {
				t.Fatalf("version %q with tree=%v must start: %v", c.version, c.treeOn, err)
			}
		})
	}
}
