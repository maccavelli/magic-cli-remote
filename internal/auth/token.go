package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const tokenPrefix = "mcr_"

// GenerateToken returns a new opaque device token (plaintext).
func GenerateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return tokenPrefix + hex.EncodeToString(b[:]), nil
}

// HashToken returns the SHA-256 hex digest of the token for at-rest storage.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
