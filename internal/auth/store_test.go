package auth_test

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/auth"
)

func TestStoreCreateValidateRevoke(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}

	dev, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || dev.ID == "" {
		t.Fatalf("empty token or id")
	}
	if got, err := store.Validate(token); err != nil || got.ID != dev.ID {
		t.Fatalf("validate: got=%+v err=%v", got, err)
	}
	if _, err := store.Validate("mcr_invalid"); err == nil {
		t.Fatal("expected invalid token error")
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	if list[0].Name != "phone" {
		t.Fatalf("name=%q", list[0].Name)
	}

	if _, err := store.Revoke(dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(token); err == nil {
		t.Fatal("expected invalid after revoke")
	}
}

func TestHashStable(t *testing.T) {
	a := auth.HashToken("mcr_abc")
	b := auth.HashToken("mcr_abc")
	if a != b || a == "" {
		t.Fatalf("hash mismatch %s %s", a, b)
	}
}

func TestStorePruneKeyless(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	// One keyless (legacy) device, one with an enrolled client key.
	if _, _, err := store.Create("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateWithClientKey("phone", "somefp"); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Prune(time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Name != "legacy" {
		t.Fatalf("expected only the keyless device pruned, got %v", removed)
	}
	left, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name != "phone" {
		t.Fatalf("keyed device must survive, got %v", left)
	}
}

func TestValidateDebouncesLastUsedWrites(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	afterCreate := store.SaveCount()
	for i := 0; i < 50; i++ {
		if _, err := store.Validate(token); err != nil {
			t.Fatal(err)
		}
	}
	// LastUsedAt updates must not rewrite the file on every Validate.
	if got := store.SaveCount() - afterCreate; got != 0 {
		t.Fatalf("Validate caused %d extra saves, want 0 (debounced)", got)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	if store.SaveCount()-afterCreate != 1 {
		t.Fatalf("Flush should write once, saves since create=%d", store.SaveCount()-afterCreate)
	}
}

func TestStorePruneStale(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create("used")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create("never-used"); err != nil {
		t.Fatal(err)
	}
	// Mark "used" as recently used.
	if _, err := store.Validate(token); err != nil {
		t.Fatal(err)
	}
	// Prune anything not used in the last hour: only "never-used" qualifies
	// (LastUsedAt nil), "used" was just validated.
	removed, err := store.Prune(time.Now().UTC().Add(-time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Name != "never-used" {
		t.Fatalf("expected only never-used pruned, got %v", removed)
	}
}

// Concurrent Creates from two Store handles on the same path must not lose
// devices (Phase 0.5 / 1.5 flock). Without path locking, last-writer-wins RMW
// drops some of the N+N records.
func TestStoreConcurrentCreateTwoHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	a, err := auth.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}

	const per = 40
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < per; i++ {
			if _, _, err := a.Create("a"); err != nil {
				t.Errorf("a create: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < per; i++ {
			if _, _, err := b.Create("b"); err != nil {
				t.Errorf("b create: %v", err)
				return
			}
		}
	}()
	wg.Wait()

	fresh, err := auth.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := fresh.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2*per {
		t.Fatalf("devices = %d, want %d (lost updates under concurrent RMW)", len(list), 2*per)
	}
}

// A revoke (or create) performed by a second Store instance on the same file —
// the CLI while the daemon runs — must be honored by the first instance, and
// the first instance's debounced flush must not resurrect revoked records.
func TestStoreHonorsExternalRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	daemon, err := auth.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	dev, token, err := daemon.Create("phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Validate(token); err != nil {
		t.Fatalf("pre-revoke validate: %v", err)
	}

	// CLI process: separate Store on the same file.
	cli, err := auth.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Revoke(dev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	keeper, keeperToken, err := cli.Create("kept")
	if err != nil {
		t.Fatal(err)
	}

	// The daemon's cached view must yield to the rewritten file.
	if _, err := daemon.Validate(token); err == nil {
		t.Fatal("revoked token still validates in the running store")
	}
	if _, err := daemon.Validate(keeperToken); err != nil {
		t.Fatalf("CLI-created token rejected: %v", err)
	}

	// A flush from the daemon must not clobber the CLI's writes.
	if err := daemon.Flush(); err != nil {
		t.Fatal(err)
	}
	fresh, err := auth.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := fresh.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != keeper.ID {
		t.Fatalf("post-flush devices = %+v, want only %s", devices, keeper.ID)
	}
}

// Device names arrive from clients (WS pairing) and the CLI; Create must
// sanitize them so terminal `pair list` output can't be corrupted by ANSI/
// control chars, a byte-truncation can't split a rune, and a UUID-shaped name
// can't collide with a device id in Revoke.
func TestCreateSanitizesDeviceName(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}

	// The ESC control byte (and NUL, newline) are stripped, defanging the ANSI
	// escape; the residual printable "[31m" is harmless without its ESC. Spaces
	// collapse. Stripping the printable residue too would mangle real names like
	// "device [work]", so only the control bytes are removed.
	dev, _, err := store.Create("  ev\x1b[31mil\x00  name\n  ")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Name != "ev[31mil name" {
		t.Fatalf("name=%q want %q", dev.Name, "ev[31mil name")
	}
	if strings.ContainsRune(dev.Name, '\x1b') || strings.ContainsRune(dev.Name, '\x00') {
		t.Fatalf("control bytes survived sanitization: %q", dev.Name)
	}

	// A fully-stripped name falls back to "device".
	blank, _, err := store.Create("\x00\x1b\n")
	if err != nil {
		t.Fatal(err)
	}
	if blank.Name != "device" {
		t.Fatalf("blank name=%q want device", blank.Name)
	}

	// A UUID-shaped name is deflected so it can never equal a device id.
	uuidish, _, err := store.Create("123e4567-e89b-12d3-a456-426614174000")
	if err != nil {
		t.Fatal(err)
	}
	if uuidish.Name == "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatal("UUID-shaped name must be deflected, not stored verbatim")
	}

	// Revoke by (sanitized) name still works.
	if _, err := store.Revoke(dev.Name); err != nil {
		t.Fatalf("revoke by name: %v", err)
	}
}
