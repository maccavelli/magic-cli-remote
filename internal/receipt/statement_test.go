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
