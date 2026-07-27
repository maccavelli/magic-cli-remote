package codex

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// silentLogger returns a logger that drops output, so test runs are quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubLister implements modelLister for tests. Set err to non-nil to make
// ListModels return that error.
type stubLister struct {
	catalog picker.Catalog
	err     error
}

func (s *stubLister) ListModels(ctx context.Context) (picker.Catalog, error) {
	if s.err != nil {
		return picker.Catalog{}, s.err
	}
	return s.catalog, nil
}

// TestSetModelRejectsEmpty verifies the empty-name guard. SetModel delegates
// to validateModelName after trimming, so the empty case is on the
// SetModel path itself, not on the catalog.
func TestSetModelRejectsEmpty(t *testing.T) {
	s := &session{opts: provider.StartOptions{}}
	if err := s.SetModel(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty model name")
	}
	if err := s.SetModel(context.Background(), "   "); err == nil {
		t.Fatal("expected error for whitespace-only model name")
	}
}

// TestSetModelValidatesAgainstCatalog pins the validation contract: a name
// not in the live model catalog must be rejected, and the change must not
// have been applied. The catalog path is exercised through validateModelName
// directly (SetModel would need a real engine, which this test does not run).
func TestSetModelValidatesAgainstCatalog(t *testing.T) {
	lister := &stubLister{
		catalog: picker.SingleCatalog(
			picker.SourceLive,
			[]picker.Option{{ID: "gpt-5.6-sol"}, {ID: "gpt-5.6-terra"}},
			"gpt-5.6-sol", false,
		),
	}
	if err := validateModelName(context.Background(), lister, "gpt-5.6-terra", silentLogger()); err != nil {
		t.Fatalf("expected gpt-5.6-terra to validate: %v", err)
	}
	if err := validateModelName(context.Background(), lister, "gpt-bogus", silentLogger()); err == nil {
		t.Fatal("expected gpt-bogus to fail validation")
	}
}

// TestSetModelFailsOpenOnListError verifies that a model/list RPC failure
// (e.g. engine hiccup) does not block a legitimate switch: fail-open.
func TestSetModelFailsOpenOnListError(t *testing.T) {
	lister := &stubLister{err: &stubErr{"stub list error"}}
	if err := validateModelName(context.Background(), lister, "anything", silentLogger()); err != nil {
		t.Fatalf("expected fail-open on list error, got: %v", err)
	}
}

// TestCommandTableRoutesModelToInPlaceOp pins that the table routes /model
// through KindOp + OpSetModel so cmdModel(..., inPlace=true) is used and the
// thread survives (MADR 0035 D3 / phase 1).
func TestCommandTableRoutesModelToInPlaceOp(t *testing.T) {
	specs := command.Specs
	found := false
	for _, s := range specs {
		if s.Name == "model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("model spec missing from canonical vocabulary")
	}
	m, ok := commandTable["model"]
	if !ok {
		t.Fatal("codex table missing model")
	}
	if m.Kind != command.KindOp || m.Op != command.OpSetModel {
		t.Errorf("model mapped to kind=%s op=%s, want kind=op op=set_model", m.Kind, m.Op)
	}
}

// stubErr is a tiny error type for tests.
type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }
