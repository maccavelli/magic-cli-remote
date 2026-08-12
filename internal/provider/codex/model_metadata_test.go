package codex

import (
	"strings"
	"testing"
)

func TestModelMetadataDecodesTiersAndPersonality(t *testing.T) {
	recs, err := decodeModelRecords(testdata147(t, "model-list-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("len = %d", len(recs))
	}
	sol, ok := lookupModel(recs, "gpt-5.6-sol")
	if !ok || sol.SupportsPersonality {
		t.Fatalf("sol = %+v", sol)
	}
	fast, ok := sol.fastTier()
	if !ok || fast.ID == "fast" || fast.Name != "Fast" {
		t.Fatalf("fast lookup must use display name, got %+v", fast)
	}
	if fast.ID != "priority" {
		t.Fatalf("opaque id = %q", fast.ID)
	}
	gpt55, ok := lookupModel(recs, "gpt-5.5")
	if !ok || !gpt55.SupportsPersonality {
		t.Fatalf("gpt-5.5 = %+v", gpt55)
	}
	if _, ok := gpt55.fastTier(); ok {
		t.Fatal("gpt-5.5 has no Fast tier")
	}
}

func TestModelMetadataRejectsEmptyAndDuplicateFast(t *testing.T) {
	if _, err := decodeModelRecords([]byte(`{"data":[{"id":"m","serviceTiers":[{"id":"","name":"Fast"}]}]}`)); err == nil {
		t.Fatal("empty tier id must fail")
	}
	if _, err := decodeModelRecords([]byte(`{"data":[{"id":"m","serviceTiers":[{"id":"a","name":"Fast"},{"id":"b","name":"Fast"}]}]}`)); err == nil {
		t.Fatal("duplicate Fast must fail")
	}
}

func TestResolveActiveModelOrder(t *testing.T) {
	if got := resolveActiveModel("sess", "cfg", "def"); got != "sess" {
		t.Fatalf("got %q", got)
	}
	if got := resolveActiveModel("", "cfg", "def"); got != "cfg" {
		t.Fatalf("got %q", got)
	}
	if got := resolveActiveModel("", "", "def"); got != "def" {
		t.Fatalf("got %q", got)
	}
	if got := resolveActiveModel("  ", "cfg", "def"); got != "cfg" {
		t.Fatalf("blank session model = %q", got)
	}
}

func TestNormalizeServiceTier(t *testing.T) {
	if normalizeServiceTier("default") != "" || normalizeServiceTier("DEFAULT") != "" || normalizeServiceTier("") != "" {
		t.Fatal("default/empty must normalize to off")
	}
	if normalizeServiceTier("priority") != "priority" {
		t.Fatal("opaque id must be kept")
	}
}

func TestModelMetadataIgnoresAdditiveFields(t *testing.T) {
	raw := testdata147(t, "model-list-metadata.json")
	if !strings.Contains(string(raw), "futureAdditiveField") {
		t.Fatal("fixture must include additive field")
	}
	if _, err := decodeModelRecords(raw); err != nil {
		t.Fatal(err)
	}
}
