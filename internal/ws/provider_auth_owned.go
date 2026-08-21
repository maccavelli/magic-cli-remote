package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// startOwnedDeviceAuth runs the transactional device-login path.
//
// Ordering here is the whole point of the phase. The pre-repair code called the
// provider first and decided admission afterwards, then returned early on three
// separate branches without ever starting the goroutine that owned cleanup — so
// a rejected or unwritable response left a real CLI running against the live
// credential with nothing left to undo it (MADR 0074 F3).
//
// The order is now: reserve admission, start the provider, attach the handle
// and its owner, and only then write frames. Every later failure cancels
// through the same owned handle exactly once.
func (s *Server) startOwnedDeviceAuth(
	ctx context.Context,
	c *client,
	env protocol.Envelope,
	deviceID string,
	starter provider.OwnedDeviceAuth,
	p protocol.StartAuthPayload,
) error {
	// 1. Admission first: a rejection here owns no child process.
	res, err := s.deviceFlows.Reserve(ctx, deviceID, p.ProviderID, p.UpstreamID)
	if err != nil {
		return s.writeAuthErr(ctx, c, env, err)
	}

	// 2. The flow outlives this request but not the server. Deriving from
	//    lifeCtx is what makes shutdown able to cancel it; the old
	//    context.WithoutCancel made the flow unreachable from shutdown, so a
	//    restart killed the child and destroyed the only in-memory backup
	//    (MADR 0074 F4).
	flowCtx := s.lifeCtx

	handle, err := starter.StartOwnedDeviceAuth(flowCtx, p.UpstreamID, p.MethodID, p.Inputs)
	if err != nil {
		res.Cancel()
		<-res.Done()
		return s.writeAuthErr(ctx, c, env, err)
	}

	// 3. Attach installs the owner goroutine before any frame is written.
	df := handle.Flow()
	if err := res.Attach(handle, providerauth.DeviceFlowInfo{
		VerificationURI: df.VerificationURI,
		UserCode:        df.UserCode,
		ExpiresIn:       df.ExpiresIn,
		Interval:        df.Interval,
	}); err != nil {
		handle.Cancel()
		res.Cancel()
		<-res.Done()
		return s.writeAuthErr(ctx, c, env, err)
	}

	// 4. Deliver the start frames behind a barrier, so a flow that finishes
	//    instantly cannot emit its terminal frame before the client has seen
	//    the flow it belongs to.
	published := make(chan struct{})
	go s.awaitOwnedDeviceFlow(res, p.ProviderID, published)

	fail := func(err error) error {
		close(published)
		res.Cancel()
		return err
	}
	if err := s.writeOKFrame(ctx, c, env.ID); err != nil {
		return fail(err)
	}
	out, encErr := protocol.NewEnvelope(protocol.TypeOAuthDeviceFlow, "", protocol.DeviceFlowPayload{
		FlowID:          res.ID(),
		ProviderID:      p.ProviderID,
		UpstreamID:      p.UpstreamID,
		VerificationURI: df.VerificationURI,
		UserCode:        df.UserCode,
		ExpiresIn:       res.ExpiresIn(),
		Interval:        df.Interval,
	})
	if encErr != nil {
		return fail(encErr)
	}
	if err := s.writeJSON(ctx, c, out); err != nil {
		return fail(err)
	}
	close(published)
	return nil
}

// awaitOwnedDeviceFlow forwards non-terminal updates and delivers exactly one
// terminal frame, to whichever connection the owning device currently has.
func (s *Server) awaitOwnedDeviceFlow(res *providerauth.Reservation, providerID string, published <-chan struct{}) {
	go func() {
		for state := range res.Updates() {
			<-published
			out, err := protocol.NewEnvelope(protocol.TypeOAuthDeviceFlowUpdate, "",
				protocol.DeviceFlowUpdatePayload{FlowID: res.ID(), State: state})
			if err != nil {
				continue
			}
			b, err := json.Marshal(out)
			if err != nil {
				continue
			}
			// Best effort: a phone that missed this still learns the outcome
			// from the terminal frame or from auth status on reconnect.
			s.sendToDevice(res.DeviceID(), b)
		}
	}()

	<-res.Done()
	<-published

	err := res.Result()
	state := res.State()
	payload := protocol.DeviceFlowResultPayload{
		FlowID:    res.ID(),
		OK:        err == nil,
		State:     state,
		Retryable: providerauth.Retryable(state),
	}
	if err != nil {
		payload.Error = clipAuthErr(err.Error())
		// Coordinator outcomes are classified by State; never overload
		// agenterr.KindAuth for them (MADR 0074 P20 step 9).
		if state == providerauth.ResultFailed {
			payload.ErrorKind = string(agenterr.Present(err.Error(), time.Now()).Kind)
		}
	}
	if bs, avail, ok := s.backupProjection(providerID); ok {
		payload.BackupState = bs
		payload.RecoveryAvailable = avail
	}

	// Delivered to the device's current connection, not the socket that
	// started the flow: that pointer is often dead by now (P20 step 7).
	if out, encErr := protocol.NewEnvelope(protocol.TypeOAuthDeviceFlowResult, "", payload); encErr == nil {
		if b, mErr := json.Marshal(out); mErr == nil {
			s.sendToDevice(res.DeviceID(), b)
		}
	}
	s.log.Info("device flow finished",
		slog.String("provider", providerID),
		slog.String("state", state),
		slog.Bool("ok", err == nil))

	s.pushProviderAuthStatus(context.WithoutCancel(s.lifeCtx), providerID)
}

// backupProjection reads a provider's non-secret recovery state.
func (s *Server) backupProjection(providerID string) (string, bool, bool) {
	if s.registry == nil {
		return "", false, false
	}
	prov, err := s.registry.Get(provider.ID(providerID))
	if err != nil {
		return "", false, false
	}
	a, ok := prov.(provider.Auth)
	if !ok {
		return "", false, false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.lifeCtx), 5*time.Second)
	defer cancel()
	st, err := a.AuthStatus(ctx)
	if err != nil || st.BackupState == "" {
		return "", false, false
	}
	return st.BackupState, st.RecoveryAvailable, true
}

// deviceStillConnectedLocked reports whether another live connection belongs to
// deviceID. Caller holds s.mu.
func (s *Server) deviceStillConnectedLocked(deviceID string) bool {
	for c := range s.clients {
		if c.authed && c.deviceID == deviceID {
			return true
		}
	}
	return false
}

// resumeWindow is how long a detached flow survives a disconnect. It matches
// the connection resume window the client negotiated, so a login outlives
// exactly the disconnects a session outlives and no longer.
func (s *Server) resumeWindow() time.Duration {
	if s.resume != nil {
		if w := s.resume.Window(); w > 0 {
			return w
		}
	}
	return providerauth.DefaultTTL
}
