package ws_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/certs"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// genClientCert mints a self-signed P-256 client certificate the way the phone
// does, and returns it alongside the SPKI fingerprint the daemon will record.
func genClientCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "mcremote-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, certs.SPKIFingerprint(leaf)
}

// newKeyServer builds a server backed by fresh stores, with client-key
// enforcement toggled by requireClientKey.
func newKeyServer(t *testing.T, requireClientKey bool) (*ws.Server, *auth.Store, *auth.PairCodeStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	codes, err := auth.OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
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
		RequireClientKey:   requireClientKey,
		Version:            "test",
	})
	return srv, store, codes
}

// startTLS serves srv over TLS with RequestClientCert, matching the listener.
func startTLS(t *testing.T, srv *ws.Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts
}

// dialWSS opens a wss connection, presenting cert as the client certificate
// when non-nil (nil means "present no client certificate").
func dialWSS(t *testing.T, ctx context.Context, ts *httptest.Server, cert *tls.Certificate) *websocket.Conn {
	t.Helper()
	tlsCfg := ts.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if cert != nil {
		tlsCfg.Certificates = []tls.Certificate{*cert}
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	wsURL := "wss" + ts.URL[len("https"):] + "/v1/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// TestWSClientKeyEnrolAndAuth covers the enforced path end to end: a device
// enrols its key at pair.claim, then authenticates with the matching cert,
// is rejected client_key_mismatch with a different cert, and client_key_required
// with none.
func TestWSClientKeyEnrolAndAuth(t *testing.T) {
	srv, store, codes := newKeyServer(t, true)
	ts := startTLS(t, srv)

	code, err := codes.Create("phone", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cert, wantFP := genClientCert(t)

	// pair.claim over the connection presenting the client key.
	conn := dialWSS(t, ctx, ts, &cert)
	claim, _ := protocol.NewEnvelope(protocol.TypePairClaim, "1", protocol.PairClaimPayload{Code: code.Display})
	writeEnv(t, ctx, conn, claim)
	got := readEnv(t, ctx, conn)
	if got.Type != protocol.TypePairOK {
		t.Fatalf("want pair_ok got %s payload=%s", got.Type, string(got.Payload))
	}
	var ok protocol.PairOKPayload
	if err := json.Unmarshal(got.Payload, &ok); err != nil {
		t.Fatal(err)
	}

	// The key was recorded on the new device record.
	devices, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ClientKeyFP != wantFP {
		t.Fatalf("device key not enrolled: %+v (want fp %s)", devices, wantFP)
	}

	authWith := func(t *testing.T, cert *tls.Certificate) protocol.Envelope {
		conn := dialWSS(t, ctx, ts, cert)
		env, _ := protocol.NewEnvelope(protocol.TypeAuth, "a", protocol.AuthPayload{Token: ok.Token})
		writeEnv(t, ctx, conn, env)
		return readEnv(t, ctx, conn)
	}

	t.Run("matching key authenticates", func(t *testing.T) {
		got := authWith(t, &cert)
		if got.Type != protocol.TypeAuthOK {
			t.Fatalf("want auth_ok got %s payload=%s", got.Type, string(got.Payload))
		}
	})

	t.Run("different key is client_key_mismatch", func(t *testing.T) {
		other, _ := genClientCert(t)
		got := authWith(t, &other)
		assertAuthErrorCode(t, got, "client_key_mismatch")
	})

	t.Run("no key is client_key_required", func(t *testing.T) {
		got := authWith(t, nil)
		assertAuthErrorCode(t, got, "client_key_required")
	})
}

// TestWSClientKeyEnforcementOff confirms a keyless device authenticates by
// token alone while enforcement is off.
func TestWSClientKeyEnforcementOff(t *testing.T) {
	srv, store, _ := newKeyServer(t, false)
	ts := startTLS(t, srv)

	// A legacy device with no recorded client key.
	_, token, err := store.Create("legacy")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Dial presenting no client certificate at all.
	conn := dialWSS(t, ctx, ts, nil)
	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(t, ctx, conn, env)
	got := readEnv(t, ctx, conn)
	if got.Type != protocol.TypeAuthOK {
		t.Fatalf("want auth_ok got %s payload=%s", got.Type, string(got.Payload))
	}
}

func assertAuthErrorCode(t *testing.T, env protocol.Envelope, wantCode string) {
	t.Helper()
	if env.Type != protocol.TypeAuthError {
		t.Fatalf("want auth_error got %s payload=%s", env.Type, string(env.Payload))
	}
	var p protocol.ErrorPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != wantCode {
		t.Fatalf("want code %q got %q", wantCode, p.Code)
	}
}

// TestHelloRequiresClientKey covers the recon-endpoint gap: with client-key
// enforcement on, a valid token alone must not pass /v1/hello — the presented
// client cert must also match the enrolled key. Otherwise a stolen token would
// still leak the Headscale control URL.
func TestHelloRequiresClientKey(t *testing.T) {
	srv, store, _ := newKeyServer(t, true)
	ts := startTLS(t, srv)

	cert, fp := genClientCert(t)
	_, token, err := store.CreateWithClientKey("phone", fp)
	if err != nil {
		t.Fatal(err)
	}

	hello := func(t *testing.T, present *tls.Certificate) int {
		tlsCfg := ts.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		if present != nil {
			tlsCfg.Certificates = []tls.Certificate{*present}
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/hello", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}

	t.Run("token without client cert is rejected", func(t *testing.T) {
		if code := hello(t, nil); code != http.StatusUnauthorized {
			t.Fatalf("want 401 with no client cert, got %d", code)
		}
	})
	t.Run("token with wrong client cert is rejected", func(t *testing.T) {
		other, _ := genClientCert(t)
		if code := hello(t, &other); code != http.StatusUnauthorized {
			t.Fatalf("want 401 with wrong client cert, got %d", code)
		}
	})
	t.Run("token with the enrolled client cert is accepted", func(t *testing.T) {
		if code := hello(t, &cert); code != http.StatusOK {
			t.Fatalf("want 200 with enrolled client cert, got %d", code)
		}
	})
}
