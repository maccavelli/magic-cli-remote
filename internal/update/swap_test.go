package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSwapAndRestart_NoService(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "new")
	dest := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(staged, []byte("newbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("oldbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := SwapAndRestart(staged, dest, SwapOpts{
		Product: "mcremote",
		Sleep:   func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "newbin" {
		t.Fatalf("dest = %q", b)
	}
}

func TestSwapAndRestart_PreservesUpState(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "new")
	dest := filepath.Join(dir, "mcremote")
	_ = os.WriteFile(staged, []byte("new"), 0o755)
	_ = os.WriteFile(dest, []byte("old"), 0o755)

	var stops, starts int
	active := true
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return active, nil },
		StopFn: func(string) error {
			stops++
			active = false
			return nil
		},
		StartFn: func(string) error {
			starts++
			active = true
			return nil
		},
	}
	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stops != 1 || starts != 1 {
		t.Fatalf("stops=%d starts=%d", stops, starts)
	}
	if !active {
		t.Fatal("should be active after swap")
	}
}

func TestSwapAndRestart_RollbackOnStartFailure(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "new")
	dest := filepath.Join(dir, "mcremote")
	_ = os.WriteFile(staged, []byte("new"), 0o755)
	_ = os.WriteFile(dest, []byte("old"), 0o755)

	active := true
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return active, nil },
		StopFn: func(string) error {
			active = false
			return nil
		},
		StartFn: func(string) error {
			return errors.New("start failed")
		},
	}
	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Sleep:          func(time.Duration) {},
	})
	if err == nil {
		t.Fatal("expected start failure")
	}
	// .prev restored to dest.
	b, _ := os.ReadFile(dest)
	if string(b) != "old" {
		t.Fatalf("dest after rollback = %q, want old", b)
	}
}

func TestSwapAndRestart_HealEnabledDown(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "new")
	dest := filepath.Join(dir, "mcremote")
	_ = os.WriteFile(staged, []byte("new"), 0o755)
	_ = os.WriteFile(dest, []byte("old"), 0o755)

	starts := 0
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return starts > 0, nil },
		StopFn:     func(string) error { return nil },
		StartFn: func(string) error {
			starts++
			return nil
		},
	}
	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		HealStart:      true, // enabled-but-stopped
		Service:        svc,
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("starts=%d, want heal start", starts)
	}
}
