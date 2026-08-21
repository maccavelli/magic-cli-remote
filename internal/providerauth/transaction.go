package providerauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// Coordinator bounds. All are deliberate: a device flow is an attended action,
// so nothing here should wait indefinitely.
const (
	defaultLockTimeout        = 10 * time.Second
	defaultValidationTimeout  = 30 * time.Second
	defaultActivationDeadline = 10 * time.Minute
)

// BackupState is the public, non-secret projection of credential recovery
// state (MADR 0074 D24). It carries no path, hash, generation id, or token.
type BackupState string

const (
	// BackupUnmanaged means no credential is under coordinator management.
	BackupUnmanaged BackupState = "unmanaged"
	// BackupCurrent means a validated committed generation matches LIVE.
	BackupCurrent BackupState = "current"
	// BackupPending means a transaction is in flight.
	BackupPending BackupState = "pending"
	// BackupLoggedOut means an explicit logout removed the credential.
	BackupLoggedOut BackupState = "logged_out"
	// BackupRecoveryRequired means a restorable generation exists and an
	// operator decision is needed.
	BackupRecoveryRequired BackupState = "recovery_required"
	// BackupReauthRequired is the known-revoked case: every surviving
	// generation was revoked by a coordinator action, so no restore can
	// succeed and only a fresh sign-in will work (MADR 0074 D24/F14).
	BackupReauthRequired BackupState = "reauth_required"
	// BackupUnsupported means this provider's credential store cannot be
	// observed or protected by the coordinator.
	BackupUnsupported BackupState = "unsupported"
)

// Status is what a client may see about credential backup state.
type Status struct {
	Provider          string      `json:"provider"`
	BackupState       BackupState `json:"backup_state"`
	RecoveryAvailable bool        `json:"recovery_available"`
}

// CoordinatorOptions tunes the bounds above; the zero value is the default.
type CoordinatorOptions struct {
	LockTimeout        time.Duration
	ValidationTimeout  time.Duration
	ActivationDeadline time.Duration
}

// Coordinator owns one provider's credential transactions: admission,
// immutable generations, the journal, validation, promotion, recovery, and
// cleanup (MADR 0074 D21).
//
// Lock order is fixed and never nested the other way: the coordinator's own
// provider lock, then the provider-native LIVE lock, released in reverse. No
// method called while holding a lock may call back into a lock-taking method.
type Coordinator struct {
	store   *store
	adapter Adapter
	opts    CoordinatorOptions

	// mu serializes this process's use of the provider lock. The on-disk
	// advisory lock covers other processes; this covers goroutines here.
	mu sync.Mutex
}

// NewCoordinator creates the on-disk layout for one provider under dataDir.
func NewCoordinator(dataDir string, adapter Adapter, opts CoordinatorOptions) (*Coordinator, error) {
	if adapter == nil {
		return nil, fmt.Errorf("provider auth: nil adapter")
	}
	st, err := newStore(dataDir, adapter.ProviderID())
	if err != nil {
		return nil, err
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = defaultLockTimeout
	}
	if opts.ValidationTimeout <= 0 {
		opts.ValidationTimeout = defaultValidationTimeout
	}
	if opts.ActivationDeadline <= 0 {
		opts.ActivationDeadline = defaultActivationDeadline
	}
	return &Coordinator{store: st, adapter: adapter, opts: opts}, nil
}

// withProviderLock runs fn under the coordinator's provider lock. fn must not
// call another locking Coordinator method.
func (c *Coordinator) withProviderLock(ctx context.Context, fn func(*Manifest) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return fsutil.WithLock(c.store.lockPath(), c.opts.LockTimeout, func() error {
		m, err := c.loadOrInit()
		if err != nil {
			return err
		}
		return fn(m)
	})
}

func (c *Coordinator) loadOrInit() (*Manifest, error) {
	m, err := loadManifest(c.store.manifestPath())
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		// A malformed or future manifest is never reconstructed from
		// filenames; the operator decides (P17 step 9).
		return nil, err
	}
	return newManifest(c.store.provider), nil
}

func (c *Coordinator) save(m *Manifest) error {
	return saveManifest(c.store.manifestPath(), m)
}

// Status reports the non-secret backup projection.
func (c *Coordinator) Status(ctx context.Context) (Status, error) {
	var st Status
	err := c.withProviderLock(ctx, func(m *Manifest) error {
		st = c.statusLocked(m)
		return nil
	})
	return st, err
}

func (c *Coordinator) statusLocked(m *Manifest) Status {
	st := Status{Provider: c.store.provider}
	cur := m.byLabel(LabelCurrent)
	prev := m.byLabel(LabelPrevious)

	// A generation a coordinator action revoked can never be restored, so it
	// is never advertised as recoverable (MADR 0074 D24/D26).
	restorable := func(g *Generation) bool { return g != nil && !g.Revoked }
	st.RecoveryAvailable = restorable(cur) || restorable(prev)

	switch m.State {
	case StateLoggedOut:
		st.BackupState = BackupLoggedOut
		st.RecoveryAvailable = false
	case StateRecoveryRequired:
		if st.RecoveryAvailable {
			st.BackupState = BackupRecoveryRequired
		} else {
			st.BackupState = BackupReauthRequired
		}
	case StatePending, StateCommitting:
		st.BackupState = BackupPending
	default:
		switch {
		case cur == nil:
			st.BackupState = BackupUnmanaged
		case !st.RecoveryAvailable:
			st.BackupState = BackupReauthRequired
		default:
			st.BackupState = BackupCurrent
		}
	}
	return st
}

// Seed performs first-use seeding: validate an existing LIVE and durably create
// CURRENT before any managed mutation. A cold host with no LIVE creates no
// artificial generation (MADR 0074 D24/P17 step 6).
func (c *Coordinator) Seed(ctx context.Context) error {
	return c.withProviderLock(ctx, func(m *Manifest) error {
		return c.seedLocked(ctx, m)
	})
}

func (c *Coordinator) seedLocked(ctx context.Context, m *Manifest) error {
	if m.State == StateLoggedOut {
		return c.save(m)
	}
	if m.byLabel(LabelCurrent) != nil {
		return c.save(m)
	}
	live, err := c.adapter.LivePath()
	if err != nil {
		return err
	}
	fp, data, err := liveFingerprint(live)
	if err != nil {
		return err
	}
	if fp == FingerprintAbsent {
		// Nothing to protect; leave the provider unmanaged.
		return c.save(m)
	}
	meta, err := c.adapter.Validate(ctx, data)
	if err != nil {
		// An unparseable LIVE is not seeded as known-good. Preserve it and
		// ask for an operator decision rather than blessing it.
		m.State = StateRecoveryRequired
		return c.save(m)
	}
	id, err := c.store.writeGeneration(data)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	m.Generations = append(m.Generations, Generation{
		ID:          id,
		Label:       LabelCurrent,
		Fingerprint: fp,
		Mode:        meta.Mode,
		Source:      SourceSeed,
		CreatedAt:   now,
		ValidatedAt: now,
	})
	m.State = StateIdle
	return c.save(m)
}

// Begin opens the single permitted transaction and creates its isolated,
// empty pending home. The manifest transition is synced before the child can
// run, so a crash is always classified (MADR 0074 D26).
func (c *Coordinator) Begin(ctx context.Context, src Source) (*Txn, error) {
	if !src.valid() {
		return nil, fmt.Errorf("provider auth: unknown transaction source %q", src)
	}
	var out *Txn
	err := c.withProviderLock(ctx, func(m *Manifest) error {
		switch m.State {
		case StatePending, StateCommitting:
			return ErrTransactionBusy
		case StateRecoveryRequired:
			return ErrRecoveryRequired
		}
		if err := c.seedLocked(ctx, m); err != nil {
			return err
		}
		if m.State == StateRecoveryRequired {
			return ErrRecoveryRequired
		}

		live, err := c.adapter.LivePath()
		if err != nil {
			return err
		}
		fp, _, err := liveFingerprint(live)
		if err != nil {
			return err
		}

		id := uuid.NewString()
		home, err := c.store.createPendingHome(id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		txn := &Txn{
			ID:                 id,
			ExpectedLive:       fp,
			Source:             src,
			CreatedAt:          now,
			ActivationDeadline: now.Add(c.opts.ActivationDeadline),
			home:               home,
		}
		m.Transaction = txn
		m.State = StatePending
		if err := c.save(m); err != nil {
			_ = c.store.removeTxn(id)
			return err
		}
		out = txn
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// StageCandidate structurally checks what the child wrote, parses it through
// the adapter, then copies the exact bytes into a new immutable generation
// labelled PENDING. Publication never reads from the child-writable home.
func (c *Coordinator) StageCandidate(ctx context.Context, txn *Txn) error {
	if txn == nil {
		return fmt.Errorf("provider auth: nil transaction")
	}
	return c.withProviderLock(ctx, func(m *Manifest) error {
		if m.Transaction == nil || m.Transaction.ID != txn.ID {
			return ErrTransactionBusy
		}
		candidate := c.store.txnHome(txn.ID) + string(os.PathSeparator) + c.adapter.CandidateName()
		data, err := c.store.readCandidate(candidate, c.adapter.MaxCandidateBytes())
		if err != nil {
			return err
		}
		vctx, cancel := context.WithTimeout(ctx, c.opts.ValidationTimeout)
		defer cancel()
		meta, err := c.adapter.Validate(vctx, data)
		if err != nil {
			return fmt.Errorf("%w: provider rejected the credential", ErrInvalidCandidate)
		}

		id, err := c.store.writeGeneration(data)
		if err != nil {
			return err
		}
		m.dropLabel(LabelPending)
		m.Generations = append(m.Generations, Generation{
			ID:          id,
			Label:       LabelPending,
			Fingerprint: FingerprintOf(data),
			Mode:        meta.Mode,
			Source:      m.Transaction.Source,
			CreatedAt:   time.Now().UTC(),
		})
		if err := c.save(m); err != nil {
			return err
		}
		return c.store.pruneGenerations(m)
	})
}

// ValidateCandidate runs the provider's own probe inside the isolated home.
// Only after it succeeds is PENDING considered publishable (MADR 0074 D25).
func (c *Coordinator) ValidateCandidate(ctx context.Context, txn *Txn) error {
	if txn == nil {
		return fmt.Errorf("provider auth: nil transaction")
	}
	return c.withProviderLock(ctx, func(m *Manifest) error {
		if m.Transaction == nil || m.Transaction.ID != txn.ID {
			return ErrTransactionBusy
		}
		pend := m.byLabel(LabelPending)
		if pend == nil {
			return fmt.Errorf("%w: nothing staged", ErrInvalidCandidate)
		}
		pctx, cancel := context.WithTimeout(ctx, c.opts.ValidationTimeout)
		defer cancel()
		if err := c.adapter.Probe(pctx, c.store.txnHome(txn.ID)); err != nil {
			return fmt.Errorf("%w: candidate failed its provider probe", ErrInvalidCandidate)
		}
		pend.ValidatedAt = time.Now().UTC()
		return c.save(m)
	})
}

// Commit publishes a validated PENDING generation. It re-compares LIVE under
// both locks, writes from the immutable generation through an atomic same
// directory temporary, verifies the published bytes, then rotates labels.
func (c *Coordinator) Commit(ctx context.Context, txn *Txn) error {
	if txn == nil {
		return fmt.Errorf("provider auth: nil transaction")
	}
	return c.withProviderLock(ctx, func(m *Manifest) error {
		if m.Transaction == nil || m.Transaction.ID != txn.ID {
			return ErrTransactionBusy
		}
		pend := m.byLabel(LabelPending)
		if pend == nil || pend.ValidatedAt.IsZero() {
			return fmt.Errorf("%w: candidate is not validated", ErrInvalidCandidate)
		}
		data, err := c.store.readGeneration(pend.ID)
		if err != nil {
			return fmt.Errorf("provider auth: read pending generation: %w", err)
		}

		live, err := c.adapter.LivePath()
		if err != nil {
			return err
		}
		nativeLock, err := c.adapter.NativeLockPath()
		if err != nil {
			return err
		}

		// Coordinator lock is already held; take the provider-native lock
		// second and release it first (D25 lock order).
		return fsutil.WithLock(nativeLock, c.opts.LockTimeout, func() error {
			fp, _, err := liveFingerprint(live)
			if err != nil {
				return err
			}
			if fp != m.Transaction.ExpectedLive {
				// Another writer won. Preserve everything and report a typed
				// conflict rather than discarding their newer credential.
				return ErrConflict
			}

			m.State = StateCommitting
			if err := c.save(m); err != nil {
				return err
			}

			if err := fsutil.WriteFileAtomic(live, data, fsutil.AtomicOptions{
				Perm: 0o600, SyncFile: true, SyncDir: true,
			}); err != nil {
				return fmt.Errorf("provider auth: publish credential: %w", err)
			}
			got, _, err := liveFingerprint(live)
			if err != nil {
				return err
			}
			if got != FingerprintOf(data) {
				m.State = StateRecoveryRequired
				_ = c.save(m)
				return fmt.Errorf("provider auth: published bytes did not verify")
			}

			c.rotateLocked(m, pend.ID)
			m.Transaction = nil
			m.State = StateIdle
			if err := c.save(m); err != nil {
				return err
			}
			if err := c.store.removeTxn(txn.ID); err != nil {
				return err
			}
			return c.store.pruneGenerations(m)
		})
	})
}

// rotateLocked promotes the pending generation to CURRENT, demotes the old
// CURRENT to PREVIOUS, and drops the old PREVIOUS so exactly two committed
// generations survive (MADR 0074 D23).
func (c *Coordinator) rotateLocked(m *Manifest, newCurrentID string) {
	m.dropLabel(LabelPrevious)
	if cur := m.byLabel(LabelCurrent); cur != nil {
		cur.Label = LabelPrevious
	}
	for i := range m.Generations {
		if m.Generations[i].ID == newCurrentID {
			m.Generations[i].Label = LabelCurrent
		}
	}
}

// Abort discards an in-flight transaction. It removes only that transaction's
// data and never touches LIVE, CURRENT, or PREVIOUS (MADR 0074 D26/D27).
func (c *Coordinator) Abort(ctx context.Context, txn *Txn) error {
	if txn == nil {
		return fmt.Errorf("provider auth: nil transaction")
	}
	return c.withProviderLock(ctx, func(m *Manifest) error {
		if m.Transaction == nil || m.Transaction.ID != txn.ID {
			// Already resolved; aborting twice is not an error.
			return nil
		}
		m.dropLabel(LabelPending)
		m.Transaction = nil
		if m.State != StateRecoveryRequired {
			m.State = StateIdle
		}
		if err := c.save(m); err != nil {
			return err
		}
		if err := c.store.removeTxn(txn.ID); err != nil {
			return err
		}
		return c.store.pruneGenerations(m)
	})
}

// MarkRevoked records that a coordinator action revoked the server-side grant
// behind every retained generation. They stay on disk for forensics but can
// never be restored or advertised as recoverable (MADR 0074 D24/F14).
func (c *Coordinator) MarkRevoked(ctx context.Context) error {
	return c.withProviderLock(ctx, func(m *Manifest) error {
		for i := range m.Generations {
			m.Generations[i].Revoked = true
		}
		return c.save(m)
	})
}

// RecordLogout writes the durable tombstone before removing LIVE and every
// retained payload. Ordering matters: after an explicit logout the tokens are
// revoked, so they are no longer known-good and must not survive a crash as if
// they were (MADR 0074 D24, P18 step 11).
func (c *Coordinator) RecordLogout(ctx context.Context) error {
	return c.withProviderLock(ctx, func(m *Manifest) error {
		live, err := c.adapter.LivePath()
		if err != nil {
			return err
		}
		nativeLock, err := c.adapter.NativeLockPath()
		if err != nil {
			return err
		}
		return fsutil.WithLock(nativeLock, c.opts.LockTimeout, func() error {
			fp, _, err := liveFingerprint(live)
			if err != nil {
				return err
			}

			// Tombstone first, and sync it, so an interrupted logout is
			// finished by recovery rather than resurrected.
			m.State = StateLoggedOut
			m.LoggedOutExpected = fp
			m.LoggedOutAt = time.Now().UTC()
			m.Transaction = nil
			m.Generations = nil
			if err := c.save(m); err != nil {
				return err
			}

			if fp != FingerprintAbsent {
				if err := os.Remove(live); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("provider auth: remove live credential: %w", err)
				}
			}
			if err := c.store.pruneGenerations(m); err != nil {
				return err
			}
			return c.store.removeStalePending("")
		})
	})
}
