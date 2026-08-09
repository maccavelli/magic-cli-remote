package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// sandboxHealthReason classifies Linux workspace-write sandbox viability
// (MADR 0048).
type sandboxHealthReason string

const (
	sandboxOK           sandboxHealthReason = "ok"
	sandboxUsernsDenied sandboxHealthReason = "userns_denied"
	sandboxBwrapMissing sandboxHealthReason = "bwrap_missing"
	sandboxProbeFailed  sandboxHealthReason = "probe_failed"
	sandboxUnknown      sandboxHealthReason = "unknown" // before first probe
)

// sandboxBrokenPolicy values (providers.codex.sandbox_broken_policy).
const (
	sandboxPolicyWarn              = "warn"
	sandboxPolicyRequireFullAccess = "require_full_access"
	sandboxPolicyRefuse            = "refuse"
)

type sandboxHealth struct {
	OK       bool
	Reason   sandboxHealthReason
	Detail   string
	ProbedAt time.Time
}

// sandboxHealthMarkers are substrings from bwrap/codex on userns-restricted
// hosts (MADR 0048 §4.1). Case-sensitive as emitted.
var sandboxHealthMarkers = []string{
	"No permissions to create a new namespace",
	"fs sandbox helper failed",
	"needs access to create user namespaces",
}

// looksLikeSandboxNamespaceFailure reports mid-turn / stderr text that means
// workspace-write cannot create a user namespace.
func looksLikeSandboxNamespaceFailure(s string) bool {
	return classifySandboxError(s) == sandboxUsernsDenied
}

// classifySandboxError maps probe/engine text to a health reason.
func classifySandboxError(text string) sandboxHealthReason {
	if text == "" {
		return sandboxProbeFailed
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "bwrap") && (strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no such file")) {
		return sandboxBwrapMissing
	}
	for _, m := range sandboxHealthMarkers {
		if strings.Contains(text, m) {
			return sandboxUsernsDenied
		}
	}
	// Broader fallbacks seen on some distros.
	if strings.Contains(lower, "user namespace") ||
		strings.Contains(lower, "unshare") && strings.Contains(lower, "operation not permitted") {
		return sandboxUsernsDenied
	}
	return sandboxProbeFailed
}

// sandboxProbeApplicable reports whether the Linux userns/bwrap probe should
// run (0071 F1). macOS uses Seatbelt (0069); a bwrap-shaped probe there is
// noise and can block engine start for no product value.
func sandboxProbeApplicable(goos string) bool {
	return goos == "linux"
}

// probeSandboxHealth runs a short, isolated check that workspace-write
// sandboxed execution can write a file. Must not touch the live app-server
// or user repos (MADR 0048 Phase 1). No-op success on non-Linux (0071 F1).
func probeSandboxHealth(ctx context.Context, bin string) sandboxHealth {
	return probeSandboxHealthGOOS(ctx, bin, runtime.GOOS)
}

func probeSandboxHealthGOOS(ctx context.Context, bin, goos string) sandboxHealth {
	h := sandboxHealth{Reason: sandboxUnknown, ProbedAt: time.Now().UTC()}
	if !sandboxProbeApplicable(goos) {
		h.OK = true
		h.Reason = sandboxOK
		h.Detail = "userns/bwrap probe is Linux-only; macOS uses Seatbelt (0069/0071 F1)"
		return h
	}
	if bin == "" {
		bin = "codex"
	}
	tmpdir, err := os.MkdirTemp("", "mcremote-codex-sandbox-probe-*")
	if err != nil {
		h.Reason = sandboxProbeFailed
		h.Detail = err.Error()
		return h
	}
	defer func() { _ = os.RemoveAll(tmpdir) }()

	probeFile := filepath.Join(tmpdir, "probe.txt")
	// Prefer real codex sandbox — the faithful discriminator (MADR 0048 §2.1.1).
	// Use /bin/sh (not bash) so Alpine-like hosts without bash still probe (0071 F1).
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// #nosec G204 — bin is operator config; args are fixed.
	cmd := exec.CommandContext(ctx, bin, "sandbox",
		"-c", `sandbox_mode="workspace-write"`,
		"--", "/bin/sh", "-c", `echo ok > "$1"`, "_", probeFile,
	)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		b, rerr := os.ReadFile(probeFile)
		if rerr == nil && strings.Contains(string(b), "ok") {
			h.OK = true
			h.Reason = sandboxOK
			h.Detail = "workspace-write probe write succeeded"
			return h
		}
		// Command succeeded but file missing — treat as probe failure.
		h.Reason = sandboxProbeFailed
		h.Detail = truncateRunes("probe command ok but probe file missing: "+text, 300)
		return h
	}
	h.Reason = classifySandboxError(text + "\n" + err.Error())
	h.Detail = truncateRunes(strings.TrimSpace(text+"\n"+err.Error()), 300)
	h.OK = false
	return h
}

// sandboxBrokenNotice is the TypeNotice body when sandboxed modes cannot write.
func sandboxBrokenNotice(h sandboxHealth) string {
	base := "Codex sandbox cannot write on this host (user namespace / bubblewrap). " +
		"Modes that use workspace-write will fail file edits. " +
		"Fix the host (see docs/config.md Codex sandbox / user namespaces), " +
		"or enable providers.codex.allow_full_access and switch to full-access, " +
		"or set sandbox_broken_policy."
	if h.Detail != "" {
		return base + " Detail: " + h.Detail
	}
	if h.Reason != "" && h.Reason != sandboxUnknown {
		return base + " Reason: " + string(h.Reason)
	}
	return base
}

// applySandboxBrokenPolicy adjusts create seed when the sandbox is unhealthy
// (MADR 0048 Phase 3). Returns error when create must fail.
func applySandboxBrokenPolicy(cfg Config, health sandboxHealth, approval, sandbox, modeID string) (string, string, string, error) {
	if health.OK || health.Reason == sandboxUnknown {
		return approval, sandbox, modeID, nil
	}
	policy := strings.TrimSpace(cfg.SandboxBrokenPolicy)
	if policy == "" {
		policy = sandboxPolicyWarn
	}
	switch policy {
	case sandboxPolicyWarn:
		return approval, sandbox, modeID, nil
	case sandboxPolicyRefuse:
		return "", "", "", fmt.Errorf("codex sandbox unavailable (%s): %s", health.Reason, health.Detail)
	case sandboxPolicyRequireFullAccess:
		if !cfg.AllowFullAccess {
			return "", "", "", fmt.Errorf(
				"codex sandbox unavailable (%s) and sandbox_broken_policy=require_full_access, "+
					"but allow_full_access is false — enable both or fix the host userns/bwrap setup",
				health.Reason,
			)
		}
		if m, ok := findCodexMode(cfg, modeFullAccess); ok {
			return m.approvalPolicy, m.sandbox, m.mode.ID, nil
		}
		return "never", "danger-full-access", modeFullAccess, nil
	default:
		// Unknown values rejected at config load; treat as warn if reached.
		return approval, sandbox, modeID, nil
	}
}

// emitSandboxBrokenNotice sends TypeNotice once per session when health is bad.
func (s *session) emitSandboxBrokenNotice(h sandboxHealth) {
	if h.OK || h.Reason == sandboxUnknown {
		return
	}
	s.mu.Lock()
	if s.sandboxNoticeSent {
		s.mu.Unlock()
		return
	}
	s.sandboxNoticeSent = true
	s.mu.Unlock()
	s.emit(event.Event{
		Type:      event.TypeNotice,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Text:      sandboxBrokenNotice(h),
	})
}

// maybePromoteSandboxFailure updates provider health from mid-turn text.
func (s *session) maybePromoteSandboxFailure(text string) {
	if !looksLikeSandboxNamespaceFailure(text) {
		return
	}
	if s.p == nil {
		return
	}
	s.p.noteSandboxFailure(text)
	s.emitSandboxBrokenNotice(s.p.sandboxHealth())
}
