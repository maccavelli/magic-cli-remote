package ws_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// 0068 P6 — a genuine-capacity refusal (every slot authenticated) tells the
// client when a slot could plausibly free instead of leaving it to guess.
func TestCapacityRefusalCarriesRetryAfter(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := ws.New(ws.Options{
		Store:              store,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
		MaxClients:         2,
		ReadDeadline:       60 * time.Second,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	url := "ws" + ts.URL[len("http"):] + "/v1/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Fill every slot with a DISTINCT authenticated device — same-device
	// dials would be absorbed by replacement (P2), not refused.
	for i := 0; i < 2; i++ {
		_, token, err := store.Create("dev" + strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		conn := authConn(ctx, t, url, token, strconv.Itoa(i))
		defer conn.Close(websocket.StatusNormalClosure, "")
	}

	over, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("over-capacity dial should upgrade then be closed: %v", err)
	}
	defer over.Close(websocket.StatusNormalClosure, "")
	_, _, err = over.Read(ctx)
	if err == nil {
		t.Fatal("over-capacity connection survived")
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) {
		t.Fatalf("want a close error, got %v", err)
	}
	if ce.Code != websocket.StatusTryAgainLater {
		t.Fatalf("close code = %d, want %d", ce.Code, websocket.StatusTryAgainLater)
	}
	m := regexp.MustCompile(`retry_after_ms=(\d+)`).FindStringSubmatch(ce.Reason)
	if m == nil {
		t.Fatalf("close reason %q missing retry_after_ms", ce.Reason)
	}
	msec, _ := strconv.Atoi(m[1])
	// Sane: at least the 5s floor, at most the full read deadline — with
	// two just-active clients the horizon estimate is ≈ the deadline.
	if msec < 5000 || msec > 120_000 {
		t.Fatalf("retry_after_ms = %d, want within [5000, 120000]", msec)
	}
}
