package picker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

func oneOption(id string) picker.Catalog {
	return picker.SingleCatalog(picker.SourceLive, []picker.Option{{ID: id}}, id, false)
}

// TestCacheServesWithinTTL is the reason the cache exists: opening the model
// picker twice used to mean two multi-MB engine fetches.
func TestCacheServesWithinTTL(t *testing.T) {
	c := picker.NewCache[string](time.Minute)
	var calls atomic.Int32
	fetch := func(context.Context) (picker.Catalog, error) {
		calls.Add(1)
		return oneOption("a"), nil
	}
	for i := 0; i < 3; i++ {
		got, err := c.Get(context.Background(), "k", fetch)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Options) != 1 || got.Options[0].ID != "a" {
			t.Fatalf("call %d: %+v", i, got)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}
}

// TestCacheExpires proves the TTL is real, not decorative.
func TestCacheExpires(t *testing.T) {
	c := picker.NewCache[string](time.Millisecond)
	var calls atomic.Int32
	fetch := func(context.Context) (picker.Catalog, error) {
		calls.Add(1)
		return oneOption("a"), nil
	}
	if _, err := c.Get(context.Background(), "k", fetch); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := c.Get(context.Background(), "k", fetch); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}

// TestCacheInvalidateOnEngineRestart: a catalog from a dead engine names models
// the new process may refuse, so a generation bump must drop it.
func TestCacheInvalidateOnEngineRestart(t *testing.T) {
	c := picker.NewCache[string](time.Minute)
	var calls atomic.Int32
	fetch := func(context.Context) (picker.Catalog, error) {
		calls.Add(1)
		return oneOption("a"), nil
	}
	if _, err := c.Get(context.Background(), "k", fetch); err != nil {
		t.Fatal(err)
	}
	c.Invalidate()
	if _, err := c.Get(context.Background(), "k", fetch); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}

// TestCacheSingleFlight: concurrent picker opens must not multiply the fetch.
func TestCacheSingleFlight(t *testing.T) {
	c := picker.NewCache[string](time.Minute)
	var calls atomic.Int32
	release := make(chan struct{})
	fetch := func(context.Context) (picker.Catalog, error) {
		calls.Add(1)
		<-release
		return oneOption("a"), nil
	}
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Get(context.Background(), "k", fetch)
		}(i)
	}
	// Give the goroutines time to pile up on the in-flight call.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("waiter %d: %v", i, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}
}

// TestCacheDoesNotCacheErrors: a transient engine hiccup must not blank the
// picker for the whole TTL, and every waiter must see the same failure rather
// than each retrying against an engine that just said no.
func TestCacheDoesNotCacheErrors(t *testing.T) {
	c := picker.NewCache[string](time.Minute)
	boom := errors.New("engine down")
	var calls atomic.Int32
	fetch := func(context.Context) (picker.Catalog, error) {
		if calls.Add(1) == 1 {
			return picker.Catalog{}, boom
		}
		return oneOption("a"), nil
	}
	if _, err := c.Get(context.Background(), "k", fetch); !errors.Is(err, boom) {
		t.Fatalf("first call err = %v, want %v", err, boom)
	}
	got, err := c.Get(context.Background(), "k", fetch)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(got.Options) != 1 {
		t.Fatalf("second call did not refetch: %+v", got)
	}
}

// TestNilCacheStillFetches keeps a provider constructed without a cache working.
func TestNilCacheStillFetches(t *testing.T) {
	var c *picker.Cache[string]
	got, err := c.Get(context.Background(), "k", func(context.Context) (picker.Catalog, error) {
		return oneOption("a"), nil
	})
	if err != nil || len(got.Options) != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
	c.Invalidate() // must not panic
}
