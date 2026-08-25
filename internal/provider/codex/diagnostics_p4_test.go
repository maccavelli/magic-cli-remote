package codex

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiagnosticProjectionRedactsAndReducesUnknownChecks(t *testing.T) {
	raw, err := os.ReadFile("testdata/0.149.1/doctor-sanitized.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ProjectDoctorReport(raw)
	if err != nil {
		t.Fatal(err)
	}
	b := string(got.JSON())
	for _, forbidden := range []string{"sk-sentinel-secret", "/home/sentinel", "https://sentinel.invalid", "must be discarded", "curl "} {
		if strings.Contains(b, forbidden) {
			t.Errorf("projection leaked %q: %s", forbidden, b)
		}
	}
	if got.SchemaVersion != 1 || got.OverallStatus != "fail" || got.CodexVersion != "0.149.1" {
		t.Fatalf("header = %+v", got)
	}
	if len(got.Checks) != 2 || got.Checks[1].ID != "future.check" || len(got.Checks[1].Details) != 0 {
		t.Fatalf("checks = %+v", got.Checks)
	}
}

func TestDiagnosticRejectsSchemaMismatchKeyMismatchAndBounds(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"schemaVersion":2,"checks":{}}`),
		[]byte(`{"schemaVersion":1,"checks":{"a":{"id":"b","category":"x","status":"pass","summary":"x","details":{},"durationMs":1}}}`),
		append([]byte(`{"schemaVersion":1,"checks":{},"padding":"`), append(make([]byte, MaxDoctorOutputBytes), []byte(`"}`)...)...),
	} {
		if _, err := ProjectDoctorReport(raw); err == nil {
			t.Fatalf("accepted invalid report (%d bytes)", len(raw))
		}
	}
}

func TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight(t *testing.T) {
	raw, _ := os.ReadFile("testdata/0.149.1/doctor-sanitized.json")
	p := New(Config{Bin: "codex"})
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	p.doctorRun = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		if bin != "codex" || len(args) != 2 || args[0] != "doctor" || args[1] != "--json" {
			t.Fatalf("argv = %q %q", bin, args)
		}
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return raw, errors.New("exit status 1") // overall fail is still a valid report.
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := p.RunDoctor(context.Background()); errs <- err }()
	}
	<-entered
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("RunDoctor: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("doctor invocations = %d, want one", calls.Load())
	}

	p.doctorRun = func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := p.RunDoctor(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
}
