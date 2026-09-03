package providerauth

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// Reconcile checkpoints an autonomous provider refresh into the generation
// chain (MADR 0074 D24, P19 step 3).
//
// It is the single path used by the startup, pre-mutation, post-commit, and
// watcher checkpoints, so all four make identical decisions. An observation is
// adopted only when it is stable, valid, the same mode, and strictly fresher;
// anything else leaves the generations exactly as they were. During an active
// transaction it defers rather than racing a publication it is about to see.
func (c *Coordinator) Reconcile(ctx context.Context) error {
	return c.withProviderLock(ctx, func(m *Manifest) error {
		return c.reconcileLocked(ctx, m)
	})
}

func (c *Coordinator) reconcileLocked(ctx context.Context, m *Manifest) error {
	switch m.State {
	case StatePending, StateCommitting:
		// A transaction owns LIVE's next value; defer.
		return nil
	case StateRecoveryRequired, StateLoggedOut:
		// Terminal until an operator or an explicit login acts.
		return nil
	case StateExternal:
		// Deliberately NOT terminal, and deliberately not probed here
		// (MADR 0134). Adoption is exactly how this state is left: the file
		// becoming usable again is the event that ends it, and that is the
		// event this function already handles. Probing would put a CLI spawn
		// on the watcher's per-event path, which startup recovery can afford
		// and this cannot.
	}

	cur := m.byLabel(LabelCurrent)
	if cur == nil {
		// Unmanaged: seeding is the right first checkpoint.
		return c.seedLocked(ctx, m)
	}

	obs, data, err := c.stableObservation(ctx)
	if err != nil || !obs.valid {
		// Unstable, absent, partial, or unparseable: change nothing. The next
		// checkpoint sees a settled file, and startup recovery classifies a
		// genuinely broken LIVE.
		return nil //nolint:nilerr // an unusable observation is not an error here
	}
	if obs.fp == cur.Fingerprint {
		return nil
	}
	if !obs.meta.NotOlder(c.metaOf(ctx, cur)) {
		// Older, unrelated, or incomparable. Never roll a rotated token
		// backward; startup recovery is where ambiguity gets escalated.
		// Equality is adopted rather than refused (MADR 0133): a rewrite that
		// leaves the provider's own clock alone has not gone backward.
		return nil
	}

	id, err := c.store.writeGeneration(data)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	m.Generations = append(m.Generations, Generation{
		ID:          id,
		Label:       LabelPending,
		Fingerprint: obs.fp,
		Mode:        obs.meta.Mode,
		Source:      SourceRefresh,
		CreatedAt:   now,
		ValidatedAt: now,
	})
	c.rotateLocked(m, id)
	if err := c.save(m); err != nil {
		return err
	}
	return c.store.pruneGenerations(m)
}

// stableObservation requires two identical validated reads before trusting what
// is on disk, so a torn write in progress is classified unstable rather than
// adopted (MADR 0074 D24, §17.4 stable-read bounds).
func (c *Coordinator) stableObservation(ctx context.Context) (observation, []byte, error) {
	deadline := time.Now().Add(StableReadDeadline)
	first, data, err := c.observeWithBytes(ctx)
	if err != nil {
		return observation{}, nil, err
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return observation{}, nil, ctx.Err()
		case <-time.After(StableReadInterval):
		}
		next, nextData, err := c.observeWithBytes(ctx)
		if err != nil {
			return observation{}, nil, err
		}
		if next.fp == first.fp && next.valid == first.valid {
			return next, nextData, nil
		}
		first, data = next, nextData
	}
	// Never settled inside the deadline: unstable, which is NOT the same as
	// invalid and must not be reported as it (MADR 0133). `valid` stays false
	// because nothing here may be trusted; `stable` false is what tells a
	// caller to look again rather than to escalate.
	return observation{fp: first.fp, valid: false, stable: false}, data, nil
}

func (c *Coordinator) observeWithBytes(ctx context.Context) (observation, []byte, error) {
	live, err := c.adapter.LivePath()
	if err != nil {
		return observation{}, nil, err
	}
	// Every return below is one settled read: stable is true because this
	// function reports what the file said at an instant, and stableObservation
	// is what decides whether two such instants agreed.
	fi, statErr := os.Lstat(live)
	if statErr == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return observation{valid: false, stable: true}, nil, nil
		}
		if fi.Size() > MaxCredentialBytes {
			return observation{valid: false, stable: true}, nil, nil
		}
	}
	fp, data, err := liveFingerprint(live)
	if err != nil {
		return observation{valid: false, stable: true}, nil, nil //nolint:nilerr // classified, not fatal
	}
	if fp == FingerprintAbsent {
		return observation{fp: FingerprintAbsent, stable: true}, nil, nil
	}
	if len(data) == 0 {
		// A file that exists but is empty is NOT a settled observation
		// (MADR 0133, amended 2026-09-03).
		//
		// Writers truncate before writing, so this is what a read looks like
		// when it lands inside someone else's write. The trap is that an empty
		// file's fingerprint is the hash of empty bytes — a stable value — so
		// two reads that both land in a truncate window agree, and without this
		// branch the pair is classified settled-and-invalid and escalated to a
		// terminal state. Measured at about 1 run in 8.
		//
		// A zero-length credential is never real, so the observation carries no
		// information in either direction. Note this deliberately does NOT
		// cover a file with content that fails to parse: that is genuine
		// corruption and must still escalate.
		return observation{fp: fp, valid: false, stable: false}, data, nil
	}
	meta, err := c.adapter.Validate(ctx, data)
	if err != nil {
		return observation{fp: fp, valid: false, stable: true}, data, nil //nolint:nilerr // classified, not fatal
	}
	return observation{fp: fp, meta: meta, valid: true, stable: true}, data, nil
}

// RecoverResult is one provider's outcome from RecoverAll.
type RecoverResult struct {
	Provider string
	State    State
	Err      error
}

// RecoverAll runs startup recovery for every coordinator and returns one result
// each (MADR 0074 P19 step 1).
//
// It never fails fast. An unsupported backend or a provider needing an operator
// decision is that provider's answer, not a reason to leave the others
// unexamined — the daemon must be able to report the whole picture.
func RecoverAll(ctx context.Context, coords []*Coordinator) []RecoverResult {
	out := make([]RecoverResult, 0, len(coords))
	for _, c := range coords {
		if c == nil {
			continue
		}
		st, err := c.Recover(ctx)
		out = append(out, RecoverResult{Provider: c.ProviderID(), State: st, Err: err})
	}
	return out
}

// RecoveryChoice is an operator's resolution of a preserved ambiguous state.
type RecoveryChoice string

const (
	// ChooseLive validates and adopts the observed LIVE as a new CURRENT.
	ChooseLive RecoveryChoice = "live"
	// ChooseCurrent republishes the retained CURRENT over LIVE.
	ChooseCurrent RecoveryChoice = "current"
	// ChoosePrevious republishes PREVIOUS as the new CURRENT, retaining the
	// displaced CURRENT as PREVIOUS.
	ChoosePrevious RecoveryChoice = "previous"
	// ChooseLoggedOut tombstones, then removes LIVE and every generation.
	ChooseLoggedOut RecoveryChoice = "logged-out"
)

func (r RecoveryChoice) valid() bool {
	switch r {
	case ChooseLive, ChooseCurrent, ChoosePrevious, ChooseLoggedOut:
		return true
	}
	return false
}

// ResolveRecovery applies an operator's choice to a manifest in
// recovery_required (MADR 0074 D26, P19 step 7).
//
// Every published file is validated after the write, and any failure preserves
// all evidence and leaves the manifest in recovery_required — a failed recovery
// must never be the thing that destroys the state it was meant to rescue.
func (c *Coordinator) ResolveRecovery(ctx context.Context, choice RecoveryChoice) error {
	if !choice.valid() {
		return fmt.Errorf("provider auth: unknown recovery choice %q", choice)
	}
	return c.withProviderLock(ctx, func(m *Manifest) error {
		if m.State != StateRecoveryRequired {
			return fmt.Errorf("%w: provider is not awaiting an operator decision", ErrRecoveryRequired)
		}
		// Record the attempt before acting on it. A resolution that fails
		// leaves the manifest in recovery_required, and startup re-evaluation
		// must not then decide something else on this operator's behalf
		// (MADR 0133).
		m.OperatorChoice, m.OperatorChoiceAt = choice, time.Now().UTC()
		if err := c.save(m); err != nil {
			return err
		}
		switch choice {
		case ChooseLoggedOut:
			return c.resolveLoggedOut(ctx, m)
		case ChooseLive:
			return c.resolveAdoptLive(ctx, m)
		case ChooseCurrent:
			return c.resolveRepublish(ctx, m, LabelCurrent)
		default:
			return c.resolveRepublish(ctx, m, LabelPrevious)
		}
	})
}

func (c *Coordinator) resolveLoggedOut(ctx context.Context, m *Manifest) error {
	live, err := c.adapter.LivePath()
	if err != nil {
		return err
	}
	fp, _, err := liveFingerprint(live)
	if err != nil {
		fp = FingerprintAbsent
	}
	m.clearOperatorChoice()
	m.State = StateLoggedOut
	m.LoggedOutExpected = fp
	m.LoggedOutAt = time.Now().UTC()
	m.Transaction = nil
	m.Generations = nil
	if err := c.save(m); err != nil {
		return err
	}
	if err := removeIfExists(live); err != nil {
		return err
	}
	if err := c.store.pruneGenerations(m); err != nil {
		return err
	}
	return c.store.removeStalePending("")
}

func (c *Coordinator) resolveAdoptLive(ctx context.Context, m *Manifest) error {
	obs, data, err := c.stableObservation(ctx)
	if err != nil {
		return err
	}
	if !obs.valid {
		return fmt.Errorf("%w: the observed credential is not usable", ErrInvalidCandidate)
	}
	id, err := c.store.writeGeneration(data)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	m.Generations = append(m.Generations, Generation{
		ID:          id,
		Label:       LabelPending,
		Fingerprint: obs.fp,
		Mode:        obs.meta.Mode,
		Source:      SourceSeed,
		CreatedAt:   now,
		ValidatedAt: now,
	})
	c.rotateLocked(m, id)
	m.clearOperatorChoice()
	m.State = StateIdle
	if err := c.save(m); err != nil {
		return err
	}
	return c.store.pruneGenerations(m)
}

// resolveRepublish writes a retained generation back over LIVE and verifies the
// published bytes before the manifest leaves recovery_required.
func (c *Coordinator) resolveRepublish(ctx context.Context, m *Manifest, label Label) error {
	gen := m.byLabel(label)
	if gen == nil {
		return fmt.Errorf("%w: no %s generation is retained", ErrInvalidCandidate, label)
	}
	if gen.Revoked {
		return fmt.Errorf("%w: the %s generation was revoked and cannot be restored",
			ErrInvalidCandidate, label)
	}
	data, err := c.store.readGeneration(gen.ID)
	if err != nil {
		return fmt.Errorf("%w: retained payload is unreadable", ErrInvalidCandidate)
	}
	if _, err := c.adapter.Validate(ctx, data); err != nil {
		return fmt.Errorf("%w: retained payload no longer validates", ErrInvalidCandidate)
	}

	live, err := c.adapter.LivePath()
	if err != nil {
		return err
	}
	nativeLock, err := c.adapter.NativeLockPath()
	if err != nil {
		return err
	}
	return fsutil.WithLock(nativeLock, c.opts.LockTimeout, func() error {
		if err := fsutil.WriteFileAtomic(live, data, fsutil.AtomicOptions{
			Perm: 0o600, SyncFile: true, SyncDir: true,
		}); err != nil {
			return fmt.Errorf("provider auth: republish credential: %w", err)
		}
		got, _, err := liveFingerprint(live)
		if err != nil {
			return err
		}
		if got != gen.Fingerprint {
			// Leave the manifest in recovery_required: a republish that did
			// not land is exactly the ambiguity the operator was resolving.
			return fmt.Errorf("provider auth: republished bytes did not verify")
		}
		if label == LabelPrevious {
			// Promote PREVIOUS, retaining the displaced CURRENT.
			if cur := m.byLabel(LabelCurrent); cur != nil {
				cur.Label = LabelPending // temporary; rotate fixes the labels
			}
			gen.Label = LabelCurrent
			for i := range m.Generations {
				if m.Generations[i].Label == LabelPending {
					m.Generations[i].Label = LabelPrevious
				}
			}
		}
		m.clearOperatorChoice()
		m.State = StateIdle
		if err := c.save(m); err != nil {
			return err
		}
		return c.store.pruneGenerations(m)
	})
}
