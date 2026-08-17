package httpagent

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The session must satisfy the interface the daemon's session-scoped
// models.list looks for. It did not before MADR 0096 D1, which is the whole
// mechanism of the bug: Manager.ModelCatalog returned "session does not report
// a model catalog", the ws handler swallowed that at Debug, and the phone's
// /model picker silently got the provider-wide default set instead of the
// session's own vendor.
func TestSessionReportsAModelCatalog(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var s provider.Session = &session{p: p}
	if _, ok := s.(provider.ModelCatalogSession); !ok {
		t.Fatal("httpagent session does not implement provider.ModelCatalogSession")
	}
}

// modelProvider resolves the vendor to scope the picker to, in priority order.
// The engine round trip is only the last resort, so the first two cases must
// not need one — these sessions have no live engine at all.
func TestSessionModelProviderResolution(t *testing.T) {
	for _, tc := range []struct {
		name         string
		sessionModel string
		configModel  string
		want         string
	}{
		{"session model wins", "kilo/kilo-auto/frontier", "openrouter/x", "kilo"},
		{"config model when the session has none", "", "kilo/kilo-auto/balanced", "kilo"},
		// Kilo model ids contain slashes; only the first separates the vendor.
		{"splits on the first slash only", "kilo/anthropic/claude-sonnet-5", "", "kilo"},
		{"bare model name is not a vendor", "some-model", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewWithLogger(&fakeDialect{id: "test"},
				Config{Bin: "false", Model: tc.configModel}, nil)
			s := &session{p: p, model: tc.sessionModel}
			// fakeDialect is not a ModelProviderLister, so the last-resort
			// branch resolves to "" rather than booting anything.
			if got := s.modelProvider(context.Background()); got != tc.want {
				t.Fatalf("modelProvider = %q, want %q", got, tc.want)
			}
		})
	}
}

// A dialect with one implicit model provider has no vendor step to scope to,
// so its default catalog already *is* the session's catalog. Answering it is
// the correct scoping, not a fallback — and it must not error, or the picker
// loses the free-text fallback the daemon depends on.
func TestSessionCatalogWithoutModelProviders(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false", Model: "m"}, nil)
	s := &session{p: p}
	cat, err := s.ModelCatalog(context.Background(), provider.CatalogScopeModels)
	if err != nil {
		t.Fatalf("ModelCatalog: %v", err)
	}
	if !cat.AllowCustom {
		t.Error("catalog refuses free text; a known model id would be unusable")
	}
}

// The providers scope is only answerable by a dialect that enumerates model
// providers. Erroring lets the ws handler fall through to the provider-wide
// answer, which is right; a silently empty catalog would make the client hide
// its provider step for the wrong reason.
func TestSessionProvidersScopeNeedsALister(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	s := &session{p: p}
	if _, err := s.ModelCatalog(context.Background(), provider.CatalogScopeProviders); err == nil {
		t.Fatal("expected an error for a dialect that lists no model providers")
	}
}
