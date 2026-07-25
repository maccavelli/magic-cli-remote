package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateTokenHasPrefix(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, tokenPrefix) {
		t.Fatalf("token = %q, want %q prefix", tok, tokenPrefix)
	}
}

func TestGenerateTokenLength(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	// "mcr_" + 64 hex chars (32 bytes)
	if len(tok) != len(tokenPrefix)+64 {
		t.Fatalf("token length = %d, want %d", len(tok), len(tokenPrefix)+64)
	}
}

func TestGenerateTokenRandom(t *testing.T) {
	t1, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	t2, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if t1 == t2 {
		t.Fatal("two generated tokens must differ")
	}
}

func TestGenerateTokenHexChars(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	hexPart := tok[len(tokenPrefix):]
	for _, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex character %c in token %q", c, tok)
		}
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	h1 := HashToken(tok)
	h2 := HashToken(tok)
	if h1 != h2 {
		t.Fatal("HashToken must be deterministic for the same input")
	}
}

func TestHashTokenNotEmpty(t *testing.T) {
	h := HashToken("anything")
	if h == "" {
		t.Fatal("HashToken must not return empty string")
	}
}

func TestHashTokenLength(t *testing.T) {
	h := HashToken("test")
	if len(h) != 64 {
		t.Fatalf("HashToken length = %d, want 64 (SHA-256 hex)", len(h))
	}
}

func TestHashTokenKnownValue(t *testing.T) {
	input := "mcr_test"
	sum := sha256.Sum256([]byte(input))
	want := hex.EncodeToString(sum[:])
	got := HashToken(input)
	if got != want {
		t.Fatalf("HashToken(%q) = %q, want %q", input, got, want)
	}
}

func TestGenerateTokenRoundTrip(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	h := HashToken(tok)
	if len(h) != 64 {
		t.Fatal("hash must be 64 hex chars")
	}
}
