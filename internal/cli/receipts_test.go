package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/receipt"
)

// receiptsFixture registers one real device key in a real auth.Store —
// matching PublicKeyFor's actual code path, not a stub — under a fresh
// temp data_dir the `--data-dir` flag points every subcommand at.
func receiptsFixture(t *testing.T) (dir, deviceID string, devicePriv *ecdsa.PrivateKey, rs *receipt.Store) {
	t.Helper()
	dir = t.TempDir()

	devicePriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&devicePriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	authStore, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	dev, _, err := authStore.CreateWithClientKey("phone", "fp-1", spki)
	if err != nil {
		t.Fatal(err)
	}
	rs, err = receipt.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, dev.ID, devicePriv, rs
}

func appendSignedDecision(t *testing.T, rs *receipt.Store, priv *ecdsa.PrivateKey, deviceID string, seq int) {
	t.Helper()
	last, ok, err := rs.LastHash(deviceID)
	if err != nil {
		t.Fatal(err)
	}
	var prev *string
	if ok {
		prev = &last
	}
	stmt, err := receipt.BuildPermissionDecisionStatement(
		"sess-1", "perm-"+strconv.Itoa(seq), deviceID, "once",
		"bash", "echo hi", time.Unix(int64(seq), 0).UTC(), "device:"+deviceID, prev,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := receipt.SignES256Compact(priv, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Append(deviceID, compact); err != nil {
		t.Fatal(err)
	}
}

// TestReceiptsVerifyIntactChain covers the "exits zero on an intact chain"
// half of this phase's core Acceptance bullet.
func TestReceiptsVerifyIntactChain(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	appendSignedDecision(t, rs, devicePriv, deviceID, 0)
	appendSignedDecision(t, rs, devicePriv, deviceID, 1)
	appendSignedDecision(t, rs, devicePriv, deviceID, 2)

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"verify", "--device", deviceID, "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "OK") || !strings.Contains(buf.String(), "intact") {
		t.Fatalf("expected an OK/intact report, got:\n%s", buf.String())
	}
}

// TestReceiptsVerifyReportsCorruptedLine covers the other half: a
// hand-corrupted `.jsonl` fixture reports the exact broken line and the
// command exits non-zero — safe to use in an audit script.
func TestReceiptsVerifyReportsCorruptedLine(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	appendSignedDecision(t, rs, devicePriv, deviceID, 0)
	appendSignedDecision(t, rs, devicePriv, deviceID, 1)
	appendSignedDecision(t, rs, devicePriv, deviceID, 2)

	// Hand-corrupt line 2 (1-indexed) directly on disk.
	path := filepath.Join(dir, "receipts", deviceID+".jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	lines[1] = flipSignaturePart(t, lines[1])
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"verify", "--device", deviceID, "--data-dir", dir})
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected a non-zero exit on a broken chain, output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "BROKEN") || !strings.Contains(buf.String(), "line 2") {
		t.Fatalf("expected a report naming line 2 as broken, got:\n%s", buf.String())
	}
}

// TestReceiptsListReportsIntactChain exercises `list` end to end, the
// summary surface an operator sees before drilling into `verify`/`show`.
func TestReceiptsListReportsIntactChain(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	appendSignedDecision(t, rs, devicePriv, deviceID, 0)
	appendSignedDecision(t, rs, devicePriv, deviceID, 1)

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, deviceID) || !strings.Contains(out, "2") || !strings.Contains(out, "intact") {
		t.Fatalf("expected device/count/intact in list output, got:\n%s", out)
	}
}

// TestReceiptsShowMatchesWhatWasApproved: the "what did this receipt
// actually attest to" command must surface the decoded tool/detail/option,
// not raw JWS — and confirm the signature verified.
func TestReceiptsShowMatchesWhatWasApproved(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	appendSignedDecision(t, rs, devicePriv, deviceID, 0)

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "--device", deviceID, "--permission", "perm-0", "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{"VERIFIED", "bash", "echo hi", "once", "permission-decision"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NOT VERIFIED") {
		t.Fatalf("expected a verified signature, got:\n%s", out)
	}
}

// TestReceiptsShowHandoffRelease: a handoff receipt (MADR 0078) is looked up
// by its nonce and rendered with from→to and the release predicate — proving
// `show` handles the new kinds, not just permission decisions.
func TestReceiptsShowHandoffRelease(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)

	stmt, err := receipt.BuildHandoffReleaseStatement(
		"sess-1", deviceID, "device-b", "nonce-42",
		time.Unix(1, 0).UTC(), "device:"+deviceID, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := receipt.SignES256Compact(devicePriv, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Append(deviceID, compact); err != nil {
		t.Fatal(err)
	}

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// Looked up by the handoff nonce (the correlation id for a handoff).
	cmd.SetArgs([]string{"show", "--device", deviceID, "--permission", "nonce-42", "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show handoff: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{"VERIFIED", "session-handoff-release", "device-b", "from_device_id"} {
		if !strings.Contains(out, want) {
			t.Errorf("handoff show output missing %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NOT VERIFIED") {
		t.Fatalf("handoff receipt should verify against the device key, got:\n%s", out)
	}
}

// TestReceiptsShowUnknownPredicateTypeDoesNotCrash: a malformed/unknown
// predicateType must degrade to an unverified-but-displayed decode, not a
// crash and not a silently empty result.
func TestReceiptsShowUnknownPredicateTypeDoesNotCrash(t *testing.T) {
	dir, deviceID, _, rs := receiptsFixture(t)

	// Hand-craft a Statement with an unknown predicateType, self-signed with
	// an unrelated key (verification is expected to fail/be skipped here —
	// what's under test is that `show` doesn't crash or refuse to display).
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{
		"_type": "https://mcremote.dev/attestations/receipt/v1",
		"subject": [{"name": "permission:perm-weird", "digest": {"sha256": "00"}}],
		"predicateType": "https://mcremote.dev/attestations/unknown-kind/v1",
		"predicate": {"anything": "goes"},
		"chain": {"scope": "device:` + deviceID + `", "prev_sha256": null}
	}`)
	compact, err := receipt.SignES256Compact(other, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Append(deviceID, compact); err != nil {
		t.Fatal(err)
	}

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "--device", deviceID, "--permission", "perm-weird", "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show must not error on an unknown predicateType: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "NOT VERIFIED") {
		t.Fatalf("expected an explicit NOT VERIFIED warning, got:\n%s", out)
	}
	if !strings.Contains(out, "unknown-kind") {
		t.Fatalf("expected the raw predicateType still shown, got:\n%s", out)
	}
}

// TestReceiptsVerifyAfterDeviceRevoked: revoking a device deletes its
// Device record — the auth store's copy of the public key — but the key
// archived beside the chain at receipt time (the daemon writes it before
// every round trip) keeps the chain verifiable. This is the regression
// guard for the "revoked devices" limitation found in the 0077
// post-implementation debug pass and closed on the follow-up.
func TestReceiptsVerifyAfterDeviceRevoked(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	appendSignedDecision(t, rs, devicePriv, deviceID, 0)
	appendSignedDecision(t, rs, devicePriv, deviceID, 1)

	// What the daemon's round trip does before contacting the phone.
	spki, err := x509.MarshalPKIXPublicKey(&devicePriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.ArchiveKey(deviceID, spki); err != nil {
		t.Fatal(err)
	}

	// Revoke the device — its record (and the auth store's key) is gone.
	authStore, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.PublicKeyFor(deviceID); err == nil {
		t.Fatal("fixture broken: auth store still resolves the revoked device's key")
	}

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"verify", "--device", deviceID, "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify after revoke must succeed via the archived key: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "intact") {
		t.Fatalf("expected an intact report, got:\n%s", buf.String())
	}

	// `show` resolves through the same fallback.
	buf.Reset()
	cmd = newReceiptsCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "--device", deviceID, "--permission", "perm-0", "--data-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show after revoke: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "Signature: VERIFIED") {
		t.Fatalf("expected a VERIFIED signature via the archived key, got:\n%s", buf.String())
	}
}

// TestReceiptsVerifyRevokedWithoutArchiveFailsClearly: a chain whose device
// was revoked before any key was archived (a pre-archival chain) cannot be
// verified — the error must name both misses rather than pretending the
// chain checked out.
func TestReceiptsVerifyRevokedWithoutArchiveFailsClearly(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	appendSignedDecision(t, rs, devicePriv, deviceID, 0)

	authStore, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}

	cmd := newReceiptsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"verify", "--device", deviceID, "--data-dir", dir})
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("verify must fail when no key exists anywhere, output:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "archived") {
		t.Fatalf("error should mention the archive miss, got: %v", err)
	}
}

// flipSignaturePart decodes a JWS compact string's signature segment, flips
// a byte, and re-encodes — guaranteed to change the decoded value, unlike
// overwriting a fixed trailing base64 character (whose last position can
// carry as few as 2 real bits, sometimes round-tripping to the same byte).
func flipSignaturePart(t *testing.T, compact string) string {
	t.Helper()
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		t.Fatalf("not a 3-part JWS: %q", compact)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xFF
	parts[2] = base64.RawURLEncoding.EncodeToString(raw)
	return strings.Join(parts, ".")
}
