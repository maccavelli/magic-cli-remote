package auth

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestOpenStoreCannotTruncateAConcurrentWrite is MADR 0201 D25 (0073 §6.7).
// OpenStore stat'ed devices.json and created an empty one outside the
// cross-process lock, so a device a writer added in between, under that lock,
// was truncated away.
func TestOpenStoreCannotTruncateAConcurrentWrite(t *testing.T) {
	b, err := json.Marshal(fileData{Devices: []deviceRecord{{
		ID: "dev-1", Name: "phone", TokenHash: "h", CreatedAt: time.Now().UTC(),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for round := range 200 {
		path := filepath.Join(t.TempDir(), "devices.json")
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			// A writer in another process, as pair create is: under the
			// path lock, one atomic write.
			if err := withPathLock(path, func() error { return writeFileAtomic(path, b) }); err != nil {
				t.Errorf("round %d: writer: %v", round, err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if _, err := OpenStore(path); err != nil {
				t.Errorf("round %d: OpenStore: %v", round, err)
			}
		}()
		close(start)
		wg.Wait()

		s, err := OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		devs, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(devs) != 1 {
			t.Fatalf("round %d: %d devices after a write raced OpenStore; the device was truncated away", round, len(devs))
		}
	}
}
