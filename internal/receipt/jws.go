// Package receipt implements signed receipts for permission decisions
// (MADR 0077): JWS ES256 signing/verification, the in-toto-style Statement
// shape, and append-only per-device chained storage.
package receipt

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// jwsHeader is the fixed ES256 JWS Protected Header this package produces and
// accepts. No "kid": device/daemon identity is established out-of-band via
// the enrolled-cert lookup (MADR 0077 D9), not a JWS header field, which
// would be one more place to spoof.
const jwsHeader = `{"alg":"ES256"}`

// jwsHeaderB64 is the fixed base64url encoding of jwsHeader, computed once.
var jwsHeaderB64 = base64.RawURLEncoding.EncodeToString([]byte(jwsHeader))

// ErrMalformedJWS is returned by VerifyES256Compact when the input is not a
// well-formed three-part compact serialization.
var ErrMalformedJWS = errors.New("malformed JWS compact serialization")

// ErrWrongAlg is returned by VerifyES256Compact when the protected header is
// not exactly {"alg":"ES256"} — no algorithm negotiation, a wrong or foreign
// alg is rejected outright rather than downgraded to weaker validation.
var ErrWrongAlg = errors.New("JWS header is not exactly {\"alg\":\"ES256\"}")

// ErrSignatureInvalid is returned by VerifyES256Compact when the signature
// does not verify against the given public key.
var ErrSignatureInvalid = errors.New("JWS signature invalid")

// SignES256Compact signs payload as a JWS in Compact Serialization (RFC 7515
// §3.1) using ES256 (RFC 7518 §3.4): the ECDSA P-256 signature is the raw
// 64-byte R||S concatenation, big-endian, 32 bytes each — not ASN.1 DER.
func SignES256Compact(priv *ecdsa.PrivateKey, payload []byte) (string, error) {
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := jwsHeaderB64 + "." + payloadB64
	sum := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return signingInput + "." + sigB64, nil
}

// VerifyES256Compact verifies compact against pub and, on success, returns
// the decoded payload bytes. Rejects anything that is not exactly a
// three-part compact serialization with an {"alg":"ES256"} header.
func VerifyES256Compact(pub *ecdsa.PublicKey, compact string) ([]byte, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, ErrMalformedJWS
	}
	headerB, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header: %v", ErrMalformedJWS, err)
	}
	if string(headerB) != jwsHeader {
		return nil, ErrWrongAlg
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload: %v", ErrMalformedJWS, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature: %v", ErrMalformedJWS, err)
	}
	if len(sig) != 64 {
		return nil, fmt.Errorf("%w: signature is %d bytes, want 64", ErrMalformedJWS, len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])

	signingInput := parts[0] + "." + parts[1]
	sum := sha256.Sum256([]byte(signingInput))
	if !ecdsa.Verify(pub, sum[:], r, s) {
		return nil, ErrSignatureInvalid
	}
	return payload, nil
}

// DecodePayloadUnverified returns compact's payload bytes without checking
// its signature. For display/inspection only (the CLI's `receipts show` on
// an entry whose signer key is unavailable, or a quick summary while
// listing) — never use this result for a trust decision; call
// VerifyES256Compact for that.
func DecodePayloadUnverified(compact string) ([]byte, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, ErrMalformedJWS
	}
	headerB, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header: %v", ErrMalformedJWS, err)
	}
	if string(headerB) != jwsHeader {
		return nil, ErrWrongAlg
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload: %v", ErrMalformedJWS, err)
	}
	return payload, nil
}
