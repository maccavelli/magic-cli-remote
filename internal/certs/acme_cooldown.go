package certs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mholt/acmez/v3/acme"
)

// A DNS-01 relay that keeps failing validation is restarted by its service
// manager, and every start places a new production order: certmagic's
// ManageSync tries once, and a start finds no certificate in storage. Let's
// Encrypt allows 5 authorization failures per name per hour, refilling 1 every
// 12 minutes, so a restart loop spends the allowance in about a minute (MADR
// 0231 F4). After a validation failure, EnsureACME records it beside
// certmagic's storage and refuses to order again for the same names and
// directory until one failure has refilled (MADR 0231 D4). A certificate
// already in storage is never blocked. HTTP-01 is not covered (the owner's
// decision).

// acmeCooldown is Let's Encrypt's refill interval: one authorization failure
// per identifier every 12 minutes. Ordering no faster than this keeps a loop
// at or under the 5-per-hour limit.
const acmeCooldown = 12 * time.Minute

// cooldownFileName is the failure record in the ACME storage directory. It
// does not collide with certmagic's FileStorage layout, whose top level holds
// only directories (certificates, acme, locks, ocsp).
const cooldownFileName = "mcremote-issuance-failure.json"

// cooldownFutureSlack is how far in the future a record's time may be before
// it is treated as written under a wrong clock, and discarded.
const cooldownFutureSlack = time.Minute

// ErrIssuanceCooldown is wrapped by EnsureACME's error when it refuses to
// place an order because a validation for the same names failed less than
// acmeCooldown ago.
var ErrIssuanceCooldown = errors.New("certs: acme: waiting out a failed validation")

// nowFunc is the clock the cooldown reads; tests replace it.
var nowFunc = time.Now

// cooldownProblemTypes are the ACME problems that record a failure: the
// validation failures Let's Encrypt counts against a name, compound being how
// several arrive together, and its refusal for the limit itself.
var cooldownProblemTypes = map[string]bool{
	acme.ProblemTypeUnauthorized:      true,
	acme.ProblemTypeDNS:               true,
	acme.ProblemTypeCAA:               true,
	acme.ProblemTypeConnection:        true,
	acme.ProblemTypeIncorrectResponse: true,
	acme.ProblemTypeTLS:               true,
	acme.ProblemTypeCompound:          true,
	acme.ProblemTypeRateLimited:       true,
}

// issuanceFailure is the record's content.
type issuanceFailure struct {
	Directory string    `json:"directory"`
	Domains   []string  `json:"domains"`
	FailedAt  time.Time `json:"failed_at"`
	Problem   string    `json:"problem"`
}

// cooldownProblem reports the ACME problem type in err's chain, and whether it
// is one that records a failure. certmagic and acmez wrap the problem with %w
// all the way out, and certmagic itself matches it with errors.As.
func cooldownProblem(err error) (string, bool) {
	var p acme.Problem
	if !errors.As(err, &p) {
		return "", false
	}
	return p.Type, cooldownProblemTypes[p.Type]
}

// cooldownKey normalizes the names a record is for: trimmed, lower case and
// sorted, so the same set in another order matches.
func cooldownKey(domains []string) []string {
	key := make([]string, 0, len(domains))
	for _, d := range domains {
		key = append(key, strings.ToLower(strings.TrimSpace(d)))
	}
	slices.Sort(key)
	return key
}

func cooldownPath(dir string) string { return filepath.Join(dir, cooldownFileName) }

// recordFailure writes the record for directory and domains, replacing any
// earlier one. It writes a temp file and renames it, so a reader never sees a
// partial record; the temp file is created 0600.
func recordFailure(dir, directory string, domains []string, problem string, now time.Time) error {
	b, err := json.Marshal(issuanceFailure{
		Directory: directory,
		Domains:   cooldownKey(domains),
		FailedAt:  now.UTC(),
		Problem:   problem,
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, cooldownFileName+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, cooldownPath(dir)); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// activeCooldown reports the record that blocks an order for directory and
// domains at now, and when it ends. A missing or unreadable record, or one for
// other names or another directory, blocks nothing. A record dated more than
// cooldownFutureSlack ahead of now was written under a wrong clock: it is
// removed and blocks nothing.
func activeCooldown(dir, directory string, domains []string, now time.Time) (issuanceFailure, time.Time, bool) {
	b, err := os.ReadFile(cooldownPath(dir))
	if err != nil {
		return issuanceFailure{}, time.Time{}, false
	}
	var rec issuanceFailure
	if json.Unmarshal(b, &rec) != nil {
		return issuanceFailure{}, time.Time{}, false
	}
	if rec.Directory != directory || !slices.Equal(rec.Domains, cooldownKey(domains)) {
		return issuanceFailure{}, time.Time{}, false
	}
	if rec.FailedAt.After(now.Add(cooldownFutureSlack)) {
		_ = os.Remove(cooldownPath(dir))
		return issuanceFailure{}, time.Time{}, false
	}
	until := rec.FailedAt.Add(acmeCooldown)
	if !now.Before(until) {
		return issuanceFailure{}, time.Time{}, false
	}
	return rec, until, true
}

// clearFailure removes the record. A missing record is not an error.
func clearFailure(dir string) error {
	if err := os.Remove(cooldownPath(dir)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// refuseDuringCooldown returns an error wrapping ErrIssuanceCooldown when a
// record blocks an order for these names and one of them has no certificate
// in storage, so ManageSync would order. stored loads a name's certificate
// from storage, as certmagic's manageOne does first; only its fs.ErrNotExist
// means "would order". Any other load error is left for ManageSync to report.
func refuseDuringCooldown(ctx context.Context, dir, directory string, domains []string, now time.Time,
	stored func(context.Context, string) error) error {
	rec, until, ok := activeCooldown(dir, directory, domains, now)
	if !ok {
		return nil
	}
	for _, d := range domains {
		if err := stored(ctx, d); errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: an order for %s failed validation at %s (%s); no new order before %s. "+
				"Fix the cause, then wait, or remove %s",
				ErrIssuanceCooldown, strings.Join(rec.Domains, ","), rec.FailedAt.Format(time.RFC3339),
				rec.Problem, until.UTC().Format(time.RFC3339), cooldownPath(dir))
		}
	}
	return nil
}
