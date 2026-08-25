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
	protocol.TypeCodexThreadsRead: {
		capability: codex.CapabilityThreadList, timeoutKey: protocol.TypeCodexThreadsRead,
		requiresSurface: true, authorize: authorizeCodexGlobal, decode: decodeCodexThreadsRead,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleCodexThreadsRead)
		},
	},
	protocol.TypeCodexThreadsWrite: {
		capability: codex.CapabilityThreadMetadata, timeoutKey: protocol.TypeCodexThreadsWrite,
		mutable: true, requiresSurface: true, authorize: authorizeCodexGlobal, decode: decodeCodexThreadsWrite,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleCodexThreadsWrite)
		},
	},
	protocol.TypeCodexExecutionRead: {
		capability: codex.CapabilityCommandExec, timeoutKey: protocol.TypeCodexExecutionRead,
		requiresSurface: true, authorize: authorizeCodexExecution, decode: decodeCodexExecutionRead,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleCodexExecutionRead)
		},
	},
	protocol.TypeCodexExecutionWrite: {
		capability: codex.CapabilityCommandExec, timeoutKey: protocol.TypeCodexExecutionWrite,
		mutable: true, requiresSurface: true, authorize: authorizeCodexExecution, decode: decodeCodexExecutionWrite,
		handle: func(s *Server, ctx context.Context, c *client, env protocol.Envelope) error {
			return s.dispatchAsync(ctx, c, env, s.handleCodexExecutionWrite)
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

func decodeCodexThreadsRead(env protocol.Envelope) (string, error) {
	var payload protocol.CodexThreadsReadPayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return "", err
	}
	switch payload.Action {
	case "list", "search", "sections", "projects":
	case "delete_preview":
		if payload.ThreadID == "" || len(payload.ThreadID) > 256 {
			return "", fmt.Errorf("bounded thread_id required")
		}
	default:
		return "", fmt.Errorf("unknown Codex thread read action")
	}
	if payload.Limit < 0 || payload.Limit > 100 || len(payload.Cursor) > 1024 || len(payload.Term) > 512 {
		return "", fmt.Errorf("invalid Codex thread read bounds")
	}
	return "", nil
}

func decodeCodexThreadsWrite(env protocol.Envelope) (string, error) {
	var payload protocol.CodexThreadsWritePayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return "", err
	}
	switch payload.Action {
	case "rename", "fork", "archive", "unarchive", "delete", "move_section", "assign_project":
		if payload.ThreadID == "" || len(payload.ThreadID) > 256 {
			return "", fmt.Errorf("bounded thread_id required")
		}
	case "create_section":
	case "update_section", "delete_section":
		if payload.SectionID == "" || len(payload.SectionID) > 256 {
			return "", fmt.Errorf("bounded section_id required")
		}
	case "create_project", "import_project":
	case "update_project", "move_project", "delete_project":
		if payload.ProjectID == "" || len(payload.ProjectID) > 256 {
			return "", fmt.Errorf("bounded project_id required")
		}
	default:
		return "", fmt.Errorf("unknown Codex thread write action")
	}
	if len(payload.Name) > 256 || len(payload.Icon) > 64 || len(payload.Color) > 64 || len(payload.BeforeID) > 256 || len(payload.Confirm) > 64 || len(payload.Roots) > 100 || len(payload.ThreadIDs) > 1000 || len(payload.Metadata) > 100 {
		return "", fmt.Errorf("invalid Codex thread write bounds")
	}
	return "", nil
}

func requireCodexCapability(p *codex.Provider, id codex.CapabilityID) error {
	if !p.SupportsCapability(id) {
		return fmt.Errorf("required Codex capability is unavailable")
	}
	return nil
}

func (s *Server) handleCodexThreadsRead(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	var payload protocol.CodexThreadsReadPayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	p, err := s.codexProvider()
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unavailable", err.Error())
	}
	result := protocol.CodexThreadsReadResultPayload{}
	switch payload.Action {
	case "list":
		if err := requireCodexCapability(p, codex.CapabilityThreadList); err != nil {
			return s.writeError(ctx, c, env.ID, "unsupported", err.Error())
		}
		page, err := p.ListNativeThreads(ctx, provider.ThreadListOptions{Cursor: payload.Cursor, Limit: payload.Limit, Archived: payload.Archived})
		if err != nil {
			return s.writeError(ctx, c, env.ID, protocol.ErrCodexThreadsReadFailed, "Codex threads could not be listed")
		}
		result.Threads = &page
	case "search":
		if !p.SupportsCapability(codex.CapabilityThreadSearch) && !p.SupportsCapability(codex.CapabilityThreadList) {
			return s.writeError(ctx, c, env.ID, "unsupported", "Codex thread search is unavailable")
		}
		page, err := p.SearchNativeThreads(ctx, provider.ThreadSearchOptions{Term: payload.Term, Cursor: payload.Cursor, Limit: payload.Limit, Archived: payload.Archived})
		if err != nil {
			return s.writeError(ctx, c, env.ID, protocol.ErrCodexThreadsReadFailed, "Codex threads could not be searched")
		}
		result.Threads = &page
	case "sections":
		if err := requireCodexCapability(p, codex.CapabilityThreadSectionList); err != nil {
			return s.writeError(ctx, c, env.ID, "unsupported", err.Error())
		}
		page, err := p.ListThreadSections(ctx, payload.Cursor, payload.Limit)
		if err != nil {
			return s.writeError(ctx, c, env.ID, protocol.ErrCodexThreadsReadFailed, "Codex thread sections could not be listed")
		}
		result.Sections = &page
	case "projects":
		if err := requireCodexCapability(p, codex.CapabilityProjectList); err != nil {
			return s.writeError(ctx, c, env.ID, "unsupported", err.Error())
		}
		page, err := p.ListProjects(ctx)
		if err != nil {
			return s.writeError(ctx, c, env.ID, protocol.ErrCodexThreadsReadFailed, "Codex projects could not be listed")
		}
		result.Projects = &page
	case "delete_preview":
		if err := requireCodexCapability(p, codex.CapabilityThreadList); err != nil {
			return s.writeError(ctx, c, env.ID, "unsupported", err.Error())
		}
		preview, err := p.PreviewDeleteNativeThread(ctx, payload.ThreadID)
		if err != nil {
			return s.writeError(ctx, c, env.ID, protocol.ErrCodexThreadsReadFailed, "Codex delete impact could not be read")
		}
		result.DeletePreview = &preview
	}
	out, _ := protocol.NewEnvelope(protocol.TypeCodexThreadsReadResult, env.ID, result)
	return s.writeJSON(ctx, c, out)
}

func projectMutationFromPhone(payload protocol.CodexThreadsWritePayload, envelopeID string) provider.ProjectMutation {
	return provider.ProjectMutation{
		Name: payload.Name, Roots: append([]string(nil), payload.Roots...), Metadata: payload.Metadata,
		ThreadIDs: append([]string(nil), payload.ThreadIDs...), IdempotencyKey: "mcremote:" + envelopeID,
	}
}

func (s *Server) handleCodexThreadsWrite(ctx context.Context, c *client, env protocol.Envelope, _ string) error {
	var payload protocol.CodexThreadsWritePayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	p, err := s.codexProvider()
	if err != nil {
		return s.writeError(ctx, c, env.ID, "unavailable", err.Error())
	}
	result := protocol.CodexThreadsWriteResultPayload{OK: true}
	fail := func(err error) error {
		if err == nil {
			return nil
		}
		return s.writeError(ctx, c, env.ID, protocol.ErrCodexThreadsWriteFailed, "Codex thread change was not applied")
	}
	switch payload.Action {
	case "rename":
		err = p.RenameNativeThread(ctx, payload.ThreadID, payload.Name)
	case "fork":
		var thread provider.AgentSessionMeta
		thread, err = p.ForkNativeThread(ctx, payload.ThreadID)
		result.Thread = &thread
	case "archive":
		err = p.ArchiveNativeThread(ctx, payload.ThreadID, true)
	case "unarchive":
		err = p.ArchiveNativeThread(ctx, payload.ThreadID, false)
	case "delete":
		if payload.Confirm != "delete permanently" {
			return s.writeError(ctx, c, env.ID, protocol.ErrConfirmRequired, "Permanent deletion requires explicit confirmation")
		}
		var deleted provider.ThreadDeleteResult
		deleted, err = p.DeleteNativeThread(ctx, payload.ThreadID)
		result.Delete = &deleted
	case "move_section":
		err = p.MoveNativeThread(ctx, payload.ThreadID, payload.SectionID, payload.BeforeID)
	case "assign_project":
		err = p.AssignNativeThreadProject(ctx, payload.ThreadID, provider.ProjectAssignment{ProjectID: payload.ProjectID, Set: payload.ProjectSet})
	case "create_section":
		var section provider.ThreadSection
		section, err = p.CreateThreadSection(ctx, provider.ThreadSectionMutation{Name: payload.Name, Icon: payload.Icon, Color: payload.Color, AppearanceSet: payload.AppearanceSet})
		result.Section = &section
	case "update_section":
		var section provider.ThreadSection
		section, err = p.UpdateThreadSection(ctx, payload.SectionID, provider.ThreadSectionMutation{Name: payload.Name, Icon: payload.Icon, Color: payload.Color, AppearanceSet: payload.AppearanceSet})
		result.Section = &section
	case "delete_section":
		if payload.Confirm != "delete section" {
			return s.writeError(ctx, c, env.ID, protocol.ErrConfirmRequired, "Section deletion requires explicit confirmation")
		}
		err = p.DeleteThreadSection(ctx, payload.SectionID)
	case "create_project":
		var project provider.Project
		project, err = p.CreateProject(ctx, projectMutationFromPhone(payload, env.ID))
		result.Project = &project
	case "import_project":
		var project provider.Project
		project, err = p.ImportProject(ctx, projectMutationFromPhone(payload, env.ID))
		result.Project = &project
	case "update_project":
		var project provider.Project
		project, err = p.UpdateProject(ctx, payload.ProjectID, projectMutationFromPhone(payload, env.ID))
		result.Project = &project
	case "move_project":
		err = p.MoveProject(ctx, payload.ProjectID, payload.BeforeID)
	case "delete_project":
		if payload.Confirm != "delete project" {
			return s.writeError(ctx, c, env.ID, protocol.ErrConfirmRequired, "Project deletion requires explicit confirmation")
		}
		err = p.DeleteProject(ctx, payload.ProjectID)
	}
	if err != nil {
		return fail(err)
	}
	out, _ := protocol.NewEnvelope(protocol.TypeCodexThreadsWriteResult, env.ID, result)
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
