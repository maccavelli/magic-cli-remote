package ws

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// TestCapCatalogOptions pins the frame-budget guard. The relay caps one message
// at 1 MiB; an uncapped OpenCode catalog was already using half of that, and
// the failure mode past the cap is a dropped connection, not a long list.
func TestCapCatalogOptions(t *testing.T) {
	opts := make([]picker.Option, maxCatalogOptions+100)
	for i := range opts {
		opts[i] = picker.Option{ID: fmt.Sprintf("m%04d", i)}
	}
	body := protocol.ModelsResultFromCatalog("stub",
		picker.SingleCatalog(picker.SourceLive, opts, "m0000", true))

	dropped := capCatalogOptions(&body)
	if dropped != 100 {
		t.Fatalf("dropped = %d, want 100", dropped)
	}
	if len(body.Options) != maxCatalogOptions {
		t.Fatalf("options = %d, want %d", len(body.Options), maxCatalogOptions)
	}
	if !body.Truncated {
		t.Fatal("truncation was silent")
	}
}

// TestCapCatalogOptionsLeavesSmallCatalogAlone: the flag must mean something,
// so it must not be set for a catalog that fits.
func TestCapCatalogOptionsLeavesSmallCatalogAlone(t *testing.T) {
	body := protocol.ModelsResultFromCatalog("stub", picker.SingleCatalog(
		picker.SourceLive, []picker.Option{{ID: "a"}, {ID: "b"}}, "a", true))
	if dropped := capCatalogOptions(&body); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if body.Truncated {
		t.Fatal("small catalog marked truncated")
	}
}

// TestCappedCatalogFitsTheFrame is the reason the cap number is what it is:
// a maximally-capped reply must stay well inside the 1 MiB relay message limit
// even with realistically long ids, labels and descriptions.
func TestCappedCatalogFitsTheFrame(t *testing.T) {
	opts := make([]picker.Option, maxCatalogOptions)
	for i := range opts {
		opts[i] = picker.Option{
			ID:          fmt.Sprintf("some-model-provider/some-fairly-long-model-identifier-%04d", i),
			Label:       fmt.Sprintf("Some Fairly Long Model Display Name %04d", i),
			Description: "1000K context",
			Group:       "some-model-provider",
			Meta: map[string]string{
				picker.MetaReleaseDate: "2026-07-16",
				picker.MetaStatus:      "active",
				picker.MetaContext:     "1000K",
			},
		}
	}
	body := protocol.ModelsResultFromCatalog("stub",
		picker.SingleCatalog(picker.SourceLive, opts, opts[0].ID, true))
	capCatalogOptions(&body)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	const relayFrameLimit = 1 << 20
	if len(b) > relayFrameLimit/2 {
		t.Fatalf("capped reply is %d bytes, over half the %d-byte relay frame limit",
			len(b), relayFrameLimit)
	}
	t.Logf("capped reply: %d bytes", len(b))
}

// A provider that capped its own list already dropped rows this layer will
// never see. Before MADR 0096 D3 that loss was invisible on the wire: kilo cut
// 716 of 866 models below the ws cap, so capCatalogOptions never fired and the
// reply went out claiming completeness.
func TestProviderTruncationSurvivesTheTransport(t *testing.T) {
	cat := picker.SingleCatalog(picker.SourceLive,
		[]picker.Option{{ID: "a"}, {ID: "b"}}, "a", true)
	cat.Truncated = true

	body := protocol.ModelsResultFromCatalog("kilo", cat)
	if !body.Truncated {
		t.Fatal("provider-side truncation was dropped building the reply")
	}
	if dropped := capCatalogOptions(&body); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if !body.Truncated {
		t.Fatal("the transport cleared a truncation flag it did not set")
	}
}

// commonOptionGroup names the scope a session-scoped catalog actually covers,
// which the reply echoes so the client can label the picker. A catalog
// spanning vendors names none — guessing one would mislabel it.
func TestCommonOptionGroup(t *testing.T) {
	one := []picker.Option{{Group: "kilo"}, {Group: "kilo"}}
	if got := commonOptionGroup(one); got != "kilo" {
		t.Errorf("single-group catalog = %q, want kilo", got)
	}
	mixed := []picker.Option{{Group: "kilo"}, {Group: "openrouter"}}
	if got := commonOptionGroup(mixed); got != "" {
		t.Errorf("mixed catalog claimed scope %q", got)
	}
	if got := commonOptionGroup(nil); got != "" {
		t.Errorf("empty catalog claimed scope %q", got)
	}
}
