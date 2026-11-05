package certs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/mholt/acmez/v3/acme"
)

const testDirectory = "https://acme.example/directory"

// wrapLikeCertmagic wraps p the way a failed DNS-01 order reaches EnsureACME's
// caller: acmez's authorization and challenge errors, certmagic's ErrNoRetry
// and Obtain wrapping, manageOne's, and EnsureACME's own.
func wrapLikeCertmagic(p acme.Problem) error {
	err := fmt.Errorf("authorization failed: %w", p)
	err = fmt.Errorf("[example.com] solving challenges: %w (order=https://acme.example/order/1)", err)
	err = certmagic.ErrNoRetry{Err: fmt.Errorf("[example.com] %w (ca=%s)", err, testDirectory)}
	err = fmt.Errorf("[example.com] Obtain: %w", err)
	err = fmt.Errorf("example.com: obtaining certificate: %w", err)
	return fmt.Errorf("certs: acme: manage example.com: %w", err)
}

func TestCooldownProblemTypes(t *testing.T) {
	counted := []string{
		acme.ProblemTypeUnauthorized, acme.ProblemTypeDNS, acme.ProblemTypeCAA, acme.ProblemTypeConnection,
		acme.ProblemTypeIncorrectResponse, acme.ProblemTypeTLS, acme.ProblemTypeCompound, acme.ProblemTypeRateLimited,
	}
	for _, typ := range counted {
		got, ok := cooldownProblem(wrapLikeCertmagic(acme.Problem{Type: typ}))
		if !ok || got != typ {
			t.Errorf("%s: cooldownProblem = %q, %v; want it to record the failure (MADR 0231 D4)", typ, got, ok)
		}
	}
	notCounted := map[string]error{
		"serverInternal":    wrapLikeCertmagic(acme.Problem{Type: acme.ProblemTypeServerInternal}),
		"malformed":         wrapLikeCertmagic(acme.Problem{Type: acme.ProblemTypeMalformed}),
		"deadline":          fmt.Errorf("certs: acme: manage example.com: %w", context.DeadlineExceeded),
		"plain":             errors.New("certs: acme: manage example.com: dial tcp: connection refused"),
		"no problem at all": nil,
	}
	for name, err := range notCounted {
		if _, ok := cooldownProblem(err); ok {
			t.Errorf("%s: cooldownProblem recorded a failure; only a validation failure or the rate limit should", name)
		}
	}
}

func TestCooldownRecordAndExpiry(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 5, 13, 53, 36, 0, time.UTC)
	if err := recordFailure(dir, testDirectory, []string{"example.com"}, acme.ProblemTypeUnauthorized, at); err != nil {
		t.Fatal(err)
	}
	rec, until, ok := activeCooldown(dir, testDirectory, []string{"example.com"}, at.Add(acmeCooldown-time.Second))
	if !ok {
		t.Fatal("not active one second before the cooldown ends")
	}
	if !until.Equal(at.Add(12 * time.Minute)) {
		t.Errorf("until = %v, want %v: the cooldown is Let's Encrypt's 12-minute refill (MADR 0231 D4)",
			until, at.Add(12*time.Minute))
	}
	if rec.Problem != acme.ProblemTypeUnauthorized {
		t.Errorf("problem = %q", rec.Problem)
	}
	if _, _, ok := activeCooldown(dir, testDirectory, []string{"example.com"}, at.Add(acmeCooldown)); ok {
		t.Error("still active once the cooldown has run")
	}
	if err := clearFailure(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cooldownPath(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("record still there after clearFailure: %v", err)
	}
	if err := clearFailure(dir); err != nil {
		t.Errorf("clearing a missing record: %v", err)
	}
}

func TestCooldownKeyedByDirectoryAndNames(t *testing.T) {
	dir := t.TempDir()
	at := time.Now()
	if err := recordFailure(dir, testDirectory, []string{"b.example", "A.example"}, acme.ProblemTypeDNS, at); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := activeCooldown(dir, testDirectory, []string{" a.example ", "b.example"}, at); !ok {
		t.Error("the same names in another order and case are not blocked")
	}
	if _, _, ok := activeCooldown(dir, "https://other.example/directory", []string{"a.example", "b.example"}, at); ok {
		t.Error("another directory is blocked")
	}
	if _, _, ok := activeCooldown(dir, testDirectory, []string{"a.example"}, at); ok {
		t.Error("another set of names is blocked")
	}
}

func TestCooldownFutureRecordIgnored(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := recordFailure(dir, testDirectory, []string{"example.com"}, acme.ProblemTypeUnauthorized,
		now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := activeCooldown(dir, testDirectory, []string{"example.com"}, now); ok {
		t.Error("a record two minutes in the future blocks an order")
	}
	if _, err := os.Stat(cooldownPath(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the future record was not removed: %v", err)
	}
}

func TestCooldownRecordIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix mode bits; the ACME directory's ACL governs")
	}
	dir := t.TempDir()
	if err := recordFailure(dir, testDirectory, []string{"example.com"}, acme.ProblemTypeUnauthorized, time.Now()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(cooldownPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("record mode = %o, want 600", mode)
	}
}
