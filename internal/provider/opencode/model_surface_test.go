package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// readSurfaceFixture reads one file from the committed 1.18.21 evidence corpus.
// surface_contract_test.go has its own reader, but it lives in the external
// opencode_test package and this file needs unexported decoder access.
func readSurfaceFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata/surface-1.18.21", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

// ids flattens rung IDs for comparison.
func ids(levels []picker.ThinkingLevel) []string {
	out := make([]string, 0, len(levels))
	for _, l := range levels {
		out = append(out, l.ID)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDecodeVariantsMatrix pins every shape the optional 1.18.21
// `Model.variants` record can take. A rung must come from an advertised key and
// nowhere else (MADR 0112 A14).
func TestDecodeVariantsMatrix(t *testing.T) {
	obj := json.RawMessage(`{"reasoningEffort":"high"}`)
	for _, tc := range []struct {
		name string
		in   map[string]json.RawMessage
		want []string
	}{
		{"absent", nil, nil},
		{"empty", map[string]json.RawMessage{}, nil},
		{"single", map[string]json.RawMessage{"high": obj}, []string{"high"}},
		{
			"many are ordered cheapest-first regardless of map order",
			map[string]json.RawMessage{"xhigh": obj, "low": obj, "medium": obj, "minimal": obj, "high": obj},
			[]string{"minimal", "low", "medium", "high", "xhigh"},
		},
		{
			"unknown rung names are kept after every known one",
			map[string]json.RawMessage{"high": obj, "turbo": obj},
			[]string{"high", "turbo"},
		},
		{
			"non-object values are dropped, not promoted",
			map[string]json.RawMessage{
				"high":   obj,
				"astr":   json.RawMessage(`"high"`),
				"anum":   json.RawMessage(`3`),
				"anull":  json.RawMessage(`null`),
				"anarr":  json.RawMessage(`[]`),
				"abool":  json.RawMessage(`true`),
				"aempty": json.RawMessage(``),
			},
			[]string{"high"},
		},
		{
			"keys are trimmed and duplicates-after-trim collapse to the first",
			map[string]json.RawMessage{"  high  ": obj, "high": obj},
			[]string{"high"},
		},
		{"empty-after-trim keys are dropped", map[string]json.RawMessage{"   ": obj}, nil},
		{"every value malformed yields no rungs", map[string]json.RawMessage{"a": json.RawMessage(`1`)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(decodeVariants(tc.in))
			if !eq(got, tc.want) {
				t.Fatalf("decodeVariants = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDecodeVariantsNeverInventsDisplayText proves a rung carries only the
// upstream key: the variants schema has no label, prose or default marker, so
// synthesising one would be a daemon-invented ladder (MADR 0052 §1).
func TestDecodeVariantsNeverInventsDisplayText(t *testing.T) {
	got := decodeVariants(map[string]json.RawMessage{"high": json.RawMessage(`{"reasoningEffort":"high"}`)})
	if len(got) != 1 {
		t.Fatalf("want 1 rung, got %d", len(got))
	}
	if got[0].Label != "" || got[0].Description != "" || got[0].Default {
		t.Fatalf("rung invented display text or a default: %+v", got[0])
	}
}

// TestDecodeVariantsIsDeterministic proves repeated decodes of the same catalog
// produce the same order despite Go's randomised map iteration.
func TestDecodeVariantsIsDeterministic(t *testing.T) {
	obj := json.RawMessage(`{}`)
	in := map[string]json.RawMessage{"zeta": obj, "alpha": obj, "beta": obj, "gamma": obj}
	first := ids(decodeVariants(in))
	for i := 0; i < 50; i++ {
		if got := ids(decodeVariants(in)); !eq(got, first) {
			t.Fatalf("iteration %d = %v, want %v", i, got, first)
		}
	}
}

// TestModelSurfaceDecodesCapabilities proves the advertised input block reaches
// the picker row and that an absent block stays unknown rather than false.
func TestModelSurfaceDecodesCapabilities(t *testing.T) {
	var withCaps providerModel
	if err := json.Unmarshal([]byte(`{
		"id":"m","capabilities":{"reasoning":true,"attachment":true,"toolcall":true,
		"input":{"text":true,"image":true,"audio":true,"video":false,"pdf":false}},
		"variants":{"high":{}}}`), &withCaps); err != nil {
		t.Fatal(err)
	}
	s := withCaps.surface()
	if !s.Attachment || !s.ToolCall || !s.Reasoning {
		t.Fatalf("flags lost: %+v", s)
	}
	if !s.Inputs.Text || !s.Inputs.Image || !s.Inputs.Audio || s.Inputs.Video || s.Inputs.PDF {
		t.Fatalf("input modalities wrong: %+v", s.Inputs)
	}
	opt := s.apply(picker.Option{ID: "opencode/m"})
	if opt.Inputs == nil || !opt.Inputs.Image {
		t.Fatalf("option lost inputs: %+v", opt.Inputs)
	}
	if !eq(ids(opt.ThinkingLevels), []string{"high"}) {
		t.Fatalf("option lost rungs: %v", ids(opt.ThinkingLevels))
	}

	var noCaps providerModel
	if err := json.Unmarshal([]byte(`{"id":"m"}`), &noCaps); err != nil {
		t.Fatal(err)
	}
	bare := noCaps.surface().apply(picker.Option{ID: "opencode/m"})
	if bare.Inputs != nil {
		t.Fatalf("absent capabilities must stay unknown, got %+v", bare.Inputs)
	}
	if bare.Attachment || bare.ToolCall || bare.Reasoning || bare.ThinkingLevels != nil {
		t.Fatalf("absent capabilities invented a surface: %+v", bare)
	}
}

// TestModelSurfaceTextOnlyIsNotUnknown proves an all-false-but-text block is
// carried: "accepts only text" and "did not say" must stay distinguishable.
func TestModelSurfaceTextOnlyIsNotUnknown(t *testing.T) {
	var m providerModel
	if err := json.Unmarshal([]byte(`{"id":"big-pickle","capabilities":{"attachment":false,
		"input":{"text":true,"image":false,"audio":false,"video":false,"pdf":false}},
		"variants":{}}`), &m); err != nil {
		t.Fatal(err)
	}
	opt := m.surface().apply(picker.Option{ID: "opencode/big-pickle"})
	if opt.Inputs == nil {
		t.Fatal("text-only model reported an unknown input surface")
	}
	if !opt.Inputs.Text || opt.Inputs.Image || opt.Inputs.Audio {
		t.Fatalf("text-only inputs wrong: %+v", opt.Inputs)
	}
	if opt.Attachment {
		t.Fatal("big-pickle must not advertise attachments")
	}
	if len(opt.ThinkingLevels) != 0 {
		t.Fatalf("empty variants must yield no rungs, got %v", ids(opt.ThinkingLevels))
	}
}

// TestModelSurfaceCacheReplaceAndLookup pins the cache contract, including the
// rule that an empty refresh never erases known surfaces.
func TestModelSurfaceCacheReplaceAndLookup(t *testing.T) {
	var c modelSurfaceCache
	if _, ok := c.lookup("opencode/m"); ok {
		t.Fatal("empty cache returned a surface")
	}
	c.replace(map[string]modelSurface{"opencode/m": {Attachment: true}})
	got, ok := c.lookup("opencode/m")
	if !ok || !got.Attachment {
		t.Fatalf("lookup after replace = %+v, %v", got, ok)
	}
	if c.len() != 1 {
		t.Fatalf("len = %d, want 1", c.len())
	}
	c.replace(nil)
	if _, ok := c.lookup("opencode/m"); !ok {
		t.Fatal("an empty refresh erased a known surface")
	}
	c.replace(map[string]modelSurface{"opencode/other": {ToolCall: true}})
	if _, ok := c.lookup("opencode/m"); ok {
		t.Fatal("replace must swap the whole cache, not merge")
	}
}

// TestModelSurfaceCacheConcurrent proves concurrent refresh and lookup are safe;
// AfterBoot writes while live sessions read.
func TestModelSurfaceCacheConcurrent(t *testing.T) {
	var c modelSurfaceCache
	c.replace(map[string]modelSurface{"opencode/m": {Attachment: true}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			c.replace(map[string]modelSurface{"opencode/m": {Attachment: true}})
		}
	}()
	for i := 0; i < 500; i++ {
		_, _ = c.lookup("opencode/m")
		_ = c.len()
	}
	<-done
}

// TestFixtureModelSurfacesDecode pins the committed 1.18.21 evidence corpus:
// the shapes this decoder must handle are the ones actually observed.
func TestFixtureModelSurfacesDecode(t *testing.T) {
	var fixture struct {
		Models map[string]providerModel `json:"models"`
	}
	readSurfaceFixture(t, "model-surface.json", &fixture)
	if len(fixture.Models) == 0 {
		t.Fatal("fixture carries no models")
	}
	big, ok := fixture.Models["opencode/big-pickle"]
	if !ok {
		t.Fatal("fixture lost the seeded default model")
	}
	bigSurface := big.surface()
	if bigSurface.Attachment || len(bigSurface.Levels) != 0 {
		t.Fatalf("big-pickle must advertise no attachments and no rungs: %+v", bigSurface)
	}
	if !bigSurface.Inputs.Text || bigSurface.Inputs.Image {
		t.Fatalf("big-pickle input surface wrong: %+v", bigSurface.Inputs)
	}
	muse, ok := fixture.Models["opencode/muse-spark-1.2-contributor-free"]
	if !ok {
		t.Skip("fixture has no multimodal model to pin")
	}
	museSurface := muse.surface()
	if !museSurface.Attachment || !museSurface.Inputs.Image || !museSurface.Inputs.Audio {
		t.Fatalf("multimodal model surface wrong: %+v", museSurface)
	}
	if len(museSurface.Levels) == 0 {
		t.Fatal("multimodal fixture model advertised no rungs")
	}
}
