package picker

import (
	"testing"
)

func TestValidateSelectionSingle(t *testing.T) {
	c := SingleCatalog(SourceStatic, []Option{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B", Enabled: WithEnabled(false)},
	}, "a", false)

	if err := c.ValidateSelection([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateSelection([]string{"a", "b"}); err == nil {
		t.Fatal("expected multi reject")
	}
	if err := c.ValidateSelection([]string{"b"}); err == nil {
		t.Fatal("expected disabled reject")
	}
	if err := c.ValidateSelection([]string{"nope"}); err == nil {
		t.Fatal("expected unknown reject")
	}
	c.AllowCustom = true
	if err := c.ValidateSelection([]string{"custom"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSelectionMulti(t *testing.T) {
	c := Catalog{
		Kind: KindMulti,
		Options: []Option{
			{ID: "a"}, {ID: "b"}, {ID: "c"},
		},
		MinSelect: 1,
		MaxSelect: 2,
	}.Normalize()
	if err := c.ValidateSelection(nil); err == nil {
		t.Fatal("expected min reject")
	}
	if err := c.ValidateSelection([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateSelection([]string{"a", "b", "c"}); err == nil {
		t.Fatal("expected max reject")
	}
	// Dedupe
	if err := c.ValidateSelection([]string{"a", "a"}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeLiveStatic(t *testing.T) {
	live := SingleCatalog(SourceLive, []Option{
		{ID: "opencode/x", Label: "X"},
	}, "opencode/x", true)
	static := SingleCatalog(SourceStatic, []Option{
		{ID: "opencode/x", Label: "X-static"},
		{ID: "opencode/y", Label: "Y"},
	}, "opencode/y", true)
	m := MergeLiveStatic(live, static)
	if m.Source != SourceMerged {
		t.Fatalf("source %s", m.Source)
	}
	if len(m.Options) != 2 {
		t.Fatalf("options %d", len(m.Options))
	}
	if m.Options[0].Label != "X" {
		t.Fatalf("live should win label: %s", m.Options[0].Label)
	}
	if m.DefaultIDs[0] != "opencode/x" {
		t.Fatalf("defaults %v", m.DefaultIDs)
	}
}
