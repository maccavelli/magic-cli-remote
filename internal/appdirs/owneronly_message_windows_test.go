//go:build windows

package appdirs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// usersSID is BUILTIN\Users: a principal FileIsOwnerOnly treats as foreign that
// exists on every Windows install. Fixtures grant it access explicitly, never
// by relying on what the host's %TEMP% happens to inherit (MADR 0155 F5).
const usersSID = "S-1-5-32-545"

// TestForeignTrusteesAgreesWithPredicate pins the invariant that makes the
// message trustworthy: foreignTrustees is empty exactly when noForeignTrustee
// is true, so a refusal never names a principal the check did not object to
// and never omits one it did.
func TestForeignTrusteesAgreesWithPredicate(t *testing.T) {
	owner, err := windows.StringToSid("S-1-5-21-1111111111-2222222222-3333333333-1001")
	if err != nil {
		t.Fatal(err)
	}
	o := owner.String()
	for _, tc := range []struct {
		name string
		sddl string
		want []string
	}{
		{"owner only", "O:" + o + "D:P(A;;FA;;;" + o + ")", nil},
		{"owner, SYSTEM, Administrators", "O:" + o + "D:(A;;FA;;;OW)(A;;FA;;;SY)(A;;FA;;;BA)", nil},
		{"explicit Users read", "O:" + o + "D:(A;;FA;;;" + o + ")(A;;FR;;;BU)", []string{"BU"}},
		{"inherited Users read", "O:" + o + "D:AI(A;ID;FA;;;" + o + ")(A;ID;0x1200a9;;;BU)", []string{"BU"}},
		{"two foreign, one repeated", "O:" + o + "D:(A;;FR;;;BU)(A;;FR;;;WD)(A;OICI;FR;;;BU)", []string{"BU", "WD"}},
		{"foreign SID string", "O:" + o + "D:(A;;FR;;;S-1-5-21-9-9-9-500)", []string{"S-1-5-21-9-9-9-500"}},
		{"a DENY for a foreign trustee does not count", "O:" + o + "D:(A;;FA;;;OW)(D;;FA;;;WD)", nil},
		{"no DACL means everyone", "O:" + o, []string{"WD"}},
		{"unparsable ACE", "O:" + o + "D:(A;;FA)", []string{"?"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := foreignTrustees(tc.sddl, owner)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("foreignTrustees = %q, want %q", got, tc.want)
			}
			if pred := noForeignTrustee(tc.sddl, owner); pred != (len(got) == 0) {
				t.Errorf("disagreement: noForeignTrustee=%v but foreignTrustees=%q", pred, got)
			}
		})
	}
}

// exposedFile creates a file in a directory this project made private, then
// gives BUILTIN\Users read access to it: explicitly on the file, or inherited
// from a subdirectory that grants it to its children.
func exposedFile(t *testing.T, inherited bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if inherited {
		dir = filepath.Join(dir, "shared")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("icacls", dir, "/grant", "*"+usersSID+":(OI)(CI)(R)").CombinedOutput(); err != nil {
			t.Fatalf("icacls grant inheritable on dir: %v: %s", err, out)
		}
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !inherited {
		if out, err := exec.Command("icacls", path, "/grant", "*"+usersSID+":(R)").CombinedOutput(); err != nil {
			t.Fatalf("icacls grant: %v: %s", err, out)
		}
	}
	if ok, err := FileIsOwnerOnly(path); err != nil || ok {
		t.Fatalf("fixture is not exposed (ok=%v err=%v); the test would assert nothing", ok, err)
	}
	return path
}

// TestNotOwnerOnlyDetailNamesTheTrustee is MADR 0155 F6: the Windows message
// must say who can read the file and must not prescribe chmod.
func TestNotOwnerOnlyDetailNamesTheTrustee(t *testing.T) {
	users, err := windows.StringToSid(usersSID)
	if err != nil {
		t.Fatal(err)
	}
	// Resolved here too, so the assertion holds on a localised Windows where
	// BUILTIN\Users has another name.
	want := accountName(users)

	for _, inherited := range []bool{false, true} {
		path := exposedFile(t, inherited)
		got := NotOwnerOnlyDetail(path)
		if !strings.HasPrefix(got, "readable by ") {
			t.Errorf("inherited=%v: detail %q does not start with the cause", inherited, got)
		}
		if !strings.Contains(got, want) {
			t.Errorf("inherited=%v: detail %q does not name %s", inherited, got, want)
		}
		if !strings.Contains(got, "/remove:g *"+usersSID) {
			t.Errorf("inherited=%v: remedy %q does not remove %s", inherited, got, usersSID)
		}
		if strings.Contains(strings.ToLower(got), "chmod") {
			t.Errorf("inherited=%v: Windows detail says chmod: %q", inherited, got)
		}
	}
}

// TestNotOwnerOnlyDetailRemedyWorks runs the exact command the detail prints,
// as an operator would paste it into either shell, and requires the file to
// be owner-only afterwards. Advice that does not work is F6 again, so the
// wording is not trusted until it has been executed.
func TestNotOwnerOnlyDetailRemedyWorks(t *testing.T) {
	shells := []struct {
		name string
		argv func(cmd string) []string
	}{
		{"cmd", func(c string) []string { return []string{"cmd", "/d", "/c", c} }},
		{"powershell", func(c string) []string {
			return []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", c}
		}},
	}
	for _, sh := range shells {
		for _, inherited := range []bool{false, true} {
			name := sh.name
			if inherited {
				name += "/inherited"
			} else {
				name += "/explicit"
			}
			t.Run(name, func(t *testing.T) {
				path := exposedFile(t, inherited)
				detail := NotOwnerOnlyDetail(path)
				_, remedy, found := strings.Cut(detail, "; run: ")
				if !found {
					t.Fatalf("detail %q has no remedy", detail)
				}
				argv := sh.argv(remedy)
				cmd := exec.Command(argv[0], argv[1:]...)
				if sh.name == "cmd" {
					// cmd re-parses its command line itself; hand it the
					// string verbatim, as a pasted line would arrive.
					cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /d /c ` + remedy}
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("remedy %q failed under %s: %v: %s", remedy, sh.name, err, out)
				}
				ok, err := FileIsOwnerOnly(path)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Errorf("after running %q under %s the file is still not owner-only", remedy, sh.name)
				}
				// And the caller must still be able to read it: a remedy that
				// locks the operator out of their own config is no remedy.
				if _, err := os.ReadFile(path); err != nil {
					t.Errorf("after the remedy the owner cannot read the file: %v", err)
				}
			})
		}
	}
}
