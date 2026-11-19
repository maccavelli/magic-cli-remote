package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `auth recovery status` and `receipts verify` print the result a script acts on, so a
// failed write is a failed command (0131 P5, R3). Neither may hide the other
// outcome: a broken chain is reported as broken whether or not the line saying so
// could be written.

func TestAuthRecoveryStatusReturnsAFailedWrite(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", cfgPath)

	root := &cobra.Command{Use: "mcremote", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("config", "", "")
	root.PersistentFlags().String("data-dir", "", "")
	root.AddCommand(newAuthRecoveryCmd())
	w := &failOnWriter{marker: "recovery_available="}
	root.SetOut(w)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"auth-recovery", "--data-dir", t.TempDir(), "status"})

	err := root.ExecuteContext(context.Background())
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the recovery status") {
		t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
	}
	if got := ExitCode(err); got != authRecoveryExitFailed {
		t.Fatalf("exit code = %d, want %d (the code of a failed command)", got, authRecoveryExitFailed)
	}
}

// verifyTo runs `receipts verify` for deviceID writing to w.
func verifyTo(t *testing.T, w *failOnWriter, dir, deviceID string) error {
	t.Helper()
	cmd := newReceiptsCmd()
	cmd.SetOut(w)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"verify", "--device", deviceID, "--data-dir", dir})
	return cmd.Execute()
}

// corruptLine2 flips the signature of the second line of the device's chain.
func corruptLine2(t *testing.T, dir, deviceID string) {
	t.Helper()
	path := filepath.Join(dir, "receipts", deviceID+".jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines[1] = flipSignaturePart(t, lines[1])
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptsVerifyIntactChainReturnsAFailedWrite(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	for i := range 3 {
		appendSignedDecision(t, rs, devicePriv, deviceID, i)
	}
	err := verifyTo(t, &failOnWriter{marker: "OK:"}, dir, deviceID)
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the verification result") {
		t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
	}
	if strings.Contains(err.Error(), "broken") {
		t.Fatalf("an intact chain is reported as broken: %v", err)
	}
}

// Both fail: the broken chain is the result and stays the headline, with the
// failed write added to it, not put in its place.
func TestReceiptsVerifyBrokenChainStaysBrokenWhenTheWriteFails(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	for i := range 3 {
		appendSignedDecision(t, rs, devicePriv, deviceID, i)
	}
	corruptLine2(t, dir, deviceID)

	err := verifyTo(t, &failOnWriter{marker: "BROKEN:"}, dir, deviceID)
	if err == nil || !strings.HasPrefix(err.Error(), "chain broken at line 2") {
		t.Fatalf("err = %v, want it to begin with 'chain broken at line 2'", err)
	}
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "could not be written") {
		t.Fatalf("err = %v, want the failed write added to the broken-chain error", err)
	}
}

// A healthy writer: the broken chain is reported exactly as before.
func TestReceiptsVerifyBrokenChainWithAHealthyWriterIsUnchanged(t *testing.T) {
	dir, deviceID, devicePriv, rs := receiptsFixture(t)
	for i := range 3 {
		appendSignedDecision(t, rs, devicePriv, deviceID, i)
	}
	corruptLine2(t, dir, deviceID)

	w := &failOnWriter{marker: "never-in-the-output"}
	err := verifyTo(t, w, dir, deviceID)
	if err == nil || err.Error() != "chain broken at line 2" {
		t.Fatalf("err = %v, want exactly 'chain broken at line 2'", err)
	}
	// cobra adds its usage text after a returned error; the verdict line comes first.
	if got := w.buf.String(); !strings.HasPrefix(got, "BROKEN: device "+deviceID+" chain broken at line 2.\n") {
		t.Fatalf("output = %q", got[:min(len(got), 120)])
	}
}
