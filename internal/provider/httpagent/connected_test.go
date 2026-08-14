package httpagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func configProvidersJSON(ids ...string) string {
	type row struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	var rows []row
	for _, id := range ids {
		rows = append(rows, row{ID: id, Key: "sk-DO-NOT-LEAK"})
	}
	b, _ := json.Marshal(map[string]any{"providers": rows, "default": map[string]string{}})
	return string(b)
}

func providerJSON(connected []string) string {
	b, _ := json.Marshal(map[string]any{
		"all":       []map[string]any{{"id": "hpc-ai", "key": "sk-DO-NOT-LEAK"}},
		"connected": connected,
		"default":   map[string]string{},
	})
	return string(b)
}

func spyAPI(t *testing.T, bodies map[string]string, hits *[]string, mu *sync.Mutex) API {
	t.Helper()
	return func(_ context.Context, _, path string, _, out any) error {
		if mu != nil {
			mu.Lock()
			*hits = append(*hits, path)
			mu.Unlock()
		} else {
			*hits = append(*hits, path)
		}
		body, ok := bodies[path]
		if !ok {
			return fmt.Errorf("unexpected fetch of %s", path)
		}
		return json.Unmarshal([]byte(body), out)
	}
}

func TestFetchConfigProviderIDsDropsKey(t *testing.T) {
	var hits []string
	ids, err := FetchConfigProviderIDs(context.Background(), spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON("togetherai"),
	}, &hits, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["togetherai"]; !ok {
		t.Fatalf("ids=%v, want togetherai", ids)
	}
	blob, _ := json.Marshal(ids)
	if strings.Contains(string(blob), "sk-") {
		t.Fatalf("connected ids carried a key: %s", blob)
	}
}

func TestVerifyHappyPathHitsConfigNotProvider(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var hits []string
	api := spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON("togetherai"),
	}, &hits, nil)
	if err := p.VerifyUpstreamConnected(context.Background(), api, "togetherai"); err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h == "/provider" {
			t.Fatalf("happy path fetched /provider: %v", hits)
		}
	}
}

func TestVerifyDisputeEscalatesOnce(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var hits []string
	api := spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON(),
		"/provider":         providerJSON(nil),
	}, &hits, nil)
	err := p.VerifyUpstreamConnected(context.Background(), api, "togetherai")
	if !errors.Is(err, provider.ErrCredentialNotAccepted) {
		t.Fatalf("err=%v, want ErrCredentialNotAccepted", err)
	}
	var providers, full int
	for _, h := range hits {
		switch h {
		case "/provider":
			full++
		case "/config/providers":
			providers++
		}
	}
	if full != 1 {
		t.Fatalf("GET /provider count=%d, want 1; hits=%v", full, hits)
	}
	if providers < 1 {
		t.Fatalf("GET /config/providers missing: %v", hits)
	}
}

func TestVerifyDisputeAcceptsIfProviderConnected(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var hits []string
	api := spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON(),
		"/provider":         providerJSON([]string{"togetherai"}),
	}, &hits, nil)
	if err := p.VerifyUpstreamConnected(context.Background(), api, "togetherai"); err != nil {
		t.Fatal(err)
	}
}

func TestConnectedCacheServesWithinTTL(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	p.Remember(map[string]struct{}{"togetherai": {}}, "config")
	snap := p.Snapshot()
	if !snap.Fresh {
		t.Fatal("just-remembered cache was not fresh")
	}
	if _, ok := snap.IDs["togetherai"]; !ok {
		t.Fatal("cached id missing")
	}
}

func TestNegativeCacheSuppressesReread(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var hits []string
	var mu sync.Mutex
	api := spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON(),
		"/provider":         providerJSON(nil),
	}, &hits, &mu)
	_ = p.VerifyUpstreamConnected(context.Background(), api, "togetherai")
	mu.Lock()
	first := len(hits)
	mu.Unlock()
	_ = p.VerifyUpstreamConnected(context.Background(), api, "togetherai")
	mu.Lock()
	defer mu.Unlock()
	var full int
	for _, h := range hits[first:] {
		if h == "/provider" {
			full++
		}
	}
	if full != 0 {
		t.Fatalf("negative cache still hit /provider; hits=%v", hits)
	}
}

func TestSingleFlightProviderFetch(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var started atomic.Int32
	gate := make(chan struct{})
	var hits atomic.Int32
	api := func(_ context.Context, _, path string, _, out any) error {
		if path == "/config/providers" {
			return json.Unmarshal([]byte(configProvidersJSON()), out)
		}
		if path != "/provider" {
			return fmt.Errorf("unexpected %s", path)
		}
		hits.Add(1)
		started.Add(1)
		<-gate
		return json.Unmarshal([]byte(providerJSON(nil)), out)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.VerifyUpstreamConnected(context.Background(), api, "togetherai")
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for started.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// Give the other goroutines a moment to pile up on the single-flight wait.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("GET /provider count=%d, want 1", n)
	}
}

func TestMutationRingWraps(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	for i := 0; i < 40; i++ {
		p.Note("set", fmt.Sprintf("v%d", i))
	}
	if n := p.MutationRingLen(); n != mutationRingCap {
		t.Fatalf("ring len=%d, want %d", n, mutationRingCap)
	}
	if p.Snapshot().Seq != 40 {
		t.Fatalf("seq=%d, want 40", p.Snapshot().Seq)
	}
}

func TestAfterCredentialWriteCompensates(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var hits []string
	api := spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON(),
		"/provider":         providerJSON(nil),
	}, &hits, nil)
	var cleared bool
	err := p.afterCredentialWrite(context.Background(), api, "togetherai", func(context.Context) error {
		cleared = true
		return nil
	})
	if !errors.Is(err, provider.ErrCredentialNotAccepted) {
		t.Fatalf("err=%v", err)
	}
	if !cleared {
		t.Fatal("compensating delete was not called")
	}
}

func TestMergeConnectedSnapshotAddsVerifiedID(t *testing.T) {
	st := provider.AuthState{
		Status: provider.AuthMissing,
		Upstreams: []provider.UpstreamAuth{
			{ID: "kilo", Status: provider.AuthConfigured},
			{ID: "azure", Status: provider.AuthMissing},
		},
	}
	got := mergeConnectedSnapshot(st, map[string]struct{}{"togetherai": {}, "azure": {}})
	if got.Status != provider.AuthConfigured {
		t.Fatalf("status=%q", got.Status)
	}
	var together, azure string
	for _, up := range got.Upstreams {
		switch up.ID {
		case "togetherai":
			together = up.Status
		case "azure":
			azure = up.Status
		}
	}
	if together != provider.AuthConfigured || azure != provider.AuthConfigured {
		t.Fatalf("together=%q azure=%q ups=%+v", together, azure, got.Upstreams)
	}
}

func TestVerifyDoesNotLogSecret(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	var hits []string
	api := spyAPI(t, map[string]string{
		"/config/providers": configProvidersJSON(),
		"/provider":         providerJSON(nil),
	}, &hits, nil)
	err := p.VerifyUpstreamConnected(context.Background(), api, "togetherai")
	if err == nil {
		t.Fatal("expected rejection")
	}
	if strings.Contains(err.Error(), "sk-") {
		t.Fatalf("error leaked a key: %v", err)
	}
}
