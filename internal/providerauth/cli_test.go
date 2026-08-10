package providerauth_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// fakeCLI writes a script that emits the given output then sleeps, imitating a
// device flow that prints its code and polls.
func fakeCLI(t *testing.T, output string, sleepSeconds int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-cli")
	script := "#!/bin/sh\ncat <<'MCEOF'\n" + output + "\nMCEOF\nsleep " +
		itoa(sleepSeconds) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func itoa(i int) string {
	if i <= 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return out
}

// The verbatim codex 0.146.0 device output captured on 2026-08-10, ANSI escapes
// and all. If codex reformats this, the parse must fail loudly here rather
// than silently show the phone a wrong code (MADR 0074 D15).
const codexDeviceOutput = "\n" +
	"Welcome to Codex [v\x1b[90m0.146.0\x1b[0m]\n" +
	"\x1b[90mOpenAI's command-line coding agent\x1b[0m\n" +
	"\n" +
	"Follow these steps to sign in with ChatGPT using device code authorization:\n" +
	"\n" +
	"1. Open this link in your browser and sign in to your account\n" +
	"   \x1b[94mhttps://auth.openai.com/codex/device\x1b[0m\n" +
	"\n" +
	"2. Enter this one-time code \x1b[90m(expires in 15 minutes)\x1b[0m\n" +
	"   \x1b[94mK5GK-PUGKG\x1b[0m\n"

func TestStripANSI(t *testing.T) {
	got := providerauth.StripANSI("   \x1b[94mK5GK-PUGKG\x1b[0m")
	if strings.TrimSpace(got) != "K5GK-PUGKG" {
		t.Fatalf("got %q", got)
	}
}

func TestParsesRealCodexDeviceOutput(t *testing.T) {
	bin := fakeCLI(t, codexDeviceOutput, 30)
	cls, flow, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 10*time.Second)
	if err != nil {
		t.Fatalf("StartCLIDeviceFlow: %v", err)
	}
	defer flow.Kill()

	if cls.UserCode != "K5GK-PUGKG" {
		t.Errorf("user code = %q, want K5GK-PUGKG", cls.UserCode)
	}
	if cls.VerificationURI != "https://auth.openai.com/codex/device" {
		t.Errorf("verification URI = %q", cls.VerificationURI)
	}
	if strings.Contains(cls.UserCode, "\x1b") || strings.Contains(cls.VerificationURI, "\x1b") {
		t.Error("ANSI escapes survived into the parsed values")
	}
}

// Garbled output must fail cleanly rather than produce a plausible-looking
// wrong code — the user would type it, be rejected, and have no idea why.
func TestGarbledOutputFailsCleanly(t *testing.T) {
	bin := fakeCLI(t, "something went wrong\nno code here\n", 1)
	_, _, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 3*time.Second)
	if err == nil {
		t.Fatal("garbled output produced a flow")
	}
}

// A CLI that prints nothing must not hang the caller forever.
func TestScanTimeout(t *testing.T) {
	bin := fakeCLI(t, "", 30)
	start := time.Now()
	_, _, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 2*time.Second)
	if err == nil {
		t.Fatal("a silent CLI produced a flow")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("scan timeout not honoured (%s)", elapsed)
	}
}

// Cancelling must reap the whole process group. The real motivation: codex's
// npm shim execs a vendored grandchild that survived a plain kill during the
// MADR 0074 probe and kept the flow alive after the parent was gone.
func TestKillReapsTheProcessGroup(t *testing.T) {
	bin := fakeCLI(t, codexDeviceOutput, 120)
	_, flow, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	flow.Kill()

	done := make(chan error, 1)
	go func() { done <- flow.Wait(context.Background()) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Wait never returned after Kill; the child outlived the flow")
	}
}

func TestWaitHonoursContext(t *testing.T) {
	bin := fakeCLI(t, codexDeviceOutput, 120)
	_, flow, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Kill()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- flow.Wait(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled wait reported success")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancel did not end the wait")
	}
}
