package auth_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
)

func TestStoreCreateValidateRevoke(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}

	dev, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || dev.ID == "" {
		t.Fatalf("empty token or id")
	}
	if got, err := store.Validate(token); err != nil || got.ID != dev.ID {
		t.Fatalf("validate: got=%+v err=%v", got, err)
	}
	if _, err := store.Validate("mcr_invalid"); err == nil {
		t.Fatal("expected invalid token error")
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	if list[0].Name != "phone" {
		t.Fatalf("name=%q", list[0].Name)
	}

	if _, err := store.Revoke(dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(token); err == nil {
		t.Fatal("expected invalid after revoke")
	}
}

func TestHashStable(t *testing.T) {
	a := auth.HashToken("mcr_abc")
	b := auth.HashToken("mcr_abc")
	if a != b || a == "" {
		t.Fatalf("hash mismatch %s %s", a, b)
	}
}

func TestStorePruneKeyless(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	// One keyless (legacy) device, one with an enrolled client key.
	if _, _, err := store.Create("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateWithClientKey("phone", "somefp"); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Prune(time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Name != "legacy" {
		t.Fatalf("expected only the keyless device pruned, got %v", removed)
	}
	left, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name != "phone" {
		t.Fatalf("keyed device must survive, got %v", left)
	}
}

func TestValidateDebouncesLastUsedWrites(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	afterCreate := store.SaveCount()
	for i := 0; i < 50; i++ {
		if _, err := store.Validate(token); err != nil {
			t.Fatal(err)
		}
	}
	// LastUsedAt updates must not rewrite the file on every Validate.
	if got := store.SaveCount() - afterCreate; got != 0 {
		t.Fatalf("Validate caused %d extra saves, want 0 (debounced)", got)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	if store.SaveCount()-afterCreate != 1 {
		t.Fatalf("Flush should write once, saves since create=%d", store.SaveCount()-afterCreate)
	}
}

func TestStorePruneStale(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("used")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create("never-used"); err != nil {
		t.Fatal(err)
	}
	// Mark "used" as recently used.
	if _, err := store.Validate(token); err != nil {
		t.Fatal(err)
	}
	// Prune anything not used in the last hour: only "never-used" qualifies
	// (LastUsedAt nil), "used" was just validated.
	removed, err := store.Prune(time.Now().UTC().Add(-time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Name != "never-used" {
		t.Fatalf("expected only never-used pruned, got %v", removed)
	}
}
