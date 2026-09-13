//go:build windows

package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskXMLParsesInRealSchtasks is PLAN 0116 rows 15-17 (MADR 0116 D24).
//
// Every other service test stubs schtasks, which is how a task file Task
// Scheduler cannot parse shipped (F25). This one hands the bytes setupSchtasks
// writes to the real schtasks.exe.
//
// It must never register a task (PLAN C8). Each file carries one invalid value,
// <ExecutionTimeLimit>NOT_A_DURATION, so a file that PARSES is still rejected at
// validation ("incorrectly formatted"), while a file in the wrong encoding is
// rejected at parse ("malformed"). Measured on build 26100 with the rendered
// definition. The name is throwaway, it is deleted in cleanup whatever happens,
// and the test asserts it does not exist afterwards.
//
// schtasks messages are localised. Output that is neither English message is a
// skip that says so, never a pass.
//
// Running schtasks by bare name is why this is a _windows_test.go file
// (MADR 0147 D9/D10, testexec.TestNoTestResolvesABareBinaryName).
func TestTaskXMLParsesInRealSchtasks(t *testing.T) {
	body, err := renderTaskXML(windowsOpts(), currentTaskUser())
	if err != nil {
		t.Fatal(err)
	}
	const valid, invalid = "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "<ExecutionTimeLimit>NOT_A_DURATION</ExecutionTimeLimit>"
	if strings.Count(body, valid) != 1 {
		t.Fatalf("rendered definition does not contain %s exactly once; the no-registration technique cannot be applied", valid)
	}
	poisoned := strings.Replace(body, valid, invalid, 1)

	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "mc-0116-p12-probe-" + hex.EncodeToString(suffix[:])
	t.Cleanup(func() {
		_ = exec.Command("schtasks", "/delete", "/tn", name, "/f").Run()
		if exec.Command("schtasks", "/query", "/tn", name).Run() == nil {
			t.Errorf("PLAN C8 breached: task %q still registered after cleanup", name)
		}
	})

	create := func(t *testing.T, file []byte) (string, bool) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "task.xml")
		if err := os.WriteFile(path, file, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("schtasks", "/create", "/tn", name, "/xml", path, "/f").CombinedOutput()
		return strings.TrimSpace(string(out)), err == nil
	}

	t.Run("UTF-16LE with BOM parses", func(t *testing.T) {
		out, registered := create(t, encodeTaskXML(poisoned))
		switch {
		case registered:
			t.Fatalf("schtasks registered a task from a definition with an invalid value; C8 relies on that being impossible: %s", out)
		case strings.Contains(out, "malformed"):
			t.Fatalf("Task Scheduler could not parse encodeTaskXML's bytes: %s", out)
		case strings.Contains(out, "incorrectly formatted"):
			// Parsed, then rejected only the injected value.
		default:
			t.Skipf("unrecognised schtasks output (localised Windows?); cannot classify: %s", out)
		}
	})

	t.Run("negative control: the pre-P12 UTF-8 bytes are malformed", func(t *testing.T) {
		legacy := xml.Header + strings.TrimPrefix(poisoned, taskXMLDeclaration)
		out, registered := create(t, []byte(legacy))
		switch {
		case registered:
			t.Fatalf("schtasks registered a task from the legacy bytes: %s", out)
		case strings.Contains(out, "malformed"):
			// The encoding defect this test exists to catch is still detectable.
		case strings.Contains(out, "incorrectly formatted"):
			t.Fatalf("schtasks now parses UTF-8 under a UTF-8 declaration, so this test no longer discriminates; revisit MADR 0116 F24: %s", out)
		default:
			t.Skipf("unrecognised schtasks output (localised Windows?); cannot classify: %s", out)
		}
	})
}
