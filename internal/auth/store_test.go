package auth_test

import (
	"path/filepath"
	"testing"

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
