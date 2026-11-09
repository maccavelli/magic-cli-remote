package gobble

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Phone provider-auth through gobble's own CLI (MADR 0179 D3, A4): gobble's
// store is written by `gobble auth login` and `logout`, with a key on the
// child's stdin and never in its argv, and read by `gobble auth status
// --json`, which names where each provider's credential comes from and never
// the credential. This package reads and writes no gobble file itself.

// statusTTL is how long one `auth status --json` answer serves, for every
// providers.list in that time. A write, a clear and a finished device flow
// drop it.
const statusTTL = 15 * time.Second

// commandTimeout bounds one short gobble auth command.
const commandTimeout = 10 * time.Second

// The method id suffixes, after the upstream id (as grok's catalog has them).
const (
	methodAPI     = ":api"
	methodDevice  = ":device"
	methodBrowser = ":browser"
)

// workspaceInput is Claude's optional workspace, passed as --workspace.
const workspaceInput = "workspace"

// The values of gobble's status rows this package reads (gobble-cli
// internal/cli/auth.go, authStatusRow, at v0.4.0).
const (
	sourceAuthJSON = "auth.json"
	authTypeAPIKey = "api_key"
	authTypeOAuth  = "oauth"
)

// statusRow is one key provider's row of `gobble auth status --json`.
type statusRow struct {
	Provider string `json:"provider"`
	Source   string `json:"source"`
	AuthType string `json:"authType"`
	Usable   bool   `json:"usable"`
}

// statusReport is `gobble auth status --json`'s output: the rows, or, when
// gobble cannot read its files, an error, with exit 2.
type statusReport struct {
	Providers []statusRow `json:"providers"`
	Error     *struct {
		File    string `json:"file"`
		Message string `json:"message"`
	} `json:"error"`
}

// loginEvent is one line of `gobble auth login --json`: device_code,
// browser_url, saved or error.
type loginEvent struct {
	Event   string `json:"event"`
	URL     string `json:"url"`
	Code    string `json:"code"`
	Expires string `json:"expires"`
	Message string `json:"message"`
}

// auth runs gobble's auth CLI for one provider instance.
type auth struct {
	bin string
	log *slog.Logger

	mu     sync.Mutex
	cached *statusReport
	at     time.Time
}

func newAuth(bin string, log *slog.Logger) *auth {
	if bin == "" {
		bin = "gobble"
	}
	if log == nil {
		log = slog.Default()
	}
	return &auth{bin: bin, log: log}
}

// drop forgets the cached status, after anything that changes it.
func (a *auth) drop() {
	a.mu.Lock()
	a.cached = nil
	a.mu.Unlock()
}

// report is the status, from the cache while it is fresh.
func (a *auth) report(ctx context.Context) (*statusReport, error) {
	a.mu.Lock()
	if a.cached != nil && time.Since(a.at) < statusTTL {
		r := a.cached
		a.mu.Unlock()
		return r, nil
	}
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, runErr := procutil.Command(ctx, a.bin, "auth", "status", "--json").Output()
	var r statusReport
	if err := json.Unmarshal(out, &r); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("gobble auth status: %w", runErr)
		}
		return nil, fmt.Errorf("gobble auth status: %w", err)
	}
	if r.Error == nil && runErr != nil {
		return nil, fmt.Errorf("gobble auth status: %w", runErr)
	}
	a.mu.Lock()
	a.cached, a.at = &r, time.Now()
	a.mu.Unlock()
	return &r, nil
}

// rows indexes a report's rows by provider.
func (r *statusReport) rows() map[string]statusRow {
	m := make(map[string]statusRow, len(r.Providers))
	for _, row := range r.Providers {
		m[row.Provider] = row
	}
	return m
}

// methods are an upstream's methods, with Configured from row when known.
// A method is configured only for a credential gobble keeps in auth.json of
// that method's kind: one from an environment variable or models.json makes
// the upstream ready, but is not this daemon's to remove (MADR 0179 A4).
func methods(p acpext.AuthProvider, row *statusRow) []provider.AuthMethod {
	api := provider.AuthMethod{ID: p.ID + methodAPI, Type: provider.AuthMethodAPIKey, Label: p.Name + " API key"}
	if p.ID == "claude" {
		api.Inputs = []provider.AuthInput{{
			Key: workspaceInput, Type: provider.AuthInputText,
			Message: "Anthropic workspace, for a key not scoped to one (optional)",
		}}
	}
	out := []provider.AuthMethod{api}
	if p.Login {
		out = append(out, provider.AuthMethod{
			ID: p.ID + methodDevice, Type: provider.AuthMethodOAuthDevice, Label: "Sign in to " + p.Name + " (device code)",
		})
	}
	if p.ID == "grok" {
		// gobble's browser sign-in opens a browser on the host (D3, W3).
		out = append(out, provider.AuthMethod{
			ID: p.ID + methodBrowser, Type: provider.AuthMethodOAuthBrowser, Label: "Sign in to " + p.Name,
			Unavailable: true, Reason: "host_oauth",
		})
	}
	if row == nil {
		return out
	}
	inFile := row.Source == sourceAuthJSON
	for i := range out {
		switch out[i].Type {
		case provider.AuthMethodAPIKey:
			out[i].Configured, out[i].ConfiguredKnown = inFile && row.AuthType == authTypeAPIKey, true
		case provider.AuthMethodOAuthDevice:
			out[i].Configured, out[i].ConfiguredKnown = inFile && row.AuthType == authTypeOAuth, true
		}
	}
	return out
}

// catalog is every upstream gobble can authenticate, in acpext's order
// (D3).
func (a *auth) catalog(context.Context) (provider.AuthCatalog, error) {
	var ups []provider.UpstreamAuth
	for _, p := range acpext.AuthProviders() {
		ups = append(ups, provider.UpstreamAuth{ID: p.ID, Label: p.Name, Methods: methods(p, nil)})
	}
	return provider.AuthCatalog{Upstreams: ups, Source: provider.AuthCatalogSourceStatic}, nil
}

// status is each upstream's presence, never a secret (D3, A4). A report gobble
// could not make is the whole state's error.
func (a *auth) status(ctx context.Context) (provider.AuthState, error) {
	r, err := a.report(ctx)
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	if r.Error != nil {
		return provider.AuthState{Status: provider.AuthError}, fmt.Errorf("gobble auth status: %s", r.Error.Message)
	}
	rows := r.rows()
	state := provider.AuthState{Status: provider.AuthMissing}
	for _, p := range acpext.AuthProviders() {
		up := provider.UpstreamAuth{ID: p.ID, Label: p.Name, Status: provider.AuthMissing}
		row, ok := rows[p.ID]
		if ok && row.Usable {
			up.Status, state.Status = provider.AuthConfigured, provider.AuthConfigured
		}
		if ok {
			up.Methods = methods(p, &row)
		} else {
			up.Methods = methods(p, nil)
		}
		state.Upstreams = append(state.Upstreams, up)
	}
	return state, nil
}

// readyUpstreams is the handshake's preference: the upstreams with a usable
// credential, in acpext's order, or none (D4).
func (a *auth) readyUpstreams(ctx context.Context) []string {
	r, err := a.report(ctx)
	if err != nil || r.Error != nil {
		return nil
	}
	rows := r.rows()
	var ids []string
	for _, p := range acpext.AuthProviders() {
		if rows[p.ID].Usable {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// hasCredential reports whether any upstream is ready (D4).
func (a *auth) hasCredential() bool {
	return len(a.readyUpstreams(context.Background())) > 0
}

// lookup is upstream id's AuthProvider.
func lookup(id string) (acpext.AuthProvider, bool) {
	for _, p := range acpext.AuthProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return acpext.AuthProvider{}, false
}

// failure is a failed login's message: its error event's, else its stderr's
// last line. It never holds the key, which only ever went to stdin.
func failure(stdout, stderr []byte, err error) error {
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	for sc.Scan() {
		var ev loginEvent
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Event == "error" && ev.Message != "" {
			return errors.New(ev.Message)
		}
	}
	if line := lastLine(stderr); line != "" {
		return fmt.Errorf("%s: %w", line, err)
	}
	return err
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// setCredential stores an API key through `gobble auth login <id> --api-key
// -`, the key on stdin (D3).
func (a *auth) setCredential(ctx context.Context, upstreamID, methodID, secret string, inputs map[string]string) error {
	if _, ok := lookup(upstreamID); !ok {
		return fmt.Errorf("gobble has no upstream %q", upstreamID)
	}
	if methodID != upstreamID+methodAPI {
		return fmt.Errorf("%w: %s", provider.ErrAuthMethodUnsupported, methodID)
	}
	args := []string{"auth", "login", upstreamID, "--api-key", "-", "--json"}
	if ws := strings.TrimSpace(inputs[workspaceInput]); ws != "" {
		args = append(args, "--workspace", ws)
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := procutil.Command(ctx, a.bin, args...)
	cmd.Stdin = strings.NewReader(secret + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	defer a.drop()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gobble auth login %s: %w", upstreamID, failure(stdout.Bytes(), stderr.Bytes(), err))
	}
	return nil
}

// clearCredential removes an upstream's auth.json entry (D3).
func (a *auth) clearCredential(ctx context.Context, upstreamID, _ string) error {
	if _, ok := lookup(upstreamID); !ok {
		return fmt.Errorf("gobble has no upstream %q", upstreamID)
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := procutil.Command(ctx, a.bin, "auth", "logout", upstreamID)
	cmd.Stderr = &stderr
	defer a.drop()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gobble auth logout %s: %w", upstreamID, failure(nil, stderr.Bytes(), err))
	}
	return nil
}

// startDeviceAuth runs `gobble auth login <id> --method device --json`: its
// device_code event is the phone's DeviceFlow, and wait follows the sign-in
// to its saved or error event (D3).
func (a *auth) startDeviceAuth(ctx context.Context, bin, upstreamID, methodID string, _ map[string]string, _ bool) (provider.DeviceFlow, func(context.Context) error, error) {
	p, ok := lookup(upstreamID)
	if !ok {
		return provider.DeviceFlow{}, nil, fmt.Errorf("gobble has no upstream %q", upstreamID)
	}
	if !p.Login || methodID != upstreamID+methodDevice {
		return provider.DeviceFlow{}, nil, fmt.Errorf("%w: %s", provider.ErrAuthMethodUnsupported, methodID)
	}
	if bin == "" {
		bin = a.bin
	}
	// The sign-in outlives this call: wait ends it, or its own context.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd := procutil.Command(runCtx, bin, "auth", "login", upstreamID, "--method", "device", "--json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return provider.DeviceFlow{}, nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return provider.DeviceFlow{}, nil, fmt.Errorf("gobble auth login %s: %w", upstreamID, err)
	}
	events := bufio.NewScanner(stdout)
	end := func() error {
		_, _ = io.Copy(io.Discard, stdout)
		err := cmd.Wait()
		cancel()
		a.drop()
		return err
	}

	flow, firstErr := firstDeviceCode(ctx, events)
	if firstErr != nil {
		_ = end()
		return provider.DeviceFlow{}, nil, fmt.Errorf("gobble auth login %s: %w", upstreamID, firstErr)
	}
	wait := func(wctx context.Context) error {
		stop := context.AfterFunc(wctx, cancel)
		defer stop()
		var failed error
		for events.Scan() {
			var ev loginEvent
			if json.Unmarshal(events.Bytes(), &ev) == nil && ev.Event == "error" {
				failed = errors.New(ev.Message)
			}
		}
		err := end()
		switch {
		case failed != nil:
			return fmt.Errorf("gobble auth login %s: %w", upstreamID, failed)
		case wctx.Err() != nil:
			return wctx.Err()
		case err != nil:
			return fmt.Errorf("gobble auth login %s: %w", upstreamID, failure(nil, stderr.Bytes(), err))
		}
		return nil
	}
	return flow, wait, nil
}

// firstDeviceCode reads events up to the device_code event, or the error
// that came instead.
func firstDeviceCode(ctx context.Context, events *bufio.Scanner) (provider.DeviceFlow, error) {
	for events.Scan() {
		var ev loginEvent
		if json.Unmarshal(events.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "device_code":
			flow := provider.DeviceFlow{VerificationURI: ev.URL, UserCode: ev.Code}
			if t, err := time.Parse(time.RFC3339, ev.Expires); err == nil {
				flow.ExpiresIn = max(0, int(time.Until(t).Seconds()))
			}
			return flow, nil
		case "error":
			return provider.DeviceFlow{}, errors.New(ev.Message)
		}
	}
	if err := ctx.Err(); err != nil {
		return provider.DeviceFlow{}, err
	}
	return provider.DeviceFlow{}, errors.New("the sign-in ended before it gave a device code")
}
