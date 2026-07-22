package auth_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
)

func TestPairCodeCreateClaim(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Create("phone", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Code) != 8 {
		t.Fatalf("code len=%d", len(info.Code))
	}
	if info.Display != auth.FormatPairCode(info.Code) {
		t.Fatalf("display=%q", info.Display)
	}

	// Accept hyphenated / lowercase.
	name, err := store.Claim(info.Display)
	if err != nil {
		t.Fatal(err)
	}
	if name != "phone" {
		t.Fatalf("name=%q", name)
	}

	// One-shot.
	if _, err := store.Claim(info.Code); err != auth.ErrInvalidPairCode {
		t.Fatalf("want invalid after claim, got %v", err)
	}
}

func TestPairCodeExpire(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Create("x", 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_, err = store.Claim(info.Code)
	if err != auth.ErrExpiredPairCode && err != auth.ErrInvalidPairCode {
		// After purge, expired may appear as invalid; both acceptable.
		t.Fatalf("want expired/invalid, got %v", err)
	}
}

func TestNormalizePairCode(t *testing.T) {
	if got := auth.NormalizePairCode("ab12-cd34"); got != "AB12CD34" {
		t.Fatalf("got %q", got)
	}
	if got := auth.FormatPairCode("AB12CD34"); got != "AB12-CD34" {
		t.Fatalf("format %q", got)
	}
}

func TestPairCodeWrong(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("22222222"); err != auth.ErrInvalidPairCode {
		t.Fatalf("got %v", err)
	}
}

// Take + Restore must leave the code claimable after a failed device create
// (Phase 3.2).
func TestPairCodeTakeRestore(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Create("phone", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := store.Take(info.Code)
	if err != nil {
		t.Fatal(err)
	}
	if taken.Name != "phone" {
		t.Fatalf("name=%q", taken.Name)
	}
	// Burned until restored.
	if _, err := store.Claim(info.Code); err != auth.ErrInvalidPairCode {
		t.Fatalf("want invalid while taken, got %v", err)
	}
	if err := store.Restore(taken); err != nil {
		t.Fatal(err)
	}
	name, err := store.Claim(info.Code)
	if err != nil {
		t.Fatalf("claim after restore: %v", err)
	}
	if name != "phone" {
		t.Fatalf("name=%q", name)
	}
}
