package receipt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
)

// RFC 7515 Appendix A.3's published ES256 example: the P-256 key (x, y, d)
// and the exact compact serialization the appendix walks through computing.
// Independently re-verified for this plan against the RFC's own signing
// input using Python's `cryptography` library before being hardcoded here —
// see MADR 0077 PLAN P2's grounding notes. This is the correctness bar D7
// sets: reproduce the RFC's own worked example before writing a single real
// receipt.
const (
	rfcXHex = "7fcdce2770f6c45d4183cbee6fdb4b7b580733357be9ef13bacf6e3c7bd15445"
	rfcYHex = "c7f144cd1bbd9b7e872cdfedb9eeb9f4b3695d6ea90b24ad8a4623288588e5ad"
	rfcDHex = "8e9b109e719098bf980487df1f5d77e9cb29606ebed2263b5f57c213df84f4b2"

	rfcCompact = "eyJhbGciOiJFUzI1NiJ9" +
		".eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ" +
		".DtEhU3ljbEg8L38VWAfUAqOyKAM6-Xx-F4GawxaepmXFCgfTjDxw5djxLa8ISlSApmWQxfKTUJqPP3-Kg6NU1Q"
	rfcPayload = "{\"iss\":\"joe\",\r\n \"exp\":1300819380,\r\n \"http://example.com/is_root\":true}"
)

func rfcPublicKey(t *testing.T) *ecdsa.PublicKey {
	t.Helper()
	xb, err := hex.DecodeString(rfcXHex)
	if err != nil {
		t.Fatal(err)
	}
	yb, err := hex.DecodeString(rfcYHex)
	if err != nil {
		t.Fatal(err)
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}
}

func rfcPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	db, err := hex.DecodeString(rfcDHex)
	if err != nil {
		t.Fatal(err)
	}
	pub := rfcPublicKey(t)
	return &ecdsa.PrivateKey{
		PublicKey: *pub,
		D:         new(big.Int).SetBytes(db),
	}
}

func TestES256RFC7515Vector(t *testing.T) {
	pub := rfcPublicKey(t)
	payload, err := VerifyES256Compact(pub, rfcCompact)
	if err != nil {
		t.Fatalf("VerifyES256Compact on the RFC's own published signature: %v", err)
	}
	if string(payload) != rfcPayload {
		t.Fatalf("decoded payload = %q, want %q", payload, rfcPayload)
	}
}

func TestES256RoundTrip(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"hello":"world"}`)
	compact, err := SignES256Compact(priv, payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyES256Compact(&priv.PublicKey, compact)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

// dartProducedCompact was signed by apps/mobile/lib/data/ws/jws.dart's
// signEs256Compact using the RFC 7515 A.3 private key (see rfcDHex above)
// over the payload {"interop":"dart-to-go"} — captured once by running the
// Dart signer, not re-derivable at test time since ECDSA signing is
// randomized. Proves the Go verifier accepts a real Dart-produced signature,
// not just ones Go itself produced (MADR 0077 PLAN P2's cross-platform
// interop test).
const dartProducedCompact = "eyJhbGciOiJFUzI1NiJ9" +
	".eyJpbnRlcm9wIjoiZGFydC10by1nbyJ9" +
	".08H1EAc786po7gwyuvbEwgaG220dKPhDWJKo8LaWGACmpAsnj1fiV3QRMvX89vqxawLwYoL9uHlMpNoxjLOvEw"

func TestES256AcceptsDartProducedSignature(t *testing.T) {
	priv := rfcPrivateKey(t)
	payload, err := VerifyES256Compact(&priv.PublicKey, dartProducedCompact)
	if err != nil {
		t.Fatalf("VerifyES256Compact on a Dart-produced signature: %v", err)
	}
	if string(payload) != `{"interop":"dart-to-go"}` {
		t.Fatalf("payload = %q", payload)
	}
}

func TestES256RoundTripWithRFCKey(t *testing.T) {
	priv := rfcPrivateKey(t)
	payload := []byte(`{"a":1}`)
	compact, err := SignES256Compact(priv, payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyES256Compact(&priv.PublicKey, compact)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

// flipPart decodes compact's dot-separated part i, flips every bit of its
// first byte (guaranteed to change the decoded value, unlike overwriting a
// trailing base64 character — the last character of an unpadded base64url
// group can carry as few as 2 real bits, so a fixed replacement char has a
// real chance of round-tripping to the same byte and silently turning a
// tamper test into a no-op), and re-encodes.
func flipPart(t *testing.T, compact string, i int) string {
	t.Helper()
	parts := strings.Split(compact, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[i])
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xFF
	parts[i] = base64.RawURLEncoding.EncodeToString(raw)
	return strings.Join(parts, ".")
}

func TestES256TamperedPayload(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := SignES256Compact(priv, []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	tampered := flipPart(t, compact, 1)
	if _, err := VerifyES256Compact(&priv.PublicKey, tampered); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestES256TamperedSignature(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := SignES256Compact(priv, []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	tampered := flipPart(t, compact, 2)
	if _, err := VerifyES256Compact(&priv.PublicKey, tampered); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestES256WrongPublicKey(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := SignES256Compact(priv, []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyES256Compact(&other.PublicKey, compact); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestES256MalformedCompact(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

	cases := map[string]string{
		"wrong part count":   "abc.def",
		"too many parts":     "a.b.c.d",
		"bad base64 header":  "!!!.eyJhIjoxfQ.sig",
		"bad base64 payload": "eyJhbGciOiJFUzI1NiJ9.!!!.sig",
		"bad base64 sig":     "eyJhbGciOiJFUzI1NiJ9.eyJhIjoxfQ.!!!",
		"wrong sig length":   "eyJhbGciOiJFUzI1NiJ9.eyJhIjoxfQ." + "QQ",
		"wrong alg":          `eyJhbGciOiJIUzI1NiJ9.eyJhIjoxfQ.` + "QQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyES256Compact(pub, s); err == nil {
				t.Fatalf("%s: expected an error, got nil", name)
			}
		})
	}
}
