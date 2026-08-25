package ws

import (
	"context"
	"fmt"
	"sort"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
)

type codexPhoneDecoder func(protocol.Envelope) (string, error)
type codexPhoneAuthorizer func(*Server, string, string) error
type codexPhoneHandler func(*Server, context.Context, *client, protocol.Envelope) error

type codexPhoneOperation struct {
	capability codex.CapabilityID
	timeoutKey string
	mutable    bool
	authorize  codexPhoneAuthorizer
	decode     codexPhoneDecoder
	handle     codexPhoneHandler
}

// codexPhoneOperations is the one registry for phone operations that exist
// specifically because the Codex provider exposes an app-server capability.
// Cross-provider session operations stay in the generic server switch.
var codexPhoneOperations = map[string]codexPhoneOperation{
	protocol.TypeSessionSetCollaboration: {
		capability: codex.CapabilityThreadSettings,
		timeoutKey: protocol.TypeSessionSetCollaboration,
		mutable:    true,
		authorize:  authorizeCodexSessionOwner,
		decode:     decodeCollaborationSessionID,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleSessionSetCollaboration)
		},
	},
}

type codexPhoneOperationInfo struct {
	Type       string
	Capability codex.CapabilityID
	TimeoutKey string
	Mutable    bool
}

func codexPhoneOperationList() []codexPhoneOperationInfo {
	out := make([]codexPhoneOperationInfo, 0, len(codexPhoneOperations))
	for typ, operation := range codexPhoneOperations {
		out = append(out, codexPhoneOperationInfo{
			Type: typ, Capability: operation.capability,
			TimeoutKey: operation.timeoutKey, Mutable: operation.mutable,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func (s *Server) handleCodexPhoneOperation(ctx context.Context, c *client, env protocol.Envelope) (bool, error) {
	operation, ok := codexPhoneOperations[env.Type]
	if !ok {
		return false, nil
	}
	sessionID, err := operation.decode(env)
	if err != nil {
		return true, s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	s.mu.Lock()
	deviceID := c.deviceID
	s.mu.Unlock()
	if err := operation.authorize(s, sessionID, deviceID); err != nil {
		return true, s.writeSessionErr(ctx, c, env.ID, "permission_denied", err)
	}
	return true, operation.handle(s, ctx, c, env)
}

func decodeCollaborationSessionID(env protocol.Envelope) (string, error) {
	var payload protocol.SessionSetCollaborationPayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return "", err
	}
	if payload.SessionID == "" {
		return "", fmt.Errorf("session_id required")
	}
	return payload.SessionID, nil
}

func authorizeCodexSessionOwner(s *Server, sessionID, deviceID string) error {
	return s.sessions.Authorize(sessionID, deviceID, false)
}
