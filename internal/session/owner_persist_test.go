package session_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// MADR 0056 Phase 0 / H-4: create and first-touch ownership claim must not
// succeed when durable Store.Save fails. Today writePersist / claim swallow
// Save errors (fail-open). These tests assert fail-closed and fail on main
// until Phase 1.

func TestCreateFailsWhenOwnerPersistFails(t *testing.T) {
	dir := t.TempDir()
	store, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// sessions/ is created by OpenStore; freeze it so Save cannot create meta.
	sessionsRoot := filepath.Join(dir, "sessions")
	if err := os.Chmod(sessionsRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessionsRoot, 0o700) })

	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, store, nil, nil)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "x"}, "device-a")
	if err == nil {
		// Clean up any live process if create incorrectly succeeded.
		_ = mgr.Close(ctx, meta.ID, "device-a")
		t.Fatal("MADR 0056 H-4: Create must return an error when owner persist fails; got success")
	}
}

func TestDiskClaimFailsWhenOwnerPersistFails(t *testing.T) {
	dir := t.TempDir()
	store, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Unowned durable row (legacy).
	if err := store.Save(session.Record{
		ID:       "legacy-1",
		Provider: provider.IDFake,
		Name:     "legacy",
		Status:   "disconnected",
	}); err != nil {
		t.Fatal(err)
	}
	// Freeze the session dir so the claim Save fails.
	sessDir := filepath.Join(dir, "sessions", "legacy-1")
	if err := os.Chmod(sessDir, 0o555); err != nil {
		t.Fatal(err)
	}
	// Also freeze parent so rewrite of meta.json fails even if mode differs.
	sessionsRoot := filepath.Join(dir, "sessions")
	if err := os.Chmod(sessionsRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sessionsRoot, 0o700)
		_ = os.Chmod(sessDir, 0o700)
	})

	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, store, nil, nil)

	err = mgr.Authorize("legacy-1", "claimer", true)
	if err == nil {
		t.Fatal("MADR 0056 H-4: Authorize claim must return an error when Save fails; got nil")
	}
	// Ownership must not stick in-memory/on-disk after a failed claim.
	if rec, getErr := store.Get("legacy-1"); getErr == nil && rec.OwnerDeviceID == "claimer" {
		t.Fatalf("failed claim must not persist owner; got %+v", rec)
	}
}

func TestLiveClaimFailsWhenOwnerPersistFails(t *testing.T) {
	dir := t.TempDir()
	store, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, store, nil, nil)
	ctx := context.Background()

	// Create with empty owner (legacy live) so first claim stamps owner.
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "live-legacy"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(ctx, meta.ID, "claimer") })

	sessionsRoot := filepath.Join(dir, "sessions")
	if err := os.Chmod(sessionsRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	// Session subdir may already exist from create persist; freeze it too.
	sessDir := filepath.Join(sessionsRoot, meta.ID)
	_ = os.Chmod(sessDir, 0o555)
	t.Cleanup(func() {
		_ = os.Chmod(sessionsRoot, 0o700)
		_ = os.Chmod(sessDir, 0o700)
	})

	err = mgr.Authorize(meta.ID, "claimer", true)
	if err == nil {
		t.Fatal("MADR 0056 H-4: live claim must return an error when persist fails; got nil")
	}
	got, gerr := mgr.Get(meta.ID)
	if gerr != nil {
		t.Fatal(gerr)
	}
	if got.OwnerDeviceID == "claimer" {
		t.Fatalf("failed live claim must not leave in-memory owner stamped: %+v", got)
	}
}
