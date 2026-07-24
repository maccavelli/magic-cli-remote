package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

func TestOwnerIsolationListAndPrompt(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	ctx := context.Background()

	metaA, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "a"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if metaA.OwnerDeviceID != "device-a" {
		t.Fatalf("owner=%q", metaA.OwnerDeviceID)
	}
	metaB, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "b"}, "device-b")
	if err != nil {
		t.Fatal(err)
	}

	listA := mgr.ListFor("device-a")
	if len(listA) != 1 || listA[0].ID != metaA.ID {
		t.Fatalf("listA=%+v", listA)
	}
	listB := mgr.ListFor("device-b")
	if len(listB) != 1 || listB[0].ID != metaB.ID {
		t.Fatalf("listB=%+v", listB)
	}

	if err := mgr.Prompt(ctx, metaA.ID, "x", nil, "device-b"); !errors.Is(err, session.ErrForbidden) {
		t.Fatalf("device-b prompt on A: %v", err)
	}
	if err := mgr.Prompt(ctx, metaA.ID, "x", nil, "device-a"); err != nil {
		t.Fatalf("owner prompt: %v", err)
	}
}

func TestLegacyEmptyOwnerClaim(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "legacy"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Visible to everyone while unowned.
	if n := len(mgr.ListFor("any")); n != 1 {
		t.Fatalf("list any = %d", n)
	}
	// First mutator claims.
	if err := mgr.Prompt(ctx, meta.ID, "hi", nil, "claimer"); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerDeviceID != "claimer" {
		t.Fatalf("owner after claim = %q", got.OwnerDeviceID)
	}
	if err := mgr.Prompt(ctx, meta.ID, "no", nil, "other"); !errors.Is(err, session.ErrForbidden) {
		t.Fatalf("want forbidden after claim, got %v", err)
	}
}
