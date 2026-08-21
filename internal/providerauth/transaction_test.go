package providerauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAdapter is a provider stand-in whose credential is a JSON object with a
// "mode" and a monotonic "seq". Nothing here mirrors a real provider's format;
// the coordinator must work from adapter metadata alone.
type fakeAdapter struct {
	id        string
	live      string
	lock      string
	maxBytes  int64
	failValid error
	failProbe error
	probes    int
}

func (f *fakeAdapter) ProviderID() string              { return f.id }
func (f *fakeAdapter) LivePath() (string, error)       { return f.live, nil }
func (f *fakeAdapter) NativeLockPath() (string, error) { return f.lock, nil }
func (f *fakeAdapter) CandidateName() string           { return "auth.json" }
func (f *fakeAdapter) MaxCandidateBytes() int64 {
	if f.maxBytes == 0 {
		return 64 << 10
	}
	return f.maxBytes
}
func (f *fakeAdapter) PendingEnv(home string) []string { return []string{"FAKE_HOME=" + home} }

func (f *fakeAdapter) Validate(_ context.Context, data []byte) (CredentialMeta, error) {
	if f.failValid != nil {
		return CredentialMeta{}, f.failValid
	}
	var v struct {
		Mode string `json:"mode"`
		Seq  int    `json:"seq"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return CredentialMeta{}, err
	}
	if v.Mode == "" {
		return CredentialMeta{}, errors.New("no mode")
	}
	return CredentialMeta{Mode: v.Mode, Sequence: int64(v.Seq), Revocable: v.Mode == "chatgpt"}, nil
}

func (f *fakeAdapter) Probe(_ context.Context, _ string) error {
	f.probes++
	return f.failProbe
}

func newFixture(t *testing.T) (*Coordinator, *fakeAdapter, string) {
	t.Helper()
	dataDir := t.TempDir()
	liveDir := t.TempDir()
	ad := &fakeAdapter{
		id:   "fake",
		live: filepath.Join(liveDir, "auth.json"),
		lock: filepath.Join(liveDir, "auth.json.lock"),
	}
	c, err := NewCoordinator(dataDir, ad, CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return c, ad, dataDir
}

func writeLive(t *testing.T, ad *fakeAdapter, mode string, seq int) []byte {
	t.Helper()
	b := []byte(`{"mode":"` + mode + `","seq":` + itoa(seq) + `}`)
	if err := os.WriteFile(ad.live, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// stage runs a full begin/stage/validate cycle, writing the candidate into the
// transaction's pending home the way a provider child would.
func stage(t *testing.T, c *Coordinator, ad *fakeAdapter, mode string, seq int) *Txn {
	t.Helper()
	ctx := context.Background()
	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	cand := []byte(`{"mode":"` + mode + `","seq":` + itoa(seq) + `}`)
	if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), cand, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.StageCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	return txn
}

// TestPendingHomeStartsEmpty is the F14 containment gate: the isolated home the
// provider child runs in must contain no credential material, because Codex
// revokes any token it finds there before deleting it (MADR 0074 D22).
func TestPendingHomeStartsEmpty(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(txn.Home())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("pending home has %d entries, want 0: seeding it would let the child revoke the live grant", len(entries))
	}
	fi, err := os.Stat(txn.Home())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("pending home mode = %04o, want 0700", fi.Mode().Perm())
	}
}

// TestSeedCreatesCurrent proves first-use seeding validates LIVE and durably
// creates CURRENT before any managed mutation (D23/D24).
func TestSeedCreatesCurrent(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	want := writeLive(t, ad, "chatgpt", 1)
	if err := c.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}

	m := readManifest(t, dataDir, ad.id)
	if m.State != StateIdle {
		t.Fatalf("state = %s, want idle", m.State)
	}
	cur := m.byLabel(LabelCurrent)
	if cur == nil {
		t.Fatal("no CURRENT generation")
	}
	if cur.Fingerprint != FingerprintOf(want) {
		t.Errorf("CURRENT fingerprint mismatch")
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "provider-auth", ad.id, "generations", cur.ID+".auth"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("generation payload mismatch")
	}
	fi, err := os.Stat(filepath.Join(dataDir, "provider-auth", ad.id, "generations", cur.ID+".auth"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("generation mode = %04o, want 0600", fi.Mode().Perm())
	}
}

// TestSeedColdHostCreatesNothing proves an unauthenticated host gets no
// artificial backup (D24).
func TestSeedColdHostCreatesNothing(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	if err := c.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dataDir, ad.id)
	if len(m.Generations) != 0 {
		t.Fatalf("generations = %d, want 0 on a cold host", len(m.Generations))
	}
	if m.State != StateIdle {
		t.Fatalf("state = %s, want idle", m.State)
	}
}

// TestCommitRetentionAndRotation proves the CURRENT/PREVIOUS chain keeps
// exactly two committed generations however many times it rotates (D23).
func TestCommitRetentionAndRotation(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	for seq := 2; seq <= 5; seq++ {
		txn := stage(t, c, ad, "chatgpt", seq)
		if err := c.Commit(ctx, txn); err != nil {
			t.Fatalf("commit seq %d: %v", seq, err)
		}

		live, err := os.ReadFile(ad.live)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(live), `"seq":`+itoa(seq)) {
			t.Fatalf("live = %s, want seq %d", live, seq)
		}

		m := readManifest(t, dataDir, ad.id)
		if m.State != StateIdle {
			t.Fatalf("state = %s, want idle after commit", m.State)
		}
		if got := len(m.Generations); got != 2 {
			t.Fatalf("generations = %d, want exactly CURRENT+PREVIOUS", got)
		}
		if m.byLabel(LabelCurrent).Fingerprint != FingerprintOf(live) {
			t.Fatal("CURRENT does not match LIVE after commit")
		}
		if m.byLabel(LabelPrevious) == nil {
			t.Fatal("no PREVIOUS after a rotation")
		}
		gens, err := os.ReadDir(filepath.Join(dataDir, "provider-auth", ad.id, "generations"))
		if err != nil {
			t.Fatal(err)
		}
		if len(gens) != 2 {
			t.Fatalf("generation payloads = %d, want 2", len(gens))
		}
	}
}

// TestCommitConflictOnStaleLive proves a live file changed by another writer
// since Begin is a typed conflict, not a blind overwrite (D25).
func TestCommitConflictOnStaleLive(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	txn := stage(t, c, ad, "chatgpt", 2)
	other := writeLive(t, ad, "chatgpt", 99) // a concurrent refresh wins the race

	err := c.Commit(ctx, txn)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("commit err = %v, want ErrConflict", err)
	}
	got, readErr := os.ReadFile(ad.live)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(other) {
		t.Fatal("conflict must leave the other writer's LIVE untouched")
	}
}

// TestValidateRejectsBadCandidates covers the D25 candidate gate: only a
// bounded, regular, owner-only, provider-valid file may be published.
func TestValidateRejectsBadCandidates(t *testing.T) {
	ctx := context.Background()

	t.Run("symlink", func(t *testing.T) {
		c, ad, _ := newFixture(t)
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		txn, err := c.Begin(ctx, SourceDeviceAuth)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		if err := os.WriteFile(target, []byte(`{"mode":"chatgpt","seq":9}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(txn.Home(), ad.CandidateName())); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		if err := c.StageCandidate(ctx, txn); !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("err = %v, want ErrInvalidCandidate", err)
		}
	})

	t.Run("group readable", func(t *testing.T) {
		c, ad, _ := newFixture(t)
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		txn, err := c.Begin(ctx, SourceDeviceAuth)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), []byte(`{"mode":"chatgpt","seq":9}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := c.StageCandidate(ctx, txn); !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("err = %v, want ErrInvalidCandidate", err)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		c, ad, _ := newFixture(t)
		ad.maxBytes = 32
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		txn, err := c.Begin(ctx, SourceDeviceAuth)
		if err != nil {
			t.Fatal(err)
		}
		big := `{"mode":"chatgpt","pad":"` + strings.Repeat("x", 200) + `"}`
		if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), []byte(big), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := c.StageCandidate(ctx, txn); !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("err = %v, want ErrInvalidCandidate", err)
		}
	})

	t.Run("absent", func(t *testing.T) {
		c, ad, _ := newFixture(t)
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		txn, err := c.Begin(ctx, SourceDeviceAuth)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.StageCandidate(ctx, txn); !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("err = %v, want ErrInvalidCandidate", err)
		}
	})

	t.Run("provider rejects", func(t *testing.T) {
		c, ad, _ := newFixture(t)
		writeLive(t, ad, "chatgpt", 1)
		if err := c.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		txn, err := c.Begin(ctx, SourceDeviceAuth)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := c.StageCandidate(ctx, txn); !errors.Is(err, ErrInvalidCandidate) {
			t.Fatalf("err = %v, want ErrInvalidCandidate", err)
		}
	})
}

// TestValidateProbeFailureAbortsWithoutTouchingLive proves the post-write probe
// is a precondition for publication, not a formality (D25).
func TestValidateProbeFailureAbortsWithoutTouchingLive(t *testing.T) {
	c, ad, _ := newFixture(t)
	want := writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	txn, err := c.Begin(ctx, SourceDeviceAuth)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txn.Home(), ad.CandidateName()), []byte(`{"mode":"chatgpt","seq":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.StageCandidate(ctx, txn); err != nil {
		t.Fatal(err)
	}
	ad.failProbe = errors.New("probe refused")
	if err := c.ValidateCandidate(ctx, txn); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("err = %v, want ErrInvalidCandidate", err)
	}
	got, readErr := os.ReadFile(ad.live)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(want) {
		t.Fatal("a failed probe must leave LIVE byte-identical")
	}
}

// TestAbortLeavesLiveAndCurrent proves an aborted transaction removes only its
// own pending data (D26).
func TestAbortLeavesLiveAndCurrent(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	want := writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	txn := stage(t, c, ad, "chatgpt", 2)
	home := txn.Home()
	if err := c.Abort(ctx, txn); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(ad.live)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("abort changed LIVE")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("abort left the pending home behind")
	}
	m := readManifest(t, dataDir, ad.id)
	if m.State != StateIdle {
		t.Fatalf("state = %s, want idle", m.State)
	}
	if m.byLabel(LabelPending) != nil {
		t.Fatal("abort left a PENDING label")
	}
	if m.byLabel(LabelCurrent) == nil {
		t.Fatal("abort destroyed CURRENT")
	}
}

// TestSingleTransactionAtATime proves at most one PENDING transaction (D23).
func TestSingleTransactionAtATime(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Begin(ctx, SourceDeviceAuth); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Begin(ctx, SourceDeviceAuth); !errors.Is(err, ErrTransactionBusy) {
		t.Fatalf("second Begin err = %v, want ErrTransactionBusy", err)
	}
}

// TestRecordLogoutTombstoneAndPurge proves logout is durable before removal and
// purges retained payloads, because revoked tokens are not known-good (D24).
func TestRecordLogoutTombstoneAndPurge(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	txn := stage(t, c, ad, "chatgpt", 2)
	if err := c.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}

	if err := c.RecordLogout(ctx); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dataDir, ad.id)
	if m.State != StateLoggedOut {
		t.Fatalf("state = %s, want logged_out", m.State)
	}
	if len(m.Generations) != 0 {
		t.Fatalf("generations = %d, want 0 after logout", len(m.Generations))
	}
	gens, err := os.ReadDir(filepath.Join(dataDir, "provider-auth", ad.id, "generations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 0 {
		t.Fatalf("generation payloads = %d, want 0 after logout", len(gens))
	}
	if _, err := os.Stat(ad.live); !os.IsNotExist(err) {
		t.Fatal("logout left LIVE in place")
	}
}

// TestManifestRejectsUnknownFields proves an older binary cannot mutate a
// partially understood future manifest (P17 step 4).
func TestManifestRejectsUnknownFields(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dataDir, "provider-auth", ad.id, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"provider"`, `"future_field":true,"provider"`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Begin(ctx, SourceDeviceAuth); err == nil {
		t.Fatal("Begin accepted a manifest with unknown fields")
	}
}

// TestManifestRejectsUnknownVersion proves a newer schema is refused rather
// than reinterpreted (P17 step 5).
func TestManifestRejectsUnknownVersion(t *testing.T) {
	c, ad, dataDir := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "provider-auth", ad.id, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bumped := strings.Replace(string(raw), `"version":1`, `"version":99`, 1)
	if err := os.WriteFile(path, []byte(bumped), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Begin(ctx, SourceDeviceAuth); err == nil {
		t.Fatal("Begin accepted an unknown manifest version")
	}
}

// TestKnownRevokedIsNeverRecoverable is the D24/D26 gate: a generation revoked
// by a coordinator action must never be offered as a restore candidate.
func TestKnownRevokedIsNeverRecoverable(t *testing.T) {
	c, ad, _ := newFixture(t)
	writeLive(t, ad, "chatgpt", 1)
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkRevoked(ctx); err != nil {
		t.Fatal(err)
	}

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.RecoveryAvailable {
		t.Fatal("a known-revoked generation must not be advertised as recoverable")
	}
	if st.BackupState != BackupReauthRequired {
		t.Fatalf("backup state = %s, want %s", st.BackupState, BackupReauthRequired)
	}
}

// TestStatusNeverLeaksSecrets proves the public status projection carries no
// paths, fingerprints, generation ids, or credential bytes (D24/D29).
func TestStatusNeverLeaksSecrets(t *testing.T) {
	c, ad, _ := newFixture(t)
	const secret = "SUPERSECRETTOKENVALUE"
	if err := os.WriteFile(ad.live, []byte(`{"mode":"chatgpt","seq":1,"tok":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{secret, ad.live, filepath.Dir(ad.live)} {
		if strings.Contains(string(blob), banned) {
			t.Fatalf("status leaked %q: %s", banned, blob)
		}
	}
}

func readManifest(t *testing.T, dataDir, provider string) *Manifest {
	t.Helper()
	m, err := loadManifest(filepath.Join(dataDir, "provider-auth", provider, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

var _ = time.Now
