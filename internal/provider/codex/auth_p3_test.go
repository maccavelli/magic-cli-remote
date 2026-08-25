package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestProxyPreInitializeAuthenticationFailure(t *testing.T) {
	const token = "native-codex-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err == nil {
			defer conn.CloseNow()
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err == nil {
		t.Fatal("unauthenticated WebSocket reached JSON-RPC initialization")
	}
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil || response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("authenticated boundary status = %v, err=%v", response, err)
	}
	conn.CloseNow()
}

func TestProxyCapabilityTokenFileAndVerifier(t *testing.T) {
	runtimeDir := t.TempDir()
	auth, err := createWebSocketAuth(runtimeDir, WSAuthCapabilityToken)
	if err != nil {
		t.Fatalf("create capability auth: %v", err)
	}
	defer auth.remove()
	info, err := os.Stat(auth.secretFile)
	if err != nil {
		t.Fatalf("stat secret file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %o, want 600", info.Mode().Perm())
	}
	if auth.bearer == "" || strings.Contains(auth.bearer, "\n") {
		t.Fatalf("invalid capability bearer %q", auth.bearer)
	}
	if got := auth.header.Get("Authorization"); got != "Bearer "+auth.bearer {
		t.Fatalf("authorization = %q", got)
	}
}

func TestProxySignedBearerLaunchMode(t *testing.T) {
	auth, err := createWebSocketAuth(t.TempDir(), WSAuthSignedBearer)
	if err != nil {
		t.Fatalf("create signed bearer auth: %v", err)
	}
	defer auth.remove()
	parts := strings.Split(auth.bearer, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT claims: %v", err)
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Audience string `json:"aud"`
		Expires  int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decode JWT claims JSON: %v", err)
	}
	if claims.Issuer != wsJWTIssuer || claims.Audience != wsJWTAudience || claims.Expires <= time.Now().Unix() {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestProxyHealthReadinessAndOriginRejection(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "origin forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/readyz" && r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitWebSocketHealth(ctx, server.URL, server.Client()); err != nil {
		t.Fatalf("health readiness: %v", err)
	}
	if probes.Load() != 2 {
		t.Fatalf("health probes = %d, want 2", probes.Load())
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("origin request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("origin status = %d, want 403", resp.StatusCode)
	}
}

func TestProxySecretPathStaysInsideRuntimeDirectory(t *testing.T) {
	runtimeDir := t.TempDir()
	auth, err := createWebSocketAuth(runtimeDir, WSAuthCapabilityToken)
	if err != nil {
		t.Fatalf("create auth: %v", err)
	}
	defer auth.remove()
	if !pathWithin(runtimeDir, filepath.Dir(auth.secretFile)) {
		t.Fatalf("secret escaped runtime directory: %s", auth.secretFile)
	}
}
