package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func TestClassifySandboxError(t *testing.T) {
	cases := []struct {
		in   string
		want sandboxHealthReason
	}{
		{"No permissions to create a new namespace", sandboxUsernsDenied},
		{"error: fs sandbox helper failed: boom", sandboxUsernsDenied},
		{"kernel needs access to create user namespaces", sandboxUsernsDenied},
		{"bwrap: command not found", sandboxBwrapMissing},
		{"exec: bwrap: no such file or directory", sandboxBwrapMissing},
		{"something else went wrong", sandboxProbeFailed},
		{"", sandboxProbeFailed},
	}
	for _, tc := range cases {
		if got := classifySandboxError(tc.in); got != tc.want {
			t.Errorf("classifySandboxError(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLooksLikeSandboxNamespaceFailure(t *testing.T) {
	if !looksLikeSandboxNamespaceFailure("No permissions to create a new namespace") {
		t.Fatal("expected true for userns marker")
	}
	if looksLikeSandboxNamespaceFailure("file written ok") {
		t.Fatal("expected false for normal text")
	}
}

func TestApplySandboxBrokenPolicy(t *testing.T) {
	broken := sandboxHealth{OK: false, Reason: sandboxUsernsDenied, Detail: "denied"}
	okH := sandboxHealth{OK: true, Reason: sandboxOK}

	ap, sb, id, err := applySandboxBrokenPolicy(Config{}, okH, "never", "workspace-write", modeAuto)
	if err != nil || ap != "never" || sb != "workspace-write" || id != modeAuto {
		t.Fatalf("healthy = (%q,%q,%q,%v)", ap, sb, id, err)
	}

	ap, sb, id, err = applySandboxBrokenPolicy(Config{SandboxBrokenPolicy: sandboxPolicyWarn}, broken, "never", "workspace-write", modeAuto)
	if err != nil || id != modeAuto {
		t.Fatalf("warn = (%q,%q,%q,%v)", ap, sb, id, err)
	}

	_, _, _, err = applySandboxBrokenPolicy(Config{SandboxBrokenPolicy: sandboxPolicyRefuse}, broken, "never", "workspace-write", modeAuto)
	if err == nil {
		t.Fatal("refuse must error")
	}

	_, _, _, err = applySandboxBrokenPolicy(Config{
		SandboxBrokenPolicy: sandboxPolicyRequireFullAccess,
		AllowFullAccess:     false,
	}, broken, "never", "workspace-write", modeAuto)
	if err == nil {
		t.Fatal("require_full_access without allow_full_access must error")
	}

	ap, sb, id, err = applySandboxBrokenPolicy(Config{
		SandboxBrokenPolicy: sandboxPolicyRequireFullAccess,
		AllowFullAccess:     true,
	}, broken, "never", "workspace-write", modeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if id != modeFullAccess || sb != "danger-full-access" {
		t.Fatalf("require_full_access = (%q,%q,%q)", ap, sb, id)
	}
}

func TestSandboxProbeApplicable(t *testing.T) {
	if !sandboxProbeApplicable("linux") {
		t.Fatal("linux must probe")
	}
	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		if sandboxProbeApplicable(goos) {
			t.Fatalf("%s must not run userns probe", goos)
		}
	}
}

func TestProbeSandboxHealth_NonLinuxShortCircuit(t *testing.T) {
	h := probeSandboxHealthGOOS(context.Background(), "codex-never-run", "darwin")
	if !h.OK || h.Reason != sandboxOK {
		t.Fatalf("darwin probe = %+v, want ok short-circuit", h)
	}
	if !strings.Contains(h.Detail, "Linux-only") {
		t.Fatalf("detail = %q", h.Detail)
	}
}

func TestProbeSandboxHealth_FakeBinOK(t *testing.T) {
	testexec.SkipIfNoPOSIXShell(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex-stub")
	// Stub: when invoked as sandbox, write ok to the last arg (probe path).
	// POSIX sh only — CI Linux uses dash as /bin/sh (no ${@: -1}).
	script := `#!/bin/sh
set -e
# args: sandbox -c … -- /bin/sh -c 'echo ok > "$1"' _ PROBEFILE
for a in "$@"; do probe=$a; done
echo ok > "$probe"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Force linux path so CI on macOS still exercises the real probe.
	h := probeSandboxHealthGOOS(context.Background(), bin, "linux")
	if !h.OK || h.Reason != sandboxOK {
		t.Fatalf("probe = %+v, want ok", h)
	}
}

func TestProbeSandboxHealth_FakeBinDenied(t *testing.T) {
	testexec.SkipIfNoPOSIXShell(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex-stub")
	script := `#!/bin/sh
echo "No permissions to create a new namespace" >&2
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := probeSandboxHealthGOOS(context.Background(), bin, "linux")
	if h.OK || h.Reason != sandboxUsernsDenied {
		t.Fatalf("probe = %+v, want userns_denied", h)
	}
}

func TestNewSessionEmitsSandboxNoticeWhenUnhealthy(t *testing.T) {
	p := New(Config{Bin: "codex"})
	p.setSandboxHealth(sandboxHealth{
		OK:     false,
		Reason: sandboxUsernsDenied,
		Detail: "No permissions to create a new namespace",
	})
	s := newSession(p, p.cfg, provider.StartOptions{}, p.log)
	// Drain channel with timeout by reading after emit.
	s.emitSandboxBrokenNotice(p.sandboxHealth())
	select {
	case ev := <-s.events:
		if ev.Type != event.TypeNotice {
			t.Fatalf("type = %s, want notice", ev.Type)
		}
		if !strings.Contains(ev.Text, "user namespace") && !strings.Contains(ev.Text, "bubblewrap") {
			t.Fatalf("notice text = %q", ev.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("no notice event")
	}
	// Second emit is a no-op.
	s.emitSandboxBrokenNotice(p.sandboxHealth())
	select {
	case ev := <-s.events:
		t.Fatalf("unexpected second notice: %+v", ev)
	default:
	}
}

func TestNewSessionNoNoticeWhenHealthy(t *testing.T) {
	p := New(Config{Bin: "codex"})
	p.setSandboxHealth(sandboxHealth{OK: true, Reason: sandboxOK})
	s := newSession(p, p.cfg, provider.StartOptions{}, p.log)
	s.emitSandboxBrokenNotice(p.sandboxHealth())
	select {
	case ev := <-s.events:
		t.Fatalf("unexpected event: %+v", ev)
	default:
	}
}

func TestNoteSandboxFailurePromotesHealth(t *testing.T) {
	p := New(Config{})
	p.setSandboxHealth(sandboxHealth{OK: true, Reason: sandboxOK})
	p.noteSandboxFailure("No permissions to create a new namespace during apply_patch")
	h := p.sandboxHealth()
	if h.OK || h.Reason != sandboxUsernsDenied {
		t.Fatalf("health = %+v", h)
	}
}
