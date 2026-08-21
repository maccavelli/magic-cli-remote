package providerauth

import (
	"context"
	"time"
)

// Recover runs startup recovery under the provider lock, before any status read
// or mutation. It implements the exhaustive transition table in MADR 0074 P17
// step 9: every outcome is derived from durable state plus a validated
// observation of LIVE, never guessed, and any ambiguous input preserves all
// evidence and asks for an operator decision (D26).
//
// A fingerprint comparison is made only after the structural and provider
// validation checks pass, and `absent` is a distinct value rather than the
// hash of empty bytes.
func (c *Coordinator) Recover(ctx context.Context) (State, error) {
	var out State
	err := c.withProviderLock(ctx, func(m *Manifest) error {
		st, err := c.recoverLocked(ctx, m)
		if err != nil {
			return err
		}
		out = st
		return nil
	})
	return out, err
}

// observation is a validated view of LIVE at recovery time.
type observation struct {
	fp    Fingerprint
	meta  CredentialMeta
	valid bool
}

func (c *Coordinator) observeLive(ctx context.Context) (observation, error) {
	live, err := c.adapter.LivePath()
	if err != nil {
		return observation{}, err
	}
	fp, data, err := liveFingerprint(live)
	if err != nil {
		// An irregular or unreadable LIVE is an observation of "not a usable
		// credential", not a hard failure of recovery.
		return observation{fp: "", valid: false}, nil //nolint:nilerr // classified below
	}
	if fp == FingerprintAbsent {
		return observation{fp: FingerprintAbsent}, nil
	}
	meta, err := c.adapter.Validate(ctx, data)
	if err != nil {
		return observation{fp: fp, valid: false}, nil //nolint:nilerr // classified below
	}
	return observation{fp: fp, meta: meta, valid: true}, nil
}

func (c *Coordinator) recoverLocked(ctx context.Context, m *Manifest) (State, error) {
	// recovery_required is terminal until an operator acts (P19).
	if m.State == StateRecoveryRequired {
		return StateRecoveryRequired, nil
	}

	obs, err := c.observeLive(ctx)
	if err != nil {
		return "", err
	}

	switch m.State {
	case StateLoggedOut:
		return c.recoverLoggedOut(m, obs)
	case StatePending:
		return c.recoverPending(m, obs)
	case StateCommitting:
		return c.recoverCommitting(m, obs)
	default:
		return c.recoverIdle(ctx, m, obs)
	}
}

// recoverIdle reconciles an untransacted provider against what LIVE now says.
func (c *Coordinator) recoverIdle(ctx context.Context, m *Manifest, obs observation) (State, error) {
	cur := m.byLabel(LabelCurrent)
	if cur == nil {
		// Unmanaged: seed a valid LIVE, or leave a cold host alone. Neither
		// path invents a generation or a tombstone.
		if err := c.seedLocked(ctx, m); err != nil {
			return "", err
		}
		if err := c.store.removeStalePending(""); err != nil {
			return "", err
		}
		return m.State, nil
	}

	if obs.fp == cur.Fingerprint {
		if err := c.store.removeStalePending(""); err != nil {
			return "", err
		}
		return c.finish(m, StateIdle)
	}

	// A different LIVE is promoted only when it is valid, the same mode, and
	// strictly fresher. Anything else preserves every generation and asks for
	// an operator decision rather than rolling a rotated token backward (D24).
	if obs.valid && obs.meta.Fresher(c.metaOf(ctx, cur)) {
		data, err := c.readLiveBytes()
		if err != nil {
			return "", err
		}
		id, err := c.store.writeGeneration(data)
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		m.Generations = append(m.Generations, Generation{
			ID:          id,
			Label:       LabelPending, // temporary label; rotated immediately
			Fingerprint: obs.fp,
			Mode:        obs.meta.Mode,
			Source:      SourceRefresh,
			CreatedAt:   now,
			ValidatedAt: now,
		})
		c.rotateLocked(m, id)
		if err := c.save(m); err != nil {
			return "", err
		}
		if err := c.store.pruneGenerations(m); err != nil {
			return "", err
		}
		if err := c.store.removeStalePending(""); err != nil {
			return "", err
		}
		return StateIdle, nil
	}

	return c.finish(m, StateRecoveryRequired)
}

// recoverPending resolves a transaction that was still isolated. Because a
// pending transaction never touched LIVE, an unchanged LIVE means the candidate
// is simply ownerless and must be discarded — a restarted daemon never
// publishes one (P17 step 9).
func (c *Coordinator) recoverPending(m *Manifest, obs observation) (State, error) {
	if m.Transaction == nil || obs.fp != m.Transaction.ExpectedLive {
		return c.finish(m, StateRecoveryRequired)
	}
	txnID := m.Transaction.ID
	m.dropLabel(LabelPending)
	m.Transaction = nil
	m.State = StateIdle
	if err := c.save(m); err != nil {
		return "", err
	}
	if err := c.store.removeTxn(txnID); err != nil {
		return "", err
	}
	if err := c.store.pruneGenerations(m); err != nil {
		return "", err
	}
	if err := c.store.removeStalePending(""); err != nil {
		return "", err
	}
	return StateIdle, nil
}

// recoverCommitting classifies a crash around the publication rename by
// comparing LIVE with the candidate and with the expected starting value.
func (c *Coordinator) recoverCommitting(m *Manifest, obs observation) (State, error) {
	pend := m.byLabel(LabelPending)
	if m.Transaction == nil || pend == nil {
		return c.finish(m, StateRecoveryRequired)
	}
	txnID := m.Transaction.ID

	switch obs.fp {
	case pend.Fingerprint:
		// The rename landed. Finish the label move the crash interrupted.
		c.rotateLocked(m, pend.ID)
		m.Transaction = nil
		m.State = StateIdle
	case m.Transaction.ExpectedLive:
		// The rename never happened. Discard only the uncommitted candidate.
		m.dropLabel(LabelPending)
		m.Transaction = nil
		m.State = StateIdle
	default:
		// Absent, or some third value: preserve everything.
		return c.finish(m, StateRecoveryRequired)
	}

	if err := c.save(m); err != nil {
		return "", err
	}
	if err := c.store.removeTxn(txnID); err != nil {
		return "", err
	}
	if err := c.store.pruneGenerations(m); err != nil {
		return "", err
	}
	return StateIdle, nil
}

// recoverLoggedOut finishes or refuses an interrupted logout. It never
// resurrects a revoked credential and never deletes one it did not journal.
func (c *Coordinator) recoverLoggedOut(m *Manifest, obs observation) (State, error) {
	switch obs.fp {
	case FingerprintAbsent:
		if err := c.store.pruneGenerations(m); err != nil {
			return "", err
		}
		if err := c.store.removeStalePending(""); err != nil {
			return "", err
		}
		return StateLoggedOut, nil
	case m.LoggedOutExpected:
		live, err := c.adapter.LivePath()
		if err != nil {
			return "", err
		}
		if err := removeIfExists(live); err != nil {
			return "", err
		}
		if err := c.store.pruneGenerations(m); err != nil {
			return "", err
		}
		if err := c.store.removeStalePending(""); err != nil {
			return "", err
		}
		return StateLoggedOut, nil
	default:
		// Someone signed in again outside mcremote. Keep their credential and
		// the tombstone, and let an operator reconcile the two.
		return c.finish(m, StateRecoveryRequired)
	}
}

// finish persists a terminal recovery state without touching any payload.
func (c *Coordinator) finish(m *Manifest, s State) (State, error) {
	m.State = s
	if err := c.save(m); err != nil {
		return "", err
	}
	return s, nil
}

func (c *Coordinator) readLiveBytes() ([]byte, error) {
	live, err := c.adapter.LivePath()
	if err != nil {
		return nil, err
	}
	_, data, err := liveFingerprint(live)
	return data, err
}

// metaOf re-derives metadata for a retained generation so freshness is compared
// on provider terms rather than on timestamps mcremote invented.
func (c *Coordinator) metaOf(ctx context.Context, g *Generation) CredentialMeta {
	data, err := c.store.readGeneration(g.ID)
	if err != nil {
		return CredentialMeta{Mode: g.Mode}
	}
	meta, err := c.adapter.Validate(ctx, data)
	if err != nil {
		return CredentialMeta{Mode: g.Mode}
	}
	return meta
}
