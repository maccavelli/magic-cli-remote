package receipt

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func fixedDecidedAt() time.Time {
	return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
}

const fixedPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

// diffAgainstGolden marshals got with the same indentation the fixture files
// use and byte-compares — this is the contract test that keeps the wire
// shape (MADR 0077 §7.2's example) from drifting silently.
func diffAgainstGolden(t *testing.T, path string, got any) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	gotBytes = append(gotBytes, '\n')
	if string(gotBytes) != string(want) {
		t.Fatalf("golden mismatch against %s:\n--- got ---\n%s\n--- want ---\n%s", path, gotBytes, want)
	}
}

func TestStatementPermissionDecisionGolden(t *testing.T) {
	prev := fixedPrevHash
	stmt, err := BuildPermissionDecisionStatement(
		"sess-1", "perm-1", "device-1", "opt-allow",
		"bash", "rm -rf /tmp/scratch",
		fixedDecidedAt(), "device:device-1", &prev,
	)
	if err != nil {
		t.Fatal(err)
	}
	diffAgainstGolden(t, "testdata/statement_permission_decision.json", stmt)
}

// TestStatementReceiptUnavailableGolden covers both D8 failure-mode reasons
// as separate golden cases: they must produce distinguishable records, not
// the same marker with different callers assumed to remember why.
func TestStatementReceiptUnavailableGolden(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		prev := fixedPrevHash
		stmt, err := BuildReceiptUnavailableStatement("perm-2", "device-1", "timeout", "device:device-1", &prev)
		if err != nil {
			t.Fatal(err)
		}
		diffAgainstGolden(t, "testdata/statement_receipt_unavailable_timeout.json", stmt)
	})
	t.Run("invalid_signature", func(t *testing.T) {
		stmt, err := BuildReceiptUnavailableStatement("perm-3", "device-1", "invalid_signature", "device:device-1", nil)
		if err != nil {
			t.Fatal(err)
		}
		diffAgainstGolden(t, "testdata/statement_receipt_unavailable_invalid_signature.json", stmt)
	})
}

// roundTrip marshals, unmarshals, and re-marshals stmt, asserting the second
// marshal is byte-identical to the first — the JSON round-trips cleanly for
// both predicate types (json.RawMessage preserves the inner predicate
// bytes exactly).
func roundTrip(t *testing.T, stmt *Statement) {
	t.Helper()
	first, err := json.Marshal(stmt)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Statement
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("round trip not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestStatementRoundTripPermissionDecision(t *testing.T) {
	prev := fixedPrevHash
	stmt, err := BuildPermissionDecisionStatement(
		"sess-1", "perm-1", "device-1", "opt-allow",
		"bash", "rm -rf /tmp/scratch",
		fixedDecidedAt(), "device:device-1", &prev,
	)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, stmt)

	var decoded Statement
	if err := json.Unmarshal(mustMarshal(t, stmt), &decoded); err != nil {
		t.Fatal(err)
	}
	var pred PermissionDecisionPredicate
	if err := json.Unmarshal(decoded.Predicate, &pred); err != nil {
		t.Fatal(err)
	}
	if pred.ToolName != "bash" || pred.DeviceID != "device-1" {
		t.Fatalf("decoded predicate = %+v", pred)
	}
}

func TestStatementRoundTripReceiptUnavailable(t *testing.T) {
	stmt, err := BuildReceiptUnavailableStatement("perm-3", "device-1", "invalid_signature", "device:device-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, stmt)

	var decoded Statement
	if err := json.Unmarshal(mustMarshal(t, stmt), &decoded); err != nil {
		t.Fatal(err)
	}
	var pred UnavailablePredicate
	if err := json.Unmarshal(decoded.Predicate, &pred); err != nil {
		t.Fatal(err)
	}
	if pred.Reason != "invalid_signature" || pred.PermissionID != "perm-3" {
		t.Fatalf("decoded predicate = %+v", pred)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixedHandoffAt() time.Time {
	return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
}

// TestStatementHandoffGolden pins the wire shape of both handoff predicates
// (MADR 0078 D4).
func TestStatementHandoffGolden(t *testing.T) {
	prev := fixedPrevHash
	rel, err := BuildHandoffReleaseStatement("sess-1", "device-a", "device-b", "nonce-xyz",
		fixedHandoffAt(), "device:device-a", &prev)
	if err != nil {
		t.Fatal(err)
	}
	diffAgainstGolden(t, "testdata/statement_handoff_release.json", rel)

	clm, err := BuildHandoffClaimStatement("sess-1", "device-b", "device-a", "nonce-xyz",
		fixedHandoffAt(), "device:device-b", &prev)
	if err != nil {
		t.Fatal(err)
	}
	diffAgainstGolden(t, "testdata/statement_handoff_claim.json", clm)
}

// TestHandoffReceiptsShareSubject: the release and claim Statements of one
// transfer must carry the SAME subject name and digest, since that shared
// subject is the only thing linking them across the two devices' separate
// chains (MADR 0078 D4). They differ in predicateType and chain scope.
func TestHandoffReceiptsShareSubject(t *testing.T) {
	prev := fixedPrevHash
	rel, err := BuildHandoffReleaseStatement("sess-9", "dev-a", "dev-b", "n-1",
		fixedHandoffAt(), "device:dev-a", &prev)
	if err != nil {
		t.Fatal(err)
	}
	clm, err := BuildHandoffClaimStatement("sess-9", "dev-b", "dev-a", "n-1",
		fixedHandoffAt(), "device:dev-b", &prev)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Subject[0].Name != clm.Subject[0].Name {
		t.Fatalf("subject names differ: %q vs %q", rel.Subject[0].Name, clm.Subject[0].Name)
	}
	if rel.Subject[0].Digest["sha256"] != clm.Subject[0].Digest["sha256"] {
		t.Fatal("subject digests differ — the two halves would not link")
	}
	if rel.PredicateType == clm.PredicateType {
		t.Fatal("release and claim must have distinct predicate types")
	}
	if rel.Chain.Scope == clm.Chain.Scope {
		t.Fatal("release and claim must land in different device chains")
	}
	// A different nonce breaks the linkage (proves the subject is nonce-bound).
	other, _ := BuildHandoffClaimStatement("sess-9", "dev-b", "dev-a", "n-2",
		fixedHandoffAt(), "device:dev-b", &prev)
	if other.Subject[0].Name == rel.Subject[0].Name {
		t.Fatal("a different nonce must produce a different subject name")
	}
}

// TestStatementRoundTripHandoff: both handoff predicates decode/re-encode
// byte-identically and their predicate bodies survive.
func TestStatementRoundTripHandoff(t *testing.T) {
	prev := fixedPrevHash
	rel, err := BuildHandoffReleaseStatement("sess-1", "device-a", "", "n-open",
		fixedHandoffAt(), "device:device-a", &prev)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, rel)
	var relPred HandoffReleasePredicate
	if err := json.Unmarshal(rel.Predicate, &relPred); err != nil {
		t.Fatal(err)
	}
	// Open release: to_device_id omitted.
	if relPred.ToDeviceID != "" || relPred.FromDeviceID != "device-a" {
		t.Fatalf("release predicate = %+v", relPred)
	}

	clm, err := BuildHandoffClaimStatement("sess-1", "device-b", "device-a", "n-open",
		fixedHandoffAt(), "device:device-b", &prev)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, clm)
	var clmPred HandoffClaimPredicate
	if err := json.Unmarshal(clm.Predicate, &clmPred); err != nil {
		t.Fatal(err)
	}
	if clmPred.ClaimedByDeviceID != "device-b" {
		t.Fatalf("claim predicate = %+v", clmPred)
	}
}
