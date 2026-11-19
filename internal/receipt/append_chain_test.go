package receipt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// failsAtFile is a real file that fails at one named step. The steps before it
// reach the disk; a failing close closes the file first and then complains.
type failsAtFile struct {
	f      *os.File
	failAt string // "write", "sync" or "close"
}

func (w failsAtFile) WriteString(s string) (int, error) {
	if w.failAt == "write" {
		return 0, errors.New("write failed")
	}
	return w.f.WriteString(s)
}

func (w failsAtFile) Sync() error {
	if w.failAt == "sync" {
		return errors.New("sync failed")
	}
	return w.f.Sync()
}

func (w failsAtFile) Close() error {
	err := w.f.Close()
	if w.failAt == "close" {
		return errors.New("close failed")
	}
	return err
}

func opensFailingAt(step string) func(string) (appendFile, error) {
	return func(path string) (appendFile, error) {
		f, err := openAppendFile(path)
		if err != nil {
			return nil, err
		}
		return failsAtFile{f: f.(*os.File), failAt: step}, nil
	}
}

func openCloseFails(path string) (appendFile, error) { return opensFailingAt("close")(path) }

// signedDecision is the signed permission-decision line for seq, chained onto
// whatever LastHash reports, without appending it.
func signedDecision(t *testing.T, s *Store, priv *ecdsa.PrivateKey, deviceID string, seq int) string {
	t.Helper()
	last, ok, err := s.LastHash(deviceID)
	if err != nil {
		t.Fatal(err)
	}
	var prev *string
	if ok {
		prev = &last
	}
	stmt, err := BuildPermissionDecisionStatement("sess-1", fmt.Sprintf("perm-%d", seq), deviceID, "once",
		"bash", fmt.Sprintf("echo %d", seq), time.Unix(int64(seq), 0).UTC(), "device:"+deviceID, prev)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := SignES256Compact(priv, payload)
	if err != nil {
		t.Fatal(err)
	}
	return compact
}

// When the close fails after a good sync, the line is in the file. Append says
// so (it returns the error), and the chain must still advance onto that line:
// otherwise the next entry names the previous line as its parent and the chain
// forks, which Verify reports as broken.
func TestAppendWhoseCloseFailsAfterAGoodSyncStillChainsTheNextEntry(t *testing.T) {
	s, _ := newTestStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	appendPermissionDecision(t, s, priv, "dev-1", 0)

	second := signedDecision(t, s, priv, "dev-1", 1)
	s.openAppend = openCloseFails
	err = s.Append("dev-1", second)
	s.openAppend = nil
	if err == nil || !strings.Contains(err.Error(), "close") {
		t.Fatalf("Append err = %v, want the close error", err)
	}
	sum := sha256.Sum256([]byte(second))
	if got, ok, lerr := s.LastHash("dev-1"); lerr != nil || !ok || got != hex.EncodeToString(sum[:]) {
		t.Fatalf("LastHash = %q, %v, %v; want the hash of the line that was written", got, ok, lerr)
	}

	appendPermissionDecision(t, s, priv, "dev-1", 2)
	b, rerr := os.ReadFile(s.path("dev-1"))
	if rerr != nil || strings.Count(string(b), "\n") != 3 {
		t.Fatalf("receipt file has %d lines (%v), want 3", strings.Count(string(b), "\n"), rerr)
	}
	broken, verr := s.Verify("dev-1", &priv.PublicKey, &daemonPriv.PublicKey)
	if verr != nil || broken != -1 {
		t.Fatalf("Verify = %d, %v; want -1 (intact): the entry after the failed close must chain onto it", broken, verr)
	}
}

// Only the path on which the line is in the chain wraps ErrCloseAfterSync: a
// failure to write or to sync does not, because the line may not be there and
// the chain has not advanced.
func TestAppendWrapsErrCloseAfterSyncOnlyWhenTheLineIsInTheChain(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		step string
		want bool
	}{{"write", false}, {"sync", false}, {"close", true}} {
		t.Run(tc.step, func(t *testing.T) {
			s, _ := newTestStore(t)
			line := signedDecision(t, s, priv, "dev-1", 0)
			s.openAppend = opensFailingAt(tc.step)
			err := s.Append("dev-1", line)
			if err == nil {
				t.Fatal("Append returned no error")
			}
			if got := errors.Is(err, ErrCloseAfterSync); got != tc.want {
				t.Fatalf("errors.Is(err, ErrCloseAfterSync) = %v, want %v (err = %v)", got, tc.want, err)
			}
		})
	}
}
