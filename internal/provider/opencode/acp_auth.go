package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// The phone's auth for OpenCode v2 runs on the control plane's integration and
// credential routes (0214 MADR D8, amended at P6b). An integration is one
// upstream vendor: its sign-in methods, and its connections, which are the
// stored credentials and any key the engine reads from its environment.
//
// Three facts of the engine shape it (0214 PLAN P6b Deviation 1):
//   - an integration exists only after plugins have activated in a location,
//     and only GET /api/integration waits for that, so every operation lists
//     first;
//   - a connect adds a credential and makes it active, and never replaces one,
//     so an add removes the older credentials of its kind afterwards and a
//     removal deletes them all;
//   - a running ACP engine does not see another process's credential change
//     (MADR amendment B, B6), so every accepted change restarts the open
//     sessions at their next idle point.
//
// The control plane never reads /api/credential, which returns secrets.
// Removal works from the credential ids integration.get lists.

// controlPlane is the control plane as the auth hooks use it, so tests can
// pass a fake engine instead of a process.
type controlPlane interface {
	Do(ctx context.Context, req control.Request) (control.Response, error)
}

const (
	// acpAuthReadTimeout bounds status and the catalog, which the provider
	// list waits on.
	acpAuthReadTimeout = 20 * time.Second
	// acpAuthWriteTimeout bounds an add, a removal, or an OAuth start: a few
	// local calls.
	acpAuthWriteTimeout = 30 * time.Second
	// acpListBound bounds the list call, which may start the control plane.
	// It runs apart from its caller, so a cold start (up to two minutes, A6)
	// that outlasts a short caller still finishes and serves the next one.
	acpListBound = 3 * time.Minute
	// acpCancelTimeout bounds cancelling an OAuth attempt.
	acpCancelTimeout = 10 * time.Second
)

// acpOAuthPoll is the wait between reads of an OAuth attempt's status; a
// variable so tests can shorten it.
var acpOAuthPoll = 2 * time.Second

// credentialChangedNotice is what each open session says before its agent
// restarts.
const credentialChangedNotice = "A provider credential changed; the agent restarts to apply it."

// hostOnlyReason is protocol.AuthReasonHostOAuth: the phone says "host only".
// Not imported, as in httpagent: protocol imports session, whose tests import
// the providers.
const hostOnlyReason = "host_oauth"

// acpAuth implements the Spec's auth hooks on a control plane.
type acpAuth struct {
	plane controlPlane
	log   *slog.Logger
	// restart restarts the provider's open sessions at their next idle point.
	restart func(notice string)
}

// integration is Integration.Info.
type integration struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Metadata    map[string]any      `json:"metadata"`
	Methods     []integrationMethod `json:"methods"`
	Connections []connection        `json:"connections"`
}

// integrationMethod is one of Integration.Method: key, oauth, command,
// external or env.
type integrationMethod struct {
	Type  string      `json:"type"`
	ID    string      `json:"id"`
	Label string      `json:"label"`
	Form  []formField `json:"form"`
}

// formField is Form.Field, as far as the phone can show it.
type formField struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Placeholder string `json:"placeholder"`
	Required    bool   `json:"required"`
	Hidden      bool   `json:"hidden"`
	Custom      bool   `json:"custom"`
	Default     any    `json:"default"`
	Options     []struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
	When []struct {
		Key   string `json:"key"`
		Op    string `json:"op"`
		Value any    `json:"value"`
	} `json:"when"`
}

// connection is Connection.Info: a stored credential, or an environment
// variable the engine reads.
type connection struct {
	Type string `json:"type"` // credential or env
	// ID, Label and Method are a credential's; Method is key, oauth or external.
	ID     string `json:"id"`
	Label  string `json:"label"`
	Method string `json:"method"`
	// Name is an env connection's variable.
	Name string `json:"name"`
}

// oauthAttempt is Integration.Attempt.
type oauthAttempt struct {
	AttemptID    string `json:"attemptID"`
	URL          string `json:"url"`
	Instructions string `json:"instructions"`
	Mode         string `json:"mode"`
	Time         struct {
		Expires int64 `json:"expires"`
	} `json:"time"`
}

// engineError is a control plane answer outside 2xx.
type engineError struct {
	method, path string
	status       int
	tag, message string
}

func (e *engineError) Error() string {
	msg := e.tag
	if e.message != "" {
		msg = e.tag + ": " + e.message
	}
	return fmt.Sprintf("opencode %s %s: %d %s", e.method, e.path, e.status, strings.TrimSpace(msg))
}

// call sends one request and decodes a 2xx answer's body into out.
func (a *acpAuth) call(ctx context.Context, method, path string, body, out any) error {
	res, err := a.plane.Do(ctx, control.Request{Method: method, Path: path, Body: body})
	if err != nil {
		return err
	}
	if res.Status < 200 || res.Status > 299 {
		e := &engineError{method: method, path: path, status: res.Status}
		var b struct {
			Tag     string `json:"_tag"`
			Message string `json:"message"`
		}
		if json.Unmarshal(res.Body, &b) == nil {
			e.tag, e.message = b.Tag, b.Message
		}
		return e
	}
	if out == nil || len(res.Body) == 0 {
		return nil
	}
	if err := json.Unmarshal(res.Body, out); err != nil {
		return fmt.Errorf("opencode %s %s: %w", method, path, err)
	}
	return nil
}

// list reads every integration. It is the call that waits for plugin
// activation, so it comes first in every operation. It runs apart from ctx,
// which only bounds the wait for it.
func (a *acpAuth) list(ctx context.Context) ([]integration, error) {
	type result struct {
		list []integration
		err  error
	}
	done := make(chan result, 1)
	go func() {
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpListBound)
		defer cancel()
		var out struct {
			Data []integration `json:"data"`
		}
		err := a.call(lctx, http.MethodGet, "/api/integration", nil, &out)
		done <- result{out.Data, err}
	}()
	select {
	case r := <-done:
		return r.list, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func integrationPath(id string) string { return "/api/integration/" + url.PathEscape(id) }

// get reads one integration and its connections, the active one first.
func (a *acpAuth) get(ctx context.Context, id string) (integration, error) {
	var out struct {
		Data integration `json:"data"`
	}
	err := a.call(ctx, http.MethodGet, integrationPath(id), nil, &out)
	return out.Data, err
}

// integration lists, then reads id.
func (a *acpAuth) integration(ctx context.Context, id string) (integration, error) {
	list, err := a.list(ctx)
	if err != nil {
		return integration{}, err
	}
	if !slices.ContainsFunc(list, func(in integration) bool { return in.ID == id }) {
		return integration{}, fmt.Errorf("opencode has no provider %q", id)
	}
	return a.get(ctx, id)
}

// isMCP reports an MCP server's sign-in, which is not a model upstream.
func (in integration) isMCP() bool {
	src, _ := in.Metadata["source"].(string)
	return src == "mcp"
}

// credentialIDs lists the stored credentials of a kind; "" is every kind.
func (in integration) credentialIDs(kind string) []string {
	var ids []string
	for _, c := range in.Connections {
		if c.Type == "credential" && (kind == "" || c.Method == kind) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// envNames lists the environment variables configuring the integration.
func (in integration) envNames() []string {
	var names []string
	for _, c := range in.Connections {
		if c.Type == "env" {
			names = append(names, c.Name)
		}
	}
	return names
}

// methodsOf counts the methods of a type.
func (in integration) methodsOf(typ string) int {
	n := 0
	for _, m := range in.Methods {
		if m.Type == typ {
			n++
		}
	}
	return n
}

// needsMoreThanAKey reports a sign-in the provider list should show before
// anything is configured: one that is not a bare key.
func (in integration) needsMoreThanAKey() bool {
	for _, m := range in.Methods {
		switch {
		case m.Type == "env":
		case m.Type == "key" && len(visible(m.Form)) == 0:
		default:
			return true
		}
	}
	return false
}

// upstream is the integration as the phone sees it.
func (in integration) upstream() provider.UpstreamAuth {
	up := provider.UpstreamAuth{ID: in.ID, Label: in.Name, Status: provider.AuthMissing}
	if up.Label == "" {
		up.Label = in.ID
	}
	if len(in.Connections) > 0 {
		up.Status = provider.AuthConfigured
	}
	for _, m := range in.Methods {
		am, ok := in.method(m)
		if ok {
			up.Methods = append(up.Methods, am)
		}
	}
	return up
}

// method is one sign-in as the phone sees it. A type the phone cannot drive is
// listed as host-only, so the phone never offers a button that cannot work. An
// env method is no sign-in: its connection shows as configured.
func (in integration) method(m integrationMethod) (provider.AuthMethod, bool) {
	inputs, answerable := formInputs(m.Form)
	switch m.Type {
	case "env":
		return provider.AuthMethod{}, false
	case "key":
		am := provider.AuthMethod{
			ID:              in.ID + ":key",
			Type:            provider.AuthMethodAPIKey,
			Label:           m.Label,
			Inputs:          inputs,
			ConfiguredKnown: true,
			Configured:      len(in.credentialIDs("key")) > 0,
		}
		if am.Label == "" {
			am.Label = "API key"
		}
		if !answerable {
			am.Unavailable, am.Reason = true, hostOnlyReason
		}
		return am, true
	case "oauth":
		// Which OAuth method stored a credential is not in its connection, so
		// it is known only when there is one.
		one := in.methodsOf("oauth") == 1
		am := provider.AuthMethod{
			ID:              in.ID + ":oauth:" + m.ID,
			Type:            provider.AuthMethodOAuthDevice,
			Label:           m.Label,
			Inputs:          inputs,
			ConfiguredKnown: one,
			Configured:      one && len(in.credentialIDs("oauth")) > 0,
		}
		if !answerable {
			am.Unavailable, am.Reason = true, hostOnlyReason
		}
		return am, true
	default:
		label := m.Label
		if label == "" {
			label = m.Type
		}
		return provider.AuthMethod{
			ID:          in.ID + ":" + m.Type + ":" + m.ID,
			Type:        provider.AuthMethodOAuthDevice,
			Label:       label,
			Unavailable: true,
			Reason:      hostOnlyReason,
		}, true
	}
}

// Every oauth method is listed oauth_device (MADR 0074 D30): no label or id
// decides the flow, the attempt's URL does when it starts (MADR 0074 D7), and
// a loopback URL is refused there with "signs in through a browser on the
// host".

// visible drops the fields the engine fills from their default.
func visible(fields []formField) []formField {
	var out []formField
	for _, f := range fields {
		if !f.Hidden && f.Type != "" && f.Key != "" {
			out = append(out, f)
		}
	}
	return out
}

// formInputs maps a form to the phone's inputs. answerable is false when a
// field is not a string, which the phone cannot send.
func formInputs(fields []formField) (inputs []provider.AuthInput, answerable bool) {
	for _, f := range visible(fields) {
		if f.Type != "string" {
			return nil, false
		}
		in := provider.AuthInput{
			Key:         f.Key,
			Type:        provider.AuthInputText,
			Message:     firstOf(f.Title, f.Description, f.Key),
			Placeholder: f.Placeholder,
			Required:    f.Required,
		}
		if d, ok := f.Default.(string); ok && in.Placeholder == "" {
			in.Placeholder = d
		}
		if len(f.Options) > 0 && !f.Custom {
			in.Type = provider.AuthInputSelect
			for _, o := range f.Options {
				in.Options = append(in.Options, provider.AuthInputOption{Value: o.Value, Label: o.Label, Hint: o.Description})
			}
		}
		// The phone holds one condition. With more, the field shows always,
		// which is the safe failure.
		if len(f.When) == 1 {
			w := f.When[0]
			in.When = &provider.AuthInputCondition{Key: w.Key, Op: w.Op, Value: fmt.Sprint(w.Value)}
		}
		inputs = append(inputs, in)
	}
	return inputs, true
}

func firstOf(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// answer is the form answer from the phone's inputs; nil when empty. An empty
// value is left out, so the engine applies the field's default.
func answer(inputs map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range inputs {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// status is AuthStatus: the integrations with a connection, and those whose
// sign-in is more than a bare key. The rest of the 237 are the catalog's.
func (a *acpAuth) status(ctx context.Context) (provider.AuthState, error) {
	ctx, cancel := context.WithTimeout(ctx, acpAuthReadTimeout)
	defer cancel()
	list, err := a.list(ctx)
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	st := provider.AuthState{Status: provider.AuthMissing}
	for _, in := range list {
		if in.isMCP() || (len(in.Connections) == 0 && !in.needsMoreThanAKey()) {
			continue
		}
		up := in.upstream()
		if up.Status == provider.AuthConfigured {
			st.Status = provider.AuthConfigured
		}
		st.Upstreams = append(st.Upstreams, up)
	}
	sort.Slice(st.Upstreams, func(i, j int) bool { return st.Upstreams[i].ID < st.Upstreams[j].ID })
	return st, nil
}

// catalog is AuthCatalogList: every integration but an MCP server's.
func (a *acpAuth) catalog(ctx context.Context) (provider.AuthCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, acpAuthReadTimeout)
	defer cancel()
	list, err := a.list(ctx)
	if err != nil {
		return provider.AuthCatalog{}, err
	}
	cat := provider.AuthCatalog{Source: provider.AuthCatalogSourceEngine}
	for _, in := range list {
		if !in.isMCP() {
			cat.Upstreams = append(cat.Upstreams, in.upstream())
		}
	}
	sort.Slice(cat.Upstreams, func(i, j int) bool { return cat.Upstreams[i].ID < cat.Upstreams[j].ID })
	return cat, nil
}

// setCredential stores a key through connect/key, confirms it is the active
// connection, and removes the older keys.
func (a *acpAuth) setCredential(ctx context.Context, upstreamID, methodID, secret string, inputs map[string]string) error {
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return errors.New("opencode set credential: upstream id required")
	}
	switch strings.TrimSpace(methodID) {
	// ":api" is the id 1.x listed, which a phone may still hold.
	case "", upstreamID + ":key", upstreamID + ":api":
	default:
		return fmt.Errorf("method %q is not %s's key: %w", methodID, upstreamID, provider.ErrAuthMethodUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, acpAuthWriteTimeout)
	defer cancel()
	in, err := a.integration(ctx, upstreamID)
	if err != nil {
		return err
	}
	if in.methodsOf("key") == 0 {
		return fmt.Errorf("%s takes no key: %w", upstreamID, provider.ErrAuthMethodUnsupported)
	}
	older := in.credentialIDs("key")
	body := map[string]any{"key": secret}
	if ans := answer(inputs); ans != nil {
		body["answer"] = ans
	}
	if err := a.call(ctx, http.MethodPost, integrationPath(upstreamID)+"/connect/key", body, nil); err != nil {
		// The engine's answer never echoes the key, and this adds nothing.
		return fmt.Errorf("opencode set credential for %s: %w", upstreamID, err)
	}
	defer a.restart(credentialChangedNotice)
	return a.adopt(ctx, upstreamID, "key", older)
}

// adopt confirms a new credential of kind is the active connection, then
// removes the older ones of that kind, and confirms they are gone.
func (a *acpAuth) adopt(ctx context.Context, id, kind string, older []string) error {
	after, err := a.get(ctx, id)
	if err != nil {
		return fmt.Errorf("confirm %s's new credential: %w", id, err)
	}
	if len(after.Connections) == 0 || after.Connections[0].Type != "credential" ||
		after.Connections[0].Method != kind || slices.Contains(older, after.Connections[0].ID) {
		return fmt.Errorf("%s's new credential is not the active one: %w", id, provider.ErrCredentialNotAccepted)
	}
	if len(older) == 0 {
		return nil
	}
	for _, cid := range older {
		if err := a.call(ctx, http.MethodDelete, "/api/credential/"+url.PathEscape(cid), nil, nil); err != nil {
			return fmt.Errorf("remove %s's replaced credential: %w", id, err)
		}
	}
	// DELETE answers 204 for an id it does not hold (B6), so the integration
	// says whether the removal happened.
	final, err := a.get(ctx, id)
	if err != nil {
		return fmt.Errorf("confirm %s's replaced credentials are gone: %w", id, err)
	}
	if left := slices.DeleteFunc(final.credentialIDs(kind), func(cid string) bool { return !slices.Contains(older, cid) }); len(left) > 0 {
		return fmt.Errorf("%s still holds %d replaced credential(s)", id, len(left))
	}
	return nil
}

// clearCredential deletes every stored credential of the integration and
// confirms none remains. A key from the environment cannot be removed, and the
// error names its variable.
func (a *acpAuth) clearCredential(ctx context.Context, upstreamID, _ string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return errors.New("opencode clear credential: upstream id required")
	}
	ctx, cancel := context.WithTimeout(ctx, acpAuthWriteTimeout)
	defer cancel()
	in, err := a.integration(ctx, upstreamID)
	if err != nil {
		return err
	}
	ids := in.credentialIDs("")
	if len(ids) == 0 {
		return envOnly(upstreamID, in.envNames())
	}
	removed := false
	defer func() {
		if removed {
			a.restart(credentialChangedNotice)
		}
	}()
	for _, cid := range ids {
		if err := a.call(ctx, http.MethodDelete, "/api/credential/"+url.PathEscape(cid), nil, nil); err != nil {
			return fmt.Errorf("opencode clear credential for %s: %w", upstreamID, err)
		}
		removed = true
	}
	after, err := a.get(ctx, upstreamID)
	if err != nil {
		return fmt.Errorf("confirm %s's credentials are gone: %w", upstreamID, err)
	}
	if left := after.credentialIDs(""); len(left) > 0 {
		return fmt.Errorf("%s still holds %d stored credential(s)", upstreamID, len(left))
	}
	return envOnly(upstreamID, after.envNames())
}

// envOnly is nil, or the error saying the environment still configures id.
func envOnly(id string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("%s is still configured through the environment variable %s, which Magic cannot remove",
		id, strings.Join(names, ", "))
}

// oauthMethod resolves the phone's method id, "<id>:oauth:<method>"; empty is
// the integration's only OAuth method.
func (in integration) oauthMethod(methodID string) (integrationMethod, error) {
	methodID = strings.TrimSpace(methodID)
	var only []integrationMethod
	for _, m := range in.Methods {
		if m.Type != "oauth" {
			continue
		}
		if methodID == in.ID+":oauth:"+m.ID {
			return m, nil
		}
		only = append(only, m)
	}
	if methodID == "" && len(only) == 1 {
		return only[0], nil
	}
	return integrationMethod{}, fmt.Errorf("method %q is not one of %s's sign-ins: %w",
		methodID, in.ID, provider.ErrAuthMethodUnsupported)
}

// startDeviceAuth starts an OAuth attempt through connect/oauth. Only a
// device flow is kept: one that redirects to the host's loopback, or wants a
// code pasted back, is cancelled and refused (MADR 0074 D7). The wait follows
// the attempt's own status, and on completion adopts the credential as an add
// does.
func (a *acpAuth) startDeviceAuth(ctx context.Context, _, upstreamID, methodID string, inputs map[string]string, _ bool) (provider.DeviceFlow, func(context.Context) error, error) {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return provider.DeviceFlow{}, nil, errors.New("opencode sign-in: upstream id required")
	}
	sctx, cancel := context.WithTimeout(ctx, acpAuthWriteTimeout)
	defer cancel()
	in, err := a.integration(sctx, upstreamID)
	if err != nil {
		return provider.DeviceFlow{}, nil, err
	}
	m, err := in.oauthMethod(methodID)
	if err != nil {
		return provider.DeviceFlow{}, nil, err
	}
	older := in.credentialIDs("oauth")
	body := map[string]any{"methodID": m.ID}
	if ans := answer(inputs); ans != nil {
		body["answer"] = ans
	}
	var out struct {
		Data oauthAttempt `json:"data"`
	}
	if err := a.call(sctx, http.MethodPost, integrationPath(upstreamID)+"/connect/oauth", body, &out); err != nil {
		return provider.DeviceFlow{}, nil, fmt.Errorf("opencode sign-in for %s: %w", upstreamID, err)
	}
	att := out.Data
	refuse := func(err error) (provider.DeviceFlow, func(context.Context) error, error) {
		a.cancelAttempt(upstreamID, att.AttemptID)
		return provider.DeviceFlow{}, nil, err
	}
	if att.Mode == "code" {
		return refuse(fmt.Errorf("%s's sign-in needs a code pasted back, which the phone cannot send yet: %w",
			upstreamID, provider.ErrAuthUnsupported))
	}
	cls := providerauth.Classify(att.URL, att.Instructions)
	switch cls.Kind {
	case providerauth.FlowDevice:
	case providerauth.FlowBrowser:
		return refuse(fmt.Errorf("%s signs in through a browser on the host, which the phone cannot reach yet: %w",
			upstreamID, provider.ErrAuthUnsupported))
	default:
		return refuse(fmt.Errorf("opencode sign-in for %s returned no usable code", upstreamID))
	}
	a.log.Info("device flow started", slog.String("agent", "opencode"), slog.String("upstream", upstreamID))
	expires := time.UnixMilli(att.Time.Expires)
	flow := provider.DeviceFlow{
		VerificationURI: cls.VerificationURI,
		UserCode:        cls.UserCode,
		ExpiresIn:       max(1, int(time.Until(expires).Seconds())),
		Interval:        max(1, int(acpOAuthPoll.Seconds())),
	}
	wait := func(wctx context.Context) error {
		return a.awaitAttempt(wctx, upstreamID, att.AttemptID, expires, older)
	}
	return flow, wait, nil
}

// awaitAttempt reads the attempt's status until it ends, the attempt expires,
// or ctx ends, which cancels the attempt.
func (a *acpAuth) awaitAttempt(ctx context.Context, id, attemptID string, expires time.Time, older []string) error {
	path := integrationPath(id) + "/connect/oauth/" + url.PathEscape(attemptID)
	tick := time.NewTicker(acpOAuthPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			a.cancelAttempt(id, attemptID)
			return ctx.Err()
		case <-tick.C:
		}
		var out struct {
			Data struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"data"`
		}
		err := a.call(ctx, http.MethodGet, path, nil, &out)
		var ee *engineError
		switch {
		case errors.As(err, &ee) && ee.status == http.StatusNotFound:
			return fmt.Errorf("opencode sign-in for %s: the attempt is gone: %w", id, err)
		case err != nil:
			// A failed read is not a failed sign-in; read again until it
			// expires.
			if time.Now().After(expires) {
				return fmt.Errorf("opencode sign-in for %s expired: %w", id, err)
			}
			a.log.Debug("opencode sign-in status unread", slog.String("upstream", id), slog.String("err", err.Error()))
			continue
		}
		switch out.Data.Status {
		case "complete":
			a.log.Info("device flow completed", slog.String("agent", "opencode"), slog.String("upstream", id))
			defer a.restart(credentialChangedNotice)
			return a.adopt(ctx, id, "oauth", older)
		case "failed":
			return fmt.Errorf("opencode sign-in for %s failed: %s", id, out.Data.Message)
		case "expired":
			return fmt.Errorf("opencode sign-in for %s expired", id)
		}
	}
}

// cancelAttempt ends an OAuth attempt on the engine, apart from any caller's
// context.
func (a *acpAuth) cancelAttempt(id, attemptID string) {
	if attemptID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), acpCancelTimeout)
	defer cancel()
	path := integrationPath(id) + "/connect/oauth/" + url.PathEscape(attemptID)
	if err := a.call(ctx, http.MethodDelete, path, nil, nil); err != nil {
		a.log.Warn("opencode sign-in not cancelled", slog.String("upstream", id), slog.String("err", err.Error()))
	}
}
