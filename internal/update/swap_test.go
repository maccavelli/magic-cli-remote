package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// fakeRefresher records the definition reconciliation calls in the same ordered
// log as the service cycle, so the sequence itself can be asserted (MADR 0100).
type fakeRefresher struct {
	res       UnitRefresh
	err       error
	refreshed int
	restored  int
	log       *[]string
}

func (f *fakeRefresher) RefreshUnit(product, binary string) (UnitRefresh, error) {
	f.refreshed++
	*f.log = append(*f.log, "refresh")
	return f.res, f.err
}

func (f *fakeRefresher) RestoreUnit(product string, r UnitRefresh) error {
	f.restored++
	*f.log = append(*f.log, "restore:"+r.BackupPath)
	return nil
}

func stagedPair(t *testing.T) (staged, dest string) {
	t.Helper()
	dir := t.TempDir()
	staged = filepath.Join(dir, "new")
	dest = filepath.Join(dir, "mcremote")
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return staged, dest
}

// The definition must be reconciled after the swap (the new template only
// exists once the new binary is in place) and before the start (so the new
// definition is what starts).
func TestSwapRefreshesBeforeStart(t *testing.T) {
	staged, dest := stagedPair(t)
	var order []string
	active := true
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return active, nil },
		StopFn:     func(string) error { order = append(order, "stop"); active = false; return nil },
		StartFn:    func(string) error { order = append(order, "start"); active = true; return nil },
	}
	ref := &fakeRefresher{log: &order, res: UnitRefresh{Output: "service definition unchanged: /x"}}

	var logs []string
	if err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Refresher:      ref,
		Log:            func(s string) { logs = append(logs, s) },
		Sleep:          func(time.Duration) {},
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "stop,refresh,start" {
		t.Fatalf("order = %v, want stop,refresh,start", order)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "service definition unchanged") {
		t.Fatalf("refresh output was not surfaced: %v", logs)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Fatalf("dest = %q", b)
	}
}

// A refresh that fails must not fail the update: the binary swap is what
// `update` promises (MADR 0100 D3).
func TestSwapContinuesWhenRefreshFails(t *testing.T) {
	staged, dest := stagedPair(t)
	var order []string
	active := true
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return active, nil },
		StopFn:     func(string) error { active = false; return nil },
		StartFn:    func(string) error { order = append(order, "start"); active = true; return nil },
	}
	ref := &fakeRefresher{log: &order, err: errors.New("child exited 1")}

	var logs []string
	if err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Refresher:      ref,
		Log:            func(s string) { logs = append(logs, s) },
		Sleep:          func(time.Duration) {},
	}); err != nil {
		t.Fatalf("refresh failure must not fail the update: %v", err)
	}
	if ref.restored != 0 {
		t.Fatal("nothing was written, so nothing should be restored")
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "refresh failed") || !strings.Contains(joined, "continuing") {
		t.Fatalf("failure was not reported: %v", logs)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Fatalf("dest = %q, want the swap to have happened anyway", b)
	}
}

// A failed start rolls back the definition as well as the binary, so the host
// is left exactly as it was found.
func TestSwapRestoresUnitOnStartFailure(t *testing.T) {
	staged, dest := stagedPair(t)
	var order []string
	active := true
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return active, nil },
		StopFn:     func(string) error { active = false; return nil },
		StartFn:    func(string) error { return errors.New("start failed") },
	}
	ref := &fakeRefresher{
		log: &order,
		res: UnitRefresh{Changed: true, Path: "/u/mcremote.service", BackupPath: "/u/mcremote.service.prev"},
	}

	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Refresher:      ref,
		Sleep:          func(time.Duration) {},
	})
	if err == nil {
		t.Fatal("expected the start failure to surface")
	}
	if ref.restored != 1 {
		t.Fatalf("RestoreUnit called %d times, want 1", ref.restored)
	}
	if b, _ := os.ReadFile(dest); string(b) != "old" {
		t.Fatalf("dest = %q, want the previous binary back", b)
	}
}

// An unchanged definition must not be restored on failure: there is no backup,
// and renaming a non-existent .prev would only produce noise.
func TestSwapDoesNotRestoreUnchangedUnit(t *testing.T) {
	staged, dest := stagedPair(t)
	var order []string
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return true, nil },
		StopFn:     func(string) error { return nil },
		StartFn:    func(string) error { return errors.New("start failed") },
	}
	ref := &fakeRefresher{log: &order, res: UnitRefresh{Changed: false}}

	if err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Refresher:      ref,
		Sleep:          func(time.Duration) {},
	}); err == nil {
		t.Fatal("expected the start failure to surface")
	}
	if ref.restored != 0 {
		t.Fatalf("RestoreUnit called %d times, want 0", ref.restored)
	}
}

// A nil Refresher is the pre-0100 path and must behave exactly as before.
func TestSwapSkipsRefreshWhenNil(t *testing.T) {
	staged, dest := stagedPair(t)
	active := true
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return active, nil },
		StopFn:     func(string) error { active = false; return nil },
		StartFn:    func(string) error { active = true; return nil },
	}
	if err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Sleep:          func(time.Duration) {},
	}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Fatalf("dest = %q", b)
	}
}

// TestSwapFailsOnUndeletablePrev pins MADR 0116 F9: a stale .prev that cannot
// be removed must abort the swap BEFORE the service is stopped, rather than
// being discarded and resurfacing as a half-done rename.
//
// On Windows the real trigger is a .prev still open by another process. Here
// the same branch is driven by making .prev a non-empty directory, which
// os.Remove refuses on every platform — the point is that the error is
// returned rather than swallowed.
func TestSwapFailsOnUndeletablePrev(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "new")
	dest := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(staged, []byte("newbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("oldbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := dest + ".prev"
	if err := os.Mkdir(prev, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prev, "occupied"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stops int
	svc := FuncService{
		IsActiveFn: func(string) (bool, error) { return true, nil },
		StopFn:     func(string) error { stops++; return nil },
		StartFn:    func(string) error { return nil },
	}
	err := SwapAndRestart(staged, dest, SwapOpts{
		Product:        "mcremote",
		RestartService: true,
		Service:        svc,
		Sleep:          func(time.Duration) {},
	})
	if err == nil {
		t.Fatal("swap succeeded despite an undeletable .prev")
	}
	if !strings.Contains(err.Error(), "stale backup") {
		t.Errorf("err = %v, want it to name the stale backup", err)
	}
	// The failure must be free of side effects: nothing stopped, nothing moved.
	if stops != 0 {
		t.Errorf("service was stopped %d times before the guard fired", stops)
	}
	if b, _ := os.ReadFile(dest); string(b) != "oldbin" {
		t.Errorf("dest = %q, want the original binary untouched", b)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Errorf("staged binary was consumed: %v", err)
	}
}
