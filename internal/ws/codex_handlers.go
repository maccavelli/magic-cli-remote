package ws

import (
	"context"
	"fmt"
	"sort"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
)

type codexPhoneDecoder func(protocol.Envelope) (string, error)
type codexPhoneAuthorizer func(*Server, string, string) error
type codexPhoneHandler func(*Server, context.Context, *client, protocol.Envelope) error

type codexPhoneOperation struct {
	capability      codex.CapabilityID
	timeoutKey      string
	mutable         bool
	requiresSurface bool
	authorize       codexPhoneAuthorizer
	decode          codexPhoneDecoder
	handle          codexPhoneHandler
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
	protocol.TypeCodexRuntimeRead: {
		capability: codex.CapabilityAccountRead, timeoutKey: protocol.TypeCodexRuntimeRead,
		requiresSurface: true, authorize: authorizeCodexGlobal, decode: decodeCodexGlobal,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.handleCodexRuntimeRead(ctx, c, env)
		},
	},
	protocol.TypeCodexDoctorRun: {
		capability: codex.CapabilityServerDiagnostics, timeoutKey: protocol.TypeCodexDoctorRun,
		requiresSurface: true, authorize: authorizeCodexGlobal, decode: decodeCodexGlobal,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleCodexDoctorRun)
		},
	},
	protocol.TypeCodexPermissionsWrite: {
		capability: codex.CapabilityConfigBatchWrite, timeoutKey: protocol.TypeCodexPermissionsWrite,
		mutable: true, requiresSurface: true, authorize: authorizeCodexGlobal, decode: decodeCodexPermissionsWrite,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleCodexPermissionsWrite)
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
	if operation.requiresSurface {
		s.mu.Lock()
		authed := c.authed
		negotiated := c.negotiated
		surface := c.codexSurfaceVersion
		s.mu.Unlock()
		if !authed || negotiated < protocol.V2 || surface < 1 {
			return true, s.writeError(ctx, c, env.ID, "permission_denied", "Codex surface version 1 was not negotiated")
		}
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

func decodeCodexGlobal(env protocol.Envelope) (string, error) {
	if len(env.Payload) != 0 && string(env.Payload) != "null" && string(env.Payload) != "{}" {
		return "", fmt.Errorf("payload must be empty")
	}
	return "", nil
}

func authorizeCodexGlobal(_ *Server, _, _ string) error { return nil }

func (s *Server) codexProvider() (*codex.Provider, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("provider registry unavailable")
	}
	p, err := s.registry.Get(provider.IDCodex)
	if err != nil {
		return nil, err
	}
	codexProvider, ok := p.(*codex.Provider)
	if !ok {
		return nil, fmt.Errorf("Codex provider unavailable")
	}
	return codexProvider, nil
}

func (s *Server) handleCodexRuntimeRead(ctx context.Context, c *client, env protocol.Envelope) error {
	p, err := s.codexProvider()
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unavailable", err.Error())
	}
	_ = p.RefreshRuntime(ctx)
	out, _ := protocol.NewEnvelope(protocol.TypeCodexRuntimeResult, env.ID, p.RuntimeSnapshot())
	return s.writeJSON(ctx, c, out)
}

func (s *Server) handleCodexDoctorRun(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	p, err := s.codexProvider()
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unavailable", err.Error())
	}
	report, err := p.RunDoctor(ctx)
	if err != nil {
		return s.writeError(ctx, c, env.ID, protocol.ErrDiagnosticFailed, "Codex diagnostics failed")
	}
	out, _ := protocol.NewEnvelope(protocol.TypeCodexDoctorResult, env.ID, report)
	return s.writeJSON(ctx, c, out)
}

func decodeCodexPermissionsWrite(env protocol.Envelope) (string, error) {
	var payload protocol.CodexPermissionsWritePayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return "", err
	}
	if payload.ProfileID == "" || payload.Reviewer == "" || len(payload.ProfileID) > 256 || len(payload.Reviewer) > 32 {
		return "", fmt.Errorf("profile_id and reviewer are required")
	}
	return "", nil
}

func (s *Server) handleCodexPermissionsWrite(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	var payload protocol.CodexPermissionsWritePayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	p, err := s.codexProvider()
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unavailable", err.Error())
	}
	if err := p.WritePermissionDefaults(ctx, payload.ProfileID, payload.Reviewer); err != nil {
		return s.writeError(ctx, c, env.ID, protocol.ErrConfigWriteFailed, "Codex permission defaults were not changed")
	}
	out, _ := protocol.NewEnvelope(protocol.TypeCodexPermissionsResult, env.ID, p.RuntimeSnapshot())
	return s.writeJSON(ctx, c, out)
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
