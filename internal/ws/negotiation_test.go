package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// MADR 0068 P0 (U1/U2) — version negotiation. v1 clients must see
// byte-identical behaviour; v2 offers negotiate capabilities.

func newNegotiationServer(t *testing.T) (string, string) {
	return newNegotiationServerWith(t, "")
}

func newNegotiationServerWith(t *testing.T, displayName string) (string, string) {
	t.Helper()
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
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
		DisplayName:        displayName,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + ts.URL[len("http"):] + "/v1/ws", token
}

func dialNegotiation(ctx context.Context, t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// U1: an auth with no protocols offer is a v1 client — auth_ok must carry
// exactly the v1 keys, nothing new, and the envelope stays v:1.
func TestV1AuthOKIsByteIdentical(t *testing.T) {
	url, token := newNegotiationServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)

	// Raw read: the assertion is about wire bytes, not decoded structs.
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	if v := string(frame["v"]); v != "1" {
		t.Fatalf("v1 auth_ok envelope v = %s, want 1", v)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"protocol", "caps"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("v1 auth_ok payload contains %q: %s", forbidden, frame["payload"])
		}
	}
	for _, required := range []string{"device_id", "device_name"} {
		if _, ok := payload[required]; !ok {
			t.Fatalf("v1 auth_ok payload missing %q: %s", required, frame["payload"])
		}
	}

	// And the connection still enforces strict v1 framing.
	bad := `{"v":2,"type":"ping","id":"p1"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(bad)); err != nil {
		t.Fatal(err)
	}
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeError || !strings.Contains(string(got.Payload), "bad_version") {
		t.Fatalf("v:2 frame on v1 connection: got %s %s", got.Type, string(got.Payload))
	}
}

// U2: offering [1,2] negotiates 2, returns the capability block with values
// matching the enforced configuration, and widens the envelope gate.
func TestV2NegotiationAndCaps(t *testing.T) {
	url, token := newNegotiationServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{
		Token:     token,
		Protocols: []int{1, 2},
	})
	writeEnv(ctx, t, conn, env)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s %s", got.Type, string(got.Payload))
	}
	if got.V != protocol.V2 {
		t.Fatalf("v2 auth_ok envelope v = %d, want 2", got.V)
	}
	var ok protocol.AuthOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.Protocol != protocol.V2 {
		t.Fatalf("negotiated protocol = %d, want 2", ok.Protocol)
	}
	caps := ok.Caps
	if caps == nil {
		t.Fatal("v2 auth_ok missing caps")
	}
	// Advertised == enforced: the default read deadline is 120 s and the
	// 0063 app-ping cadence is 10 s.
	if caps.ReadDeadlineMS != 120_000 {
		t.Fatalf("caps.read_deadline_ms = %d, want 120000", caps.ReadDeadlineMS)
	}
	if caps.PingIntervalMS != 10_000 {
		t.Fatalf("caps.ping_interval_ms = %d, want 10000", caps.PingIntervalMS)
	}
	if !caps.WSPingResetsDeadline {
		t.Fatal("caps.ws_ping_resets_deadline false — P1 made transport pongs count")
	}
	// P4 attaches resume on every v2 auth; advertised == enforced means the
	// window here must be the config default the store actually applies.
	if caps.Resume == nil {
		t.Fatal("v2 auth_ok missing caps.resume — P4 ships resume support")
	}
	if caps.Resume.WindowMS != 120_000 {
		t.Fatalf(
			"caps.resume.window_ms = %d, want 120000",
			caps.Resume.WindowMS,
		)
	}
	if caps.HistoryRing != session.HistoryRingCap {
		t.Fatalf("caps.history_ring = %d, want %d", caps.HistoryRing, session.HistoryRingCap)
	}
	if caps.MaxFrameBytes != 1<<20 {
		t.Fatalf("caps.max_frame_bytes = %d, want %d", caps.MaxFrameBytes, 1<<20)
	}
	if caps.TLSResumed {
		t.Fatal("caps.tls_resumed true on a plaintext httptest connection")
	}

	// The gate now accepts v:2 frames…
	ping := `{"v":2,"type":"ping","id":"p1"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(ping)); err != nil {
		t.Fatal(err)
	}
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypePong {
		t.Fatalf("v:2 ping after negotiation: got %s", got.Type)
	}
	// …and v:1 frames (shared fan-out rule) — but not v:3.
	ping1 := `{"v":1,"type":"ping","id":"p2"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(ping1)); err != nil {
		t.Fatal(err)
	}
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypePong {
		t.Fatalf("v:1 ping after negotiation: got %s", got.Type)
	}
	ping3 := `{"v":3,"type":"ping","id":"p3"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(ping3)); err != nil {
		t.Fatal(err)
	}
	got = readEnv(ctx, t, conn)
	if got.Type != protocol.TypeError || !strings.Contains(string(got.Payload), "bad_version") {
		t.Fatalf("v:3 frame: got %s %s", got.Type, string(got.Payload))
	}
}

// U2: an offer with no mutual version is rejected without burning an auth
// strike, and the connection remains usable for a corrected offer.
func TestNoMutualVersionRejectedThenRecovers(t *testing.T) {
	url, token := newNegotiationServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{
		Token:     token,
		Protocols: []int{3},
	})
	writeEnv(ctx, t, conn, env)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeError || !strings.Contains(string(got.Payload), "bad_version") {
		t.Fatalf("offer [3]: got %s %s", got.Type, string(got.Payload))
	}

	retry, _ := protocol.NewEnvelope(protocol.TypeAuth, "2", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, retry)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("v1 retry after bad offer: got %s %s", got.Type, string(got.Payload))
	}
}

func decodeAuthOKPayloadMap(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestAuthOKCarriesDisplayName(t *testing.T) {
	url, token := newNegotiationServerWith(t, "Studio Mac")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeAuthOKPayloadMap(t, raw)
	var name string
	if err := json.Unmarshal(payload["display_name"], &name); err != nil {
		t.Fatalf("display_name: %v (payload %s)", err, payload["display_name"])
	}
	if name != "Studio Mac" {
		t.Fatalf("display_name=%q", name)
	}
}

func TestV2AuthOKCarriesDisplayName(t *testing.T) {
	url, token := newNegotiationServerWith(t, "Studio Mac")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{
		Token:     token,
		Protocols: []int{1, 2},
	})
	writeEnv(ctx, t, conn, env)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s %s", got.Type, string(got.Payload))
	}
	var ok protocol.AuthOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.DisplayName != "Studio Mac" {
		t.Fatalf("DisplayName=%q", ok.DisplayName)
	}
	if ok.Protocol != protocol.V2 {
		t.Fatalf("negotiated protocol = %d, want 2", ok.Protocol)
	}
}

func TestAuthOKOmitsEmptyDisplayName(t *testing.T) {
	url, token := newNegotiationServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeAuthOKPayloadMap(t, raw)
	if _, ok := payload["display_name"]; ok {
		t.Fatalf("empty display_name must be omitted, payload has %s", payload["display_name"])
	}
}

func TestPairOKCarriesDisplayName(t *testing.T) {
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
		DisplayName:        "Studio Mac",
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + ts.URL[len("http"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

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
	if ok.DisplayName != "Studio Mac" {
		t.Fatalf("DisplayName=%q", ok.DisplayName)
	}
}

// Pure negotiation matrix (U2).
func TestNegotiateVersion(t *testing.T) {
	cases := []struct {
		offer []int
		want  int
	}{
		{nil, protocol.V1},
		{[]int{}, protocol.V1},
		{[]int{1}, protocol.V1},
		{[]int{2}, protocol.V2},
		{[]int{1, 2}, protocol.V2},
		{[]int{2, 1}, protocol.V2},
		{[]int{1, 2, 3}, protocol.V2},
		{[]int{3}, 0},
		{[]int{0, -1}, 0},
	}
	for _, tc := range cases {
		if got := protocol.NegotiateVersion(tc.offer); got != tc.want {
			t.Errorf("NegotiateVersion(%v) = %d, want %d", tc.offer, got, tc.want)
		}
	}
}
