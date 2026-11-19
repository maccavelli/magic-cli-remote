package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// failOnWriter refuses any write that contains marker, with errDiskFull, and
// keeps everything else. It records the refused text, so a test can look for it
// in an error message.
type failOnWriter struct {
	marker   string
	buf      bytes.Buffer
	rejected string
}

var errDiskFull = errors.New("disk full")

func (w *failOnWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		w.rejected += string(p)
		return 0, errDiskFull
	}
	return w.buf.Write(p)
}

// A command's result is not a status line: when it cannot be written the command
// fails, so a script that wrote it to a file or a pipe finds out (0131 P5, R2).

func TestSetupServicePrintOnlyReturnsAFailedWrite(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	cfgFile = ""
	var f setupServiceFlags
	f.printOnly = true
	f.unitName = "test-mcremote"
	w := &failOnWriter{marker: "[Unit]"}
	cmd := newSetupServiceCmd()
	cmd.SetOut(w)

	err := runSetupService(cmd, f)
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the service definition") {
		t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
	}
}

func TestSetupServiceRefreshPrintOnlyReturnsAFailedWrite(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	testexec.SkipIfNoUnixServiceManager(t) // Windows refreshes a Task Scheduler task, covered by schtasks_test.go
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dir := t.TempDir()
	body, err := service.RenderUnit(service.Options{
		Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(cfgHome, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "mcremote.service"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgFile = ""
	var f setupServiceFlags
	f.refresh = true
	f.printOnly = true

	// A healthy writer gets the unit, and nothing else.
	var healthy bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&healthy)
	if err := runSetupService(cmd, f); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(healthy.String(), "\n[Unit]\n") {
		t.Fatalf("refresh --print-only wrote %d bytes without a [Unit] section, want the unit body", healthy.Len())
	}

	w := &failOnWriter{marker: "[Unit]"}
	cmd = newSetupServiceCmd()
	cmd.SetOut(w)
	err = runSetupService(cmd, f)
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the refreshed service definition") {
		t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
	}
}

func TestReceiptsShowReturnsAFailedWrite(t *testing.T) {
	for _, tc := range []struct{ name, marker string }{
		{"the verdict line", "Signature:"},
		{"the statement's JSON", "permission-decision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, deviceID, devicePriv, rs := receiptsFixture(t)
			appendSignedDecision(t, rs, devicePriv, deviceID, 0)
			w := &failOnWriter{marker: tc.marker}
			cmd := newReceiptsCmd()
			cmd.SetOut(w)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"show", "--device", deviceID, "--permission", "perm-0", "--data-dir", dir})
			err := cmd.Execute()
			if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "write the receipt") {
				t.Fatalf("err = %v, want the write error, wrapped with what was being written", err)
			}
		})
	}
}

// secretIn returns what follows label in the refused text, up to the end of the line.
func secretIn(t *testing.T, rejected, label string) string {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(label) + `\s*(\S+)`).FindStringSubmatch(rejected)
	if m == nil {
		t.Fatalf("refused text %q does not carry %q", rejected, label)
	}
	return m[1]
}

func runPairTo(t *testing.T, w *failOnWriter, args ...string) error {
	t.Helper()
	cmd := newPairCmd()
	cmd.SetOut(w)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

// The token is minted before it is printed. When it cannot be printed the command
// says so and says what is true of it, and the error does not carry it.
func TestPairCreateReturnsAFailedTokenWrite(t *testing.T) {
	dataDir := writePairConfig(t)
	w := &failOnWriter{marker: "Token:"}
	err := runPairTo(t, w, "create", "--name", "phone")
	if !errors.Is(err, errDiskFull) {
		t.Fatalf("err = %v, want the write error", err)
	}
	token := secretIn(t, w.rejected, "Token:")
	if strings.Contains(err.Error(), token) {
		t.Fatalf("the error carries the token: %v", err)
	}
	store, serr := auth.OpenStore(filepath.Join(dataDir, "devices.json"))
	if serr != nil {
		t.Fatal(serr)
	}
	devs, lerr := store.List()
	if lerr != nil || len(devs) != 1 {
		t.Fatalf("devices = %v, %v; want the one that was created", devs, lerr)
	}
	for _, want := range []string{devs[0].ID, "could not be written", "only its hash is stored", "pair revoke " + devs[0].ID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	if _, verr := store.Validate(token); verr != nil {
		t.Errorf("the token that could not be shown no longer validates: %v", verr)
	}
}

func TestPairCreateReturnsAFailedURIWrite(t *testing.T) {
	writePairConfig(t)
	w := &failOnWriter{marker: "Pair:"}
	err := runPairTo(t, w, "create", "--name", "phone")
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "pairing URI") {
		t.Fatalf("err = %v, want the write error naming the pairing URI", err)
	}
	if uri := secretIn(t, w.rejected, "Pair:"); strings.Contains(err.Error(), uri) {
		t.Fatalf("the error carries the URI: %v", err)
	}
	if !strings.Contains(w.buf.String(), "Token:  ") {
		t.Fatalf("the token line was not written before the URI failed:\n%s", w.buf.String())
	}
}

func TestPairCodeReturnsAFailedCodeWrite(t *testing.T) {
	dataDir := writePairConfig(t)
	w := &failOnWriter{marker: "Pair code:"}
	err := runPairTo(t, w, "code", "--name", "phone")
	if !errors.Is(err, errDiskFull) {
		t.Fatalf("err = %v, want the write error", err)
	}
	code := secretIn(t, w.rejected, "Pair code:")
	if strings.Contains(err.Error(), code) {
		t.Fatalf("the error carries the code: %v", err)
	}
	for _, want := range []string{"could not be written", "stays valid for", "run the command again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	// Nothing is revoked or minted again: the one code is still on disk.
	b, rerr := os.ReadFile(filepath.Join(dataDir, "pair_codes.json"))
	if rerr != nil || strings.Count(string(b), `"name"`) != 1 {
		t.Fatalf("pair_codes.json = %q (%v), want the one code that was minted", b, rerr)
	}
}

func TestPairCodeReturnsAFailedURIWrite(t *testing.T) {
	writePairConfig(t)
	w := &failOnWriter{marker: "Pair URI:"}
	err := runPairTo(t, w, "code", "--name", "phone")
	if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "pairing URI") {
		t.Fatalf("err = %v, want the write error naming the pairing URI", err)
	}
	if uri := secretIn(t, w.rejected, "Pair URI:"); strings.Contains(err.Error(), uri) {
		t.Fatalf("the error carries the URI: %v", err)
	}
}

// A writer that works gets the same lines as before: the data sites changed how
// a failure is reported, not what is printed.
func TestPairOutputIsUnchangedForAHealthyWriter(t *testing.T) {
	writePairConfig(t)
	var out bytes.Buffer
	cmd := newPairCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"create", "--name", "phone"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	var labels []string
	for _, l := range lines {
		if i := strings.Index(l, ":"); i > 0 && i < 8 {
			labels = append(labels, l[:i+1])
		}
	}
	want := []string{"Device:", "Token:", "Host:", "WS URL:", "Pair:", "Tip:"}
	for _, w := range want {
		found := false
		for _, l := range labels {
			if l == w {
				found = true
			}
		}
		if !found {
			t.Errorf("output has no %q line:\n%s", w, out.String())
		}
	}
	if !strings.Contains(out.String(), "Store this token; it will not be shown again.\n") {
		t.Errorf("output lost its closing notice:\n%s", out.String())
	}
}
