package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

func TestWSAuthAndFakeSession(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())

	var srv *ws.Server
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		if srv != nil {
			srv.BroadcastEvent(ev)
		}
	})
	srv = ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// healthz unauthenticated
	res, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("healthz status %d", res.StatusCode)
	}

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// auth
	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s", got.Type)
	}

	// create session (may interleave with pushed session_status events)
	createEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "2", protocol.SessionCreatePayload{
		Provider: "fake",
		Name:     "demo",
		Model:    "m-test",
	})
	writeEnv(ctx, t, conn, createEnv)
	var meta session.Meta
	for {
		got = readEnv(ctx, t, conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeSessionCreated {
			t.Fatalf("want session.created got %s payload=%s", got.Type, string(got.Payload))
		}
		if err := json.Unmarshal(got.Payload, &meta); err != nil {
			t.Fatal(err)
		}
		break
	}
	// The requested model must flow through StartOptions into the session meta.
	if meta.Model != "m-test" {
		t.Fatalf("meta.Model = %q, want m-test", meta.Model)
	}

	// prompt (may interleave with pushed events)
	promptEnv, _ := protocol.NewEnvelope(protocol.TypeSessionPrompt, "3", protocol.SessionPromptPayload{
		SessionID: meta.ID,
		Text:      "hi",
	})
	writeEnv(ctx, t, conn, promptEnv)

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	var sawOK, sawChunk bool
	for !sawOK || !sawChunk {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			break
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case protocol.TypeOK:
			if env.ID == "3" {
				sawOK = true
			}
		case protocol.TypeEvent:
			var ep protocol.EventPayload
			_ = json.Unmarshal(env.Payload, &ep)
			if ep.Event.Type == event.TypeAssistantChunk {
				sawChunk = true
			}
		}
	}
	if !sawOK {
		t.Fatal("expected ok for prompt")
	}
	if !sawChunk {
		t.Fatal("expected assistant_message_chunk event")
	}
}

// The slash-command catalog leads with the canonical commands the daemon offers,
// marked by what the provider can do, so a picker never presents a command that
// would only answer with an apology (MADR 0023).
func TestWSCommandsListIncludesCanonicalCommands(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	listEnv, _ := protocol.NewEnvelope(protocol.TypeCommandsList, "2",
		protocol.CommandsListPayload{Provider: "fake"})
	writeEnv(ctx, t, conn, listEnv)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeCommandsResult {
		t.Fatalf("want commands.list_result got %s payload=%s", got.Type, string(got.Payload))
	}
	var res protocol.CommandsResultPayload
	if err := json.Unmarshal(got.Payload, &res); err != nil {
		t.Fatal(err)
	}
	byID := map[string]picker.Option{}
	for _, o := range res.Options {
		byID[o.ID] = o
	}
	// Daemon-side commands work on any session of any provider.
	for _, want := range []string{"/help", "/new", "/sessions"} {
		o, ok := byID[want]
		if !ok {
			t.Fatalf("%s missing from the catalog: %+v", want, res.Options)
		}
		if !o.IsEnabled() {
			t.Errorf("%s should be enabled everywhere", want)
		}
	}
	// Without a session id, session-dependent commands are listed but disabled,
	// with the reason in the description rather than silently omitted.
	ctxCmd, ok := byID["/context"]
	if !ok {
		t.Fatalf("/context missing from the catalog: %+v", res.Options)
	}
	if ctxCmd.IsEnabled() {
		t.Error("/context cannot be known to work without a session")
	}
	if !strings.Contains(ctxCmd.Description, "—") {
		t.Errorf("/context disabled without a reason: %q", ctxCmd.Description)
	}
}

func TestWSModelsList(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	listEnv, _ := protocol.NewEnvelope(protocol.TypeModelsList, "2", protocol.ModelsListPayload{Provider: "fake"})
	writeEnv(ctx, t, conn, listEnv)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeModelsResult {
		t.Fatalf("want models.list_result got %s payload=%s", got.Type, string(got.Payload))
	}
	var res protocol.ModelsResultPayload
	if err := json.Unmarshal(got.Payload, &res); err != nil {
		t.Fatal(err)
	}
	if res.Provider != "fake" || res.Kind != "single" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !res.AllowCustom {
		t.Fatal("expected allow_custom")
	}
	if len(res.Options) < 1 {
		t.Fatal("expected fake catalog options")
	}
	found := false
	for _, o := range res.Options {
		if o.ID == "fake-echo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing fake-echo in %+v", res.Options)
	}

	// unknown provider
	bad, _ := protocol.NewEnvelope(protocol.TypeModelsList, "3", protocol.ModelsListPayload{Provider: "nope"})
	writeEnv(ctx, t, conn, bad)
	got = readEnv(ctx, t, conn)
	if got.Type != protocol.TypeError {
		t.Fatalf("want error got %s", got.Type)
	}
}

func TestWSSessionHistoryReplay(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())

	var srv *ws.Server
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		if srv != nil {
			srv.BroadcastEvent(ev)
		}
	})
	srv = ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s", got.Type)
	}

	createEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "2", protocol.SessionCreatePayload{Provider: "fake"})
	writeEnv(ctx, t, conn, createEnv)

	// Consume live events until the turn completes so the buffer is populated.
	// Capture the live user_message event to compare shapes below.
	var meta session.Meta
	var liveUserMsg *event.Event
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatalf("read: %v (meta=%+v)", err, meta)
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case protocol.TypeSessionCreated:
			_ = json.Unmarshal(env.Payload, &meta)
			// Send the prompt now that the session id is known.
			promptEnv, _ := protocol.NewEnvelope(protocol.TypeSessionPrompt, "3", protocol.SessionPromptPayload{
				SessionID: meta.ID,
				Text:      "hi",
			})
			writeEnv(ctx, t, conn, promptEnv)
		case protocol.TypeEvent:
			var ep protocol.EventPayload
			_ = json.Unmarshal(env.Payload, &ep)
			if ep.Event.Type == event.TypeUserMessage && ep.Event.Text == "hi" {
				ev := ep.Event
				liveUserMsg = &ev
			}
			if ep.Event.Type == event.TypeTurnComplete {
				goto replay
			}
		}
	}

replay:
	if liveUserMsg == nil {
		t.Fatal("never observed a live user_message event")
	}

	histEnv, _ := protocol.NewEnvelope(protocol.TypeSessionHistory, "4", protocol.SessionIDPayload{SessionID: meta.ID})
	writeEnv(ctx, t, conn, histEnv)

	var hist protocol.SessionHistoryResultPayload
	for {
		got := readEnv(ctx, t, conn)
		if got.Type == protocol.TypeEvent {
			continue // late live events may still arrive
		}
		if got.Type != protocol.TypeSessionHistoryResult {
			t.Fatalf("want session.history_result got %s", got.Type)
		}
		if err := json.Unmarshal(got.Payload, &hist); err != nil {
			t.Fatal(err)
		}
		break
	}

	if hist.SessionID != meta.ID {
		t.Fatalf("history session id = %q want %q", hist.SessionID, meta.ID)
	}
	if len(hist.Events) == 0 {
		t.Fatal("expected buffered events, got none")
	}
	// Events must be ordered and contain the whole turn.
	var histUserMsg *event.Event
	var sawChunk bool
	for i := range hist.Events {
		e := hist.Events[i]
		if e.Type == event.TypeUserMessage && e.Text == "hi" {
			histUserMsg = &hist.Events[i]
		}
		if e.Type == event.TypeAssistantChunk {
			sawChunk = true
		}
	}
	if histUserMsg == nil {
		t.Fatal("history missing the user_message event")
	}
	if !sawChunk {
		t.Fatal("history missing assistant chunks (chunks are the point of replay)")
	}

	// Same-shape guarantee: a history event must marshal to identical JSON as
	// the corresponding live event of the same type.
	liveJSON, _ := json.Marshal(*liveUserMsg)
	histJSON, _ := json.Marshal(*histUserMsg)
	if string(liveJSON) != string(histJSON) {
		t.Fatalf("history event JSON differs from live event JSON:\n live: %s\n hist: %s", liveJSON, histJSON)
	}

	// Unknown session -> empty (non-nil) events list, not an error.
	unkEnv, _ := protocol.NewEnvelope(protocol.TypeSessionHistory, "5", protocol.SessionIDPayload{SessionID: "no-such"})
	writeEnv(ctx, t, conn, unkEnv)
	var unk protocol.SessionHistoryResultPayload
	for {
		got := readEnv(ctx, t, conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeSessionHistoryResult {
			t.Fatalf("want session.history_result got %s", got.Type)
		}
		if err := json.Unmarshal(got.Payload, &unk); err != nil {
			t.Fatal(err)
		}
		break
	}
	if unk.Events == nil || len(unk.Events) != 0 {
		t.Fatalf("unknown session events = %v, want empty non-nil", unk.Events)
	}
}

func TestWSPairClaim(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	codes, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := codes.Create("phone", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		PairCodes:          codes,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	claim, _ := protocol.NewEnvelope(protocol.TypePairClaim, "1", protocol.PairClaimPayload{
		Code: info.Display,
	})
	writeEnv(ctx, t, conn, claim)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypePairOK {
		t.Fatalf("want pair_ok got %s payload=%s", got.Type, string(got.Payload))
	}
	var ok protocol.PairOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.Token == "" || ok.DeviceID == "" {
		t.Fatalf("empty token/device: %+v", ok)
	}

	// Socket is authed — session.list should work without a second auth.
	listEnv, _ := protocol.NewEnvelope(protocol.TypeSessionList, "2", map[string]any{})
	writeEnv(ctx, t, conn, listEnv)
	got = readEnv(ctx, t, conn)
	if got.Type != protocol.TypeSessionListResult {
		t.Fatalf("want session.list_result got %s", got.Type)
	}

	// Code is one-shot.
	conn2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")
	writeEnv(ctx, t, conn2, claim)
	got = readEnv(ctx, t, conn2)
	if got.Type != protocol.TypePairError {
		t.Fatalf("want pair_error on reuse, got %s", got.Type)
	}
}

// Sticky auth (Phase 1.4): a second token on the same socket must not flip the
// device identity under in-flight async ops.
func TestWSStickyAuthRejectsSecondDevice(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, tokA, err := store.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	_, tokB, err := store.Create("b")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authA, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: tokA})
	writeEnv(ctx, t, conn, authA)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s", got.Type)
	}

	authB, _ := protocol.NewEnvelope(protocol.TypeAuth, "2", protocol.AuthPayload{Token: tokB})
	writeEnv(ctx, t, conn, authB)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthError {
		t.Fatalf("want auth_error for second device, got %s", got.Type)
	}
	var ep protocol.ErrorPayload
	if err := json.Unmarshal(got.Payload, &ep); err != nil {
		t.Fatal(err)
	}
	if ep.Code != "already_authed" {
		t.Fatalf("code=%q want already_authed", ep.Code)
	}

	// Same device may re-auth (token refresh style).
	writeEnv(ctx, t, conn, authA)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("re-auth same device: want auth_ok got %s", got.Type)
	}
}

// blockingProvider holds Start until release is closed (Phase 0.2 / 1.2).
type blockingProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingProvider) ID() provider.ID { return "block" }
func (p *blockingProvider) Ready() bool     { return true }

func (p *blockingProvider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return fake.New().Start(ctx, opts)
}

// Async create is capped per connection: a third in-flight create while two
// are blocked in Start must return rate_limited.
func TestWSAsyncCreateRateLimited(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}

	bp := &blockingProvider{
		entered: make(chan struct{}, 8),
		release: make(chan struct{}),
	}
	reg := provider.NewRegistry()
	reg.Register(bp)
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "a", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	c1, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "c1", protocol.SessionCreatePayload{
		Provider: "block", SessionID: "sess-a", Name: "n",
	})
	c2, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "c2", protocol.SessionCreatePayload{
		Provider: "block", SessionID: "sess-b", Name: "n",
	})
	c3, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "c3", protocol.SessionCreatePayload{
		Provider: "block", SessionID: "sess-c", Name: "n",
	})
	writeEnv(ctx, t, conn, c1)
	writeEnv(ctx, t, conn, c2)

	// Wait until both Starts are inside the provider.
	for i := 0; i < 2; i++ {
		select {
		case <-bp.entered:
		case <-ctx.Done():
			t.Fatal("timeout waiting for blocked Start")
		}
	}

	writeEnv(ctx, t, conn, c3)
	// Drain until rate_limited error (may interleave with nothing else yet).
	var sawRateLimit bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawRateLimit {
		readCtx, cancelRead := context.WithTimeout(ctx, 500*time.Millisecond)
		_, data, err := conn.Read(readCtx)
		cancelRead()
		if err != nil {
			continue
		}
		var env protocol.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if env.Type == protocol.TypeError {
			var ep protocol.ErrorPayload
			_ = json.Unmarshal(env.Payload, &ep)
			if ep.Code == "rate_limited" {
				sawRateLimit = true
			}
		}
	}
	close(bp.release)
	if !sawRateLimit {
		t.Fatal("expected rate_limited when a third create arrives with 2 in flight")
	}
}

func TestDisconnectDeviceClosesAuthedConn(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	dev, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s", got.Type)
	}

	n := srv.DisconnectDevice(dev.ID)
	if n != 1 {
		t.Fatalf("DisconnectDevice closed %d, want 1", n)
	}

	// Next read should fail because the server closed the socket.
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, _, err = conn.Read(readCtx)
	if err == nil {
		t.Fatal("expected read error after DisconnectDevice")
	}
}

func writeEnv(ctx context.Context, t *testing.T, conn *websocket.Conn, env protocol.Envelope) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func readEnv(ctx context.Context, t *testing.T, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestHTTPEndpointAuth(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}

	srv := ws.New(ws.Options{
		Store:              store,
		Sessions:           session.NewManager(provider.NewRegistry(), nil, nil, nil),
		Registry:           provider.NewRegistry(),
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "100.64.0.1:7531",
		HeadscaleURL:       "https://headscale.example",
	})
	srv.SetTLSStatus("letsencrypt", true)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	get := func(t *testing.T, path, token string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body
	}

	t.Run("hello requires a device token", func(t *testing.T) {
		for _, tok := range []string{"", "mcr_not-a-real-token"} {
			code, body := get(t, "/v1/hello", tok)
			if code != http.StatusUnauthorized {
				t.Fatalf("token %q: status %d want 401", tok, code)
			}
			for _, leak := range []string{"headscale_control_url", "listen", "version", "protocol", "tls_mode"} {
				if _, ok := body[leak]; ok {
					t.Fatalf("401 body discloses %q: %v", leak, body)
				}
			}
		}
	})

	t.Run("hello with a valid token returns the payload", func(t *testing.T) {
		code, body := get(t, "/v1/hello", token)
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if body["headscale_control_url"] != "https://headscale.example" {
			t.Fatalf("body=%v", body)
		}
		if body["listen"] != "100.64.0.1:7531" || body["version"] != "test" {
			t.Fatalf("body=%v", body)
		}
		if body["tls_mode"] != "letsencrypt" || body["tls_fell_back"] != true {
			t.Fatalf("tls status not surfaced: %v", body)
		}
	})

	t.Run("healthz is unauthenticated liveness only", func(t *testing.T) {
		code, body := get(t, "/healthz", "")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if body["ok"] != true {
			t.Fatalf("body=%v", body)
		}
		if len(body) != 1 {
			t.Fatalf("healthz discloses more than liveness: %v", body)
		}
	})
}

func TestWSIdleTimeout(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	srv := ws.New(ws.Options{
		Store:              store,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
		ReadDeadline:       50 * time.Millisecond,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s", got.Type)
	}

	// Read should error out after the 50ms read deadline passes because no further messages are sent.
	readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
	defer readCancel()
	_, _, err = conn.Read(readCtx)
	if err == nil {
		t.Fatal("expected connection to be closed by server due to idle timeout, but read succeeded")
	}
}
