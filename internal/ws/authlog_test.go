package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
)

// authLogRecords returns the JSON log records whose msg is the rejection line.
func authLogRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if rec["msg"] == "authentication rejected" {
			out = append(out, rec)
		}
	}
	return out
}

func newAuthLogServer(t *testing.T) (*Server, *bytes.Buffer, *auth.Store) {
	t.Helper()
	store, err := auth.OpenStore(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	s := New(Options{
		Store:              store,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
		Log:                slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	return s, buf, store
}

// TestEveryAuthRejectionIsLoggedWithoutTheToken is MADR 0201 D1 (0073 F3(b)).
// A phone that cannot log in must leave a trace on the host: every rejection,
// on the WebSocket and on GET /v1/hello, is one warn line with its code and
// the remote address — and never the token.
func TestEveryAuthRejectionIsLoggedWithoutTheToken(t *testing.T) {
	s, buf, _ := newAuthLogServer(t)
	const secret = "mcr_this-token-must-never-be-logged"

	cases := []struct {
		err  error
		code string
		addr string
	}{
		{auth.ErrInvalidToken, "invalid_token", "192.0.2.1:5001"},
		{errClientKeyRequired, "client_key_required", "192.0.2.2:5001"},
		{&deviceAuthError{deviceID: "dev-9", err: errClientKeyMismatch}, "client_key_mismatch", "192.0.2.3:5001"},
		{errAlreadyAuthed, "already_authed", "192.0.2.4:5001"},
	}
	for _, tc := range cases {
		c := newAuthedTestClient("")
		c.remoteAddr = tc.addr
		if err := s.writeAuthError(context.Background(), c, "1", tc.err); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	req.RemoteAddr = "192.0.2.5:5001"
	req.Header.Set("Authorization", "Bearer "+secret)
	s.handleHello(httptest.NewRecorder(), req)

	recs := authLogRecords(t, buf)
	if len(recs) != len(cases)+1 {
		t.Fatalf("got %d rejection records, want %d:\n%s", len(recs), len(cases)+1, buf.String())
	}
	for i, tc := range cases {
		if recs[i]["level"] != "WARN" || recs[i]["code"] != tc.code || recs[i]["remote_addr"] != strings.Split(tc.addr, ":")[0] {
			t.Fatalf("record %d = %v; want WARN, code %s, remote_addr %s", i, recs[i], tc.code, tc.addr)
		}
	}
	if recs[2]["device_id"] != "dev-9" {
		t.Fatalf("a client-key mismatch should name its device: %v", recs[2])
	}
	if recs[4]["code"] != "invalid_token" || recs[4]["remote_addr"] != "192.0.2.5" {
		t.Fatalf("hello record = %v; want invalid_token from 192.0.2.5", recs[4])
	}
	if strings.Contains(buf.String(), secret) || strings.Contains(buf.String(), "mcr_") {
		t.Fatalf("a token reached the log:\n%s", buf.String())
	}
}

// TestAuthRejectionsFromOneAddressAreThrottled: a scanner must not flood the
// log. One code from one address logs once a minute; the next line carries
// how many it stood for.
func TestAuthRejectionsFromOneAddressAreThrottled(t *testing.T) {
	s, buf, _ := newAuthLogServer(t)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	c := newAuthedTestClient("")
	c.remoteAddr = "198.51.100.7:4000"
	// Room for every auth_error frame: a full queue drops the connection,
	// and this test client has none.
	c.out = make(chan []byte, 64)

	for range 20 {
		_ = s.writeAuthError(context.Background(), c, "1", auth.ErrInvalidToken)
	}
	if n := len(authLogRecords(t, buf)); n != 1 {
		t.Fatalf("20 rejections in one minute logged %d lines, want 1", n)
	}
	now = now.Add(61 * time.Second)
	_ = s.writeAuthError(context.Background(), c, "1", auth.ErrInvalidToken)
	recs := authLogRecords(t, buf)
	if len(recs) != 2 {
		t.Fatalf("after the window: %d lines, want 2", len(recs))
	}
	if got, _ := recs[1]["suppressed"].(float64); got != 19 {
		t.Fatalf("second line suppressed = %v, want 19: %v", recs[1]["suppressed"], recs[1])
	}
}
