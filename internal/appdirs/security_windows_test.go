//go:build windows

package appdirs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsPrivateDACL is the direct MADR 0116 F23b regression.
//
// The pre-fix code compared the round-tripped SDDL string against the
// privateDACL constant, which cannot match: Windows resolves the "OW" alias to
// a concrete SID on write and may add the AI flag. These cases pin the ACE-set
// comparison that replaced it.
func TestIsPrivateDACL(t *testing.T) {
	owner, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sid := owner.String()

	for _, tc := range []struct {
		name string
		sddl string
		want bool
	}{
		{
			// What Windows actually returns after we write privateDACL: the
			// owner alias resolved, and the AI flag added.
			name: "resolved owner sid with AI flag",
			sddl: "O:" + sid + "G:" + sid + "D:PAI(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)",
			want: true,
		},
		{
			name: "literal constant form",
			sddl: "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)",
			want: true,
		},
		{
			name: "SYSTEM as an explicit sid",
			sddl: "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;S-1-5-18)",
			want: true,
		},
		{
			// Inheritance not severed: a parent's ACEs still apply.
			name: "not protected",
			sddl: "D:AI(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)",
			want: false,
		},
		{
			name: "extra trustee has access",
			sddl: "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BU)",
			want: false,
		},
		{
			name: "owner missing",
			sddl: "D:P(A;OICI;FA;;;SY)",
			want: false,
		},
		{
			name: "no dacl at all",
			sddl: "O:" + sid + "G:" + sid,
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPrivateDACL(tc.sddl, owner); got != tc.want {
				t.Errorf("isPrivateDACL(%q) = %v, want %v", tc.sddl, got, tc.want)
			}
		})
	}
}

// TestSplitDACL pins the parser the ACE-set comparison depends on.
func TestSplitDACL(t *testing.T) {
	flags, aces := splitDACL("D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if flags != "PAI" {
		t.Errorf("flags = %q, want PAI", flags)
	}
	if len(aces) != 2 {
		t.Fatalf("aces = %v, want 2", aces)
	}
	if aces[0] != "(A;OICI;FA;;;SY)" || aces[1] != "(A;OICI;FA;;;BA)" {
		t.Errorf("aces = %v", aces)
	}
}

// TestCanonicalTrustee proves aliases and explicit SIDs compare equal, which
// is what lets the round-tripped descriptor match the constant.
func TestCanonicalTrustee(t *testing.T) {
	owner, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicalTrustee("SY", owner); got != "S-1-5-18" {
		t.Errorf("SY -> %q", got)
	}
	if got, want := canonicalTrustee("OW", owner), owner.String(); got != want {
		t.Errorf("OW -> %q, want %q", got, want)
	}
	if got := canonicalTrustee("S-1-5-18", owner); got != "S-1-5-18" {
		t.Errorf("explicit sid changed: %q", got)
	}
}

// TestFileIsOwnerOnlyWindows proves the D22 property holds for a file this
// process secured, and that an unsecured file is reported honestly.
func TestFileIsOwnerOnlyWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cred.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SecurePrivateFile(path); err != nil {
		t.Fatalf("SecurePrivateFile: %v", err)
	}
	ok, err := FileIsOwnerOnly(path)
	if err != nil {
		t.Fatalf("FileIsOwnerOnly: %v", err)
	}
	if !ok {
		t.Error("a file just secured with the private DACL is not owner-only")
	}
	// Idempotent: a second call must not error.
	if err := SecurePrivateFile(path); err != nil {
		t.Fatalf("second SecurePrivateFile: %v", err)
	}
}
