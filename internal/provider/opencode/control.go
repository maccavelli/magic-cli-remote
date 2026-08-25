package opencode

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// maxShareURLBytes bounds a forwarded share link.
const maxShareURLBytes = 2048

// validShareURL accepts only a bounded HTTPS URL with no userinfo and no
// fragment (MADR 0112 A8, PLAN P9 step 4).
//
// The link is something a user will open and may forward to others, so the
// rules are about what it is safe to hand onward: http is downgradeable,
// userinfo smuggles credentials into a shareable string, and a fragment can
// carry state the user cannot see in a preview.
func validShareURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty share url")
	}
	if len(trimmed) > maxShareURLBytes {
		return "", fmt.Errorf("share url exceeds %d bytes", maxShareURLBytes)
	}
	if strings.ContainsAny(trimmed, " \t\r\n\x00") {
		return "", fmt.Errorf("share url has unexpected characters")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("share url is unparseable")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("share url scheme %q is not https", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("share url carries userinfo")
	}
	if u.Host == "" {
		return "", fmt.Errorf("share url has no host")
	}
	if u.Fragment != "" || strings.Contains(trimmed, "#") {
		return "", fmt.Errorf("share url carries a fragment")
	}
	return u.String(), nil
}

// CurrentShare implements [provider.ShareSession].
//
// This is a read and is deliberately available even when mutation policy is
// off: a session shared from the desktop is public whether or not this daemon
// may change that, and hiding it would be the more dangerous silence.
func (o *httpSession) CurrentShare(ctx context.Context) (provider.ShareState, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var info struct {
		Share *struct {
			URL string `json:"url"`
		} `json:"share"`
	}
	if err := o.h.API()(callCtx, "GET", "/session/"+o.h.AgentSessionID()+o.dir(), nil, &info); err != nil {
		return provider.ShareState{}, err
	}
	if info.Share == nil {
		return provider.ShareState{}, nil
	}
	// An unusable link is reported as shared-without-a-link rather than
	// discarded: the transcript is still public, and saying otherwise would
	// understate the exposure.
	link, err := validShareURL(info.Share.URL)
	if err != nil {
		o.h.Log().Warn("opencode share url failed validation; reporting shared without a link")
		return provider.ShareState{Shared: true}, nil
	}
	return provider.ShareState{Shared: true, URL: link}, nil
}

// Share implements [provider.ShareSession].
//
// Refused before any HTTP call when the operator has not enabled it, so a
// disabled daemon makes zero mutation requests. There is no retry: a retried
// share can publish twice, and the caller can always ask again deliberately.
func (o *httpSession) Share(ctx context.Context) (provider.ShareState, error) {
	if !o.d.allowRemoteShare {
		return provider.ShareState{}, provider.ErrShareDisabled
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var out struct {
		URL   string `json:"url"`
		Share *struct {
			URL string `json:"url"`
		} `json:"share"`
	}
	if err := o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/share"+o.dir(), map[string]any{}, &out); err != nil {
		return provider.ShareState{}, err
	}
	raw := out.URL
	if raw == "" && out.Share != nil {
		raw = out.Share.URL
	}
	link, err := validShareURL(raw)
	if err != nil {
		// Never surface an unvalidated link. The share may well have happened
		// upstream, so this reports shared-without-a-link rather than failing
		// in a way that invites a retry that would publish again.
		o.h.Log().Warn("opencode share returned a url that failed validation")
		return provider.ShareState{Shared: true}, nil
	}
	return provider.ShareState{Shared: true, URL: link}, nil
}

// Unshare implements [provider.ShareSession].
func (o *httpSession) Unshare(ctx context.Context) error {
	if !o.d.allowRemoteShare {
		return provider.ErrShareDisabled
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return o.h.API()(callCtx, "DELETE",
		"/session/"+o.h.AgentSessionID()+"/share"+o.dir(), nil, nil)
}

// ShareMutationAllowed reports the operator policy for this provider.
//
// Exported because httpagent's optional interface is satisfied from this
// package: an interface carrying an unexported method can only ever be
// implemented inside the package that declares it, which would silently exclude
// every real dialect.
func (o *httpSession) ShareMutationAllowed() bool { return o.d.allowRemoteShare }

// Compile-time check: the dialect half satisfies the share contract httpagent
// forwards.
var _ interface {
	CurrentShare(context.Context) (provider.ShareState, error)
	Share(context.Context) (provider.ShareState, error)
	Unshare(context.Context) error
} = (*httpSession)(nil)

// Shell command bounds (MADR 0112 A9, PLAN P10 step 2).
const (
	maxShellCommandBytes = 8192
	// shellTimeout bounds one blocking submission. It is generous because a
	// real command can legitimately take minutes; it is not a containment
	// mechanism, and the surface says so.
	shellTimeout = 30 * time.Minute
)

// validShellCommand checks what can be checked before submission.
//
// Deliberately no attempt to parse or constrain shell semantics: a command can
// create persistent filesystem and network effects, spawn descendants that
// outlive it, and none of that is knowable from the string. Pretending to
// sanitize it would be worse than admitting the surface is remote execution
// (MADR 0112 A9).
func validShellCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("%w: empty command", provider.ErrShellInvalid)
	}
	if len(cmd) > maxShellCommandBytes {
		return fmt.Errorf("%w: command exceeds %d bytes", provider.ErrShellInvalid, maxShellCommandBytes)
	}
	if strings.ContainsRune(cmd, 0) {
		return fmt.Errorf("%w: command contains NUL", provider.ErrShellInvalid)
	}
	if !utf8.ValidString(cmd) {
		return fmt.Errorf("%w: command is not valid UTF-8", provider.ErrShellInvalid)
	}
	return nil
}

// shellAgent resolves the agent the command runs under.
//
// The synthetic `auto` mode is not an upstream agent ID and must never be sent
// as one; it resolves to the same normal agent auto mode itself runs under. The
// result must be a visible primary agent — a subagent or hidden agent cannot
// accept a top-level turn, and sending one would fail upstream in a way that
// looks like the command was rejected (PLAN P10 step 1).
func (o *httpSession) shellAgent(ctx context.Context) (string, error) {
	modes, current := o.sessionModes(ctx)
	agent := current
	if strings.EqualFold(agent, autoModeID) || agent == "" {
		agent = normalAgentID(modes)
	}
	if agent == "" {
		return "", fmt.Errorf("%w: no primary agent is available", provider.ErrShellInvalid)
	}
	for _, m := range modes {
		if m.ID == agent && !strings.EqualFold(m.ID, autoModeID) {
			return agent, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not a visible primary agent", provider.ErrShellInvalid, agent)
}

// Shell implements [provider.ShellSession].
//
// The blocking response is deliberately discarded (out is nil): OpenCode emits
// the synthetic user message and the shell tool part over global SSE, and
// mapping the response as well would render the same command twice (PLAN P10
// step 6).
//
// There is no retry on any outcome. A timed-out command may still be running,
// and re-submitting would start a second one.
func (o *httpSession) Shell(ctx context.Context, command string) error {
	if !o.d.allowRemoteShell {
		return provider.ErrShellDisabled
	}
	if err := validShellCommand(command); err != nil {
		return err
	}
	agent, err := o.shellAgent(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{
		"agent":   agent,
		"command": command,
	}
	if mp, mid := o.resolveModel(); mid != "" {
		// The shell route takes a ModelRef object, not the free-form string the
		// command route accepts. Sending a string is an HTTP 400 (verified
		// against 1.18.21), so the two must not be assumed interchangeable.
		body["model"] = map[string]string{"providerID": mp, "modelID": mid}
	}
	callCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	// The command is never logged, here or anywhere: a daemon log pairing
	// "shell allowed" with the command that ran is a more useful artefact to an
	// attacker than either alone.
	o.h.Log().Info("opencode shell submitted",
		slog.String("agent_session_id", o.h.AgentSessionID()),
		slog.String("agent", agent))
	return o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/shell"+o.dir(), body, nil)
}

// ShellAllowed reports the operator policy for this provider. Exported for the
// same reason as ShareMutationAllowed.
func (o *httpSession) ShellAllowed() bool { return o.d.allowRemoteShell }

// Compile-time check: the dialect half satisfies the shell contract httpagent
// forwards.
var _ interface {
	Shell(context.Context, string) error
} = (*httpSession)(nil)
