package providerauth_test

import (
	"context"
	"errors"
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

// Verbatim grok 1.0.5 (5115b46bc909) login --device-auth stdout captured
// 2026-08-19 under an isolated GROK_HOME (MADR 0107 D4). The code is a dead
// probe value. If grok reformats this, the parse must fail here rather than
// show the phone a wrong code.
const grokDeviceOutput = "\n" +
	"To sign in, open this URL in your browser:\n" +
	"\n" +
	"  https://accounts.x.ai/oauth2/device?user_code=PYGZ-C7A4\n" +
	"\n" +
	"Confirm this code in your browser:\n" +
	"\n" +
	"  PYGZ-C7A4\n" +
	"\n" +
	"Only continue with a code you requested. Don't share it with anyone.\n" +
	"\n" +
	"Waiting for authorization...\n"

func TestStripANSI(t *testing.T) {
	got := providerauth.StripANSI("   \x1b[94mK5GK-PUGKG\x1b[0m")
	if strings.TrimSpace(got) != "K5GK-PUGKG" {
		t.Fatalf("got %q", got)
	}
}

func TestParsesRealCodexDeviceOutput(t *testing.T) {
	bin := fakeCLI(t, codexDeviceOutput, 30)
	cls, flow, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 10*time.Second, nil)
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

func TestParsesRealGrokDeviceOutput(t *testing.T) {
	bin := fakeCLI(t, grokDeviceOutput, 30)
	cls, flow, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 10*time.Second, nil)
	if err != nil {
		t.Fatalf("StartCLIDeviceFlow: %v", err)
	}
	defer flow.Kill()

	if cls.UserCode != "PYGZ-C7A4" {
		t.Errorf("user code = %q, want PYGZ-C7A4", cls.UserCode)
	}
	wantURI := "https://accounts.x.ai/oauth2/device?user_code=PYGZ-C7A4"
	if cls.VerificationURI != wantURI {
		t.Errorf("verification URI = %q, want %q", cls.VerificationURI, wantURI)
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
		context.Background(), bin, nil, 3*time.Second, nil)
	if err == nil {
		t.Fatal("garbled output produced a flow")
	}
}

// A CLI that prints nothing must not hang the caller forever.
func TestScanTimeout(t *testing.T) {
	bin := fakeCLI(t, "", 30)
	start := time.Now()
	_, _, err := providerauth.StartCLIDeviceFlow(
		context.Background(), bin, nil, 2*time.Second, nil)
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
		context.Background(), bin, nil, 10*time.Second, nil)
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
		context.Background(), bin, nil, 10*time.Second, nil)
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

// TestWaitAndKillAreOrderIndependent proves every observer of a flow sees the
// same terminal result whichever order Wait and Kill are called in, and that
// both are safe to call repeatedly (MADR 0074 D27, P18 step 13).
func TestWaitAndKillAreOrderIndependent(t *testing.T) {
	t.Run("kill before wait", func(t *testing.T) {
		bin := fakeCLI(t, codexDeviceOutput, 120)
		_, flow, err := providerauth.StartCLIDeviceFlow(
			context.Background(), bin, nil, 10*time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		flow.Kill()
		flow.Kill() // idempotent

		first := flow.Wait(context.Background())
		second := flow.Wait(context.Background())
		if !errors.Is(first, providerauth.ErrFlowCancelled) {
			t.Fatalf("first = %v, want ErrFlowCancelled", first)
		}
		if !errors.Is(second, providerauth.ErrFlowCancelled) {
			t.Fatalf("second = %v, want the same terminal result", second)
		}
	})

	t.Run("wait cancelled then kill", func(t *testing.T) {
		bin := fakeCLI(t, codexDeviceOutput, 120)
		_, flow, err := providerauth.StartCLIDeviceFlow(
			context.Background(), bin, nil, 10*time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- flow.Wait(ctx) }()
		cancel()

		var first error
		select {
		case first = <-done:
		case <-time.After(15 * time.Second):
			t.Fatal("cancel did not end the wait")
		}
		flow.Kill()

		if !errors.Is(first, providerauth.ErrFlowCancelled) {
			t.Fatalf("first = %v, want ErrFlowCancelled", first)
		}
		if second := flow.Wait(context.Background()); !errors.Is(second, providerauth.ErrFlowCancelled) {
			t.Fatalf("second = %v, want the same terminal result", second)
		}
	})

	t.Run("concurrent waiters agree", func(t *testing.T) {
		bin := fakeCLI(t, codexDeviceOutput, 120)
		_, flow, err := providerauth.StartCLIDeviceFlow(
			context.Background(), bin, nil, 10*time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}

		const n = 6
		results := make(chan error, n)
		for range n {
			go func() { results <- flow.Wait(context.Background()) }()
		}
		flow.Kill()

		for i := range n {
			select {
			case err := <-results:
				if !errors.Is(err, providerauth.ErrFlowCancelled) {
					t.Fatalf("waiter %d = %v, want ErrFlowCancelled", i, err)
				}
			case <-time.After(15 * time.Second):
				t.Fatalf("waiter %d never returned", i)
			}
		}
	})

	t.Run("natural exit is not reported as cancelled", func(t *testing.T) {
		// A CLI that prints its code and exits promptly.
		bin := fakeCLI(t, codexDeviceOutput, 0)
		_, flow, err := providerauth.StartCLIDeviceFlow(
			context.Background(), bin, nil, 10*time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		first := flow.Wait(context.Background())
		// Killing an already-finished flow must not rewrite its outcome.
		flow.Kill()
		second := flow.Wait(context.Background())

		if errors.Is(first, providerauth.ErrFlowCancelled) {
			t.Fatalf("a clean exit was reported as cancelled: %v", first)
		}
		if (first == nil) != (second == nil) {
			t.Fatalf("terminal result changed after Kill: %v then %v", first, second)
		}
	})
}
