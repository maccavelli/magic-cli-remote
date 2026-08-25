package codex

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const maxPermissionProfiles = 100

type permissionProfilePage struct {
	Data []struct {
		ID          string  `json:"id"`
		Description *string `json:"description"`
		Allowed     bool    `json:"allowed"`
	} `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type guardianDenial struct {
	id         string
	generation int
	event      json.RawMessage
}

func decodePermissionProfiles(raw []byte) ([]provider.PermissionProfile, string, error) {
	var page permissionProfilePage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, "", err
	}
	if len(page.Data) > maxPermissionProfiles {
		return nil, "", errors.New("permission profile page exceeds bound")
	}
	out := make([]provider.PermissionProfile, 0, len(page.Data))
	seen := make(map[string]bool, len(page.Data))
	for _, item := range page.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || len(id) > 256 || seen[id] {
			continue
		}
		seen[id] = true
		description := ""
		if item.Description != nil {
			description = boundedPermissionText(*item.Description, 512)
		}
		out = append(out, provider.PermissionProfile{
			ID: id, Description: description, Allowed: item.Allowed,
			Dangerous: id == ":danger-full-access" || id == modeFullAccess,
		})
	}
	next := ""
	if page.NextCursor != nil {
		next = boundedPermissionText(*page.NextCursor, 512)
	}
	return out, next, nil
}

func boundedPermissionText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}

func legacyPermissionProfiles(cfg Config) []provider.PermissionProfile {
	modes := availableCodexModes(cfg)
	out := make([]provider.PermissionProfile, 0, len(modes))
	for _, mode := range modes {
		out = append(out, provider.PermissionProfile{
			ID: mode.mode.ID, Description: mode.mode.Description,
			Allowed: true, Dangerous: mode.mode.Dangerous,
		})
	}
	return out
}

func defaultPermissionProfileID(profiles []provider.PermissionProfile) string {
	for _, profile := range profiles {
		if profile.Allowed && (profile.ID == ":workspace" || profile.ID == modeDefault) {
			return profile.ID
		}
	}
	for _, profile := range profiles {
		if profile.Allowed {
			return profile.ID
		}
	}
	return ""
}

func normalizeReviewer(value string) string {
	switch strings.TrimSpace(value) {
	case "", provider.ApprovalsReviewerUser:
		return provider.ApprovalsReviewerUser
	case provider.ApprovalsReviewerAutoReview, "guardian_subagent":
		return provider.ApprovalsReviewerAutoReview
	default:
		return strings.TrimSpace(value)
	}
}

func validReviewer(value string) bool {
	return value == provider.ApprovalsReviewerUser || value == provider.ApprovalsReviewerAutoReview
}

func (p *Provider) permissionProfileCatalog() []provider.PermissionProfile {
	p.runtimeMu.RLock()
	defer p.runtimeMu.RUnlock()
	return append([]provider.PermissionProfile(nil), p.profiles...)
}

func (p *Provider) refreshPermissionState(ctx context.Context, cwd string) error {
	fr := p.framer()
	if fr == nil {
		return errors.New("engine not running")
	}
	profiles := legacyPermissionProfiles(p.cfg)
	if p.supportsCapability(CapabilityPermissionProfiles) {
		var all []provider.PermissionProfile
		cursor := ""
		for len(all) < maxPermissionProfiles {
			params := map[string]any{"limit": maxPermissionProfiles}
			if cwd != "" {
				params["cwd"] = cwd
			}
			if cursor != "" {
				params["cursor"] = cursor
			}
			raw, err := fr.sendRequest(ctx, "permissionProfile/list", params)
			if err != nil {
				break
			}
			page, next, err := decodePermissionProfiles(raw)
			if err != nil {
				break
			}
			all = append(all, page...)
			if next == "" || next == cursor {
				break
			}
			cursor = next
		}
		if len(all) > 0 {
			profiles = all
		}
	}
	state := ConfigPolicyState{}
	if p.supportsCapability(CapabilityConfigRead) {
		params := map[string]any{"includeLayers": true}
		if cwd != "" {
			params["cwd"] = cwd
		}
		if raw, err := fr.sendRequest(ctx, "config/read", params); err == nil {
			state, _ = projectConfigState(raw)
		}
	}
	if p.supportsCapability(CapabilityConfigRequirements) {
		if raw, err := fr.sendRequest(ctx, "configRequirements/read", map[string]any{}); err == nil {
			var requirements struct {
				Requirements *struct {
					AllowedPermissionProfiles map[string]bool    `json:"allowedPermissionProfiles"`
					DefaultPermissions        string             `json:"defaultPermissions"`
					AllowedApprovalPolicies   *[]json.RawMessage `json:"allowedApprovalPolicies"`
				} `json:"requirements"`
			}
			if json.Unmarshal(raw, &requirements) == nil && requirements.Requirements != nil {
				for i := range profiles {
					if allowed, present := requirements.Requirements.AllowedPermissionProfiles[profiles[i].ID]; present {
						profiles[i].Allowed = profiles[i].Allowed && allowed
					}
				}
				if requirements.Requirements.AllowedApprovalPolicies != nil {
					allowsNever := false
					for _, rawPolicy := range *requirements.Requirements.AllowedApprovalPolicies {
						var policy string
						if json.Unmarshal(rawPolicy, &policy) == nil && policy == "never" {
							allowsNever = true
						}
					}
					if !allowsNever {
						state.AutoDisallowed = true
						for i := range profiles {
							if profiles[i].ID == modeAuto {
								profiles[i].Allowed = false
							}
						}
					}
				}
				if state.PolicyDetail == "" && (len(requirements.Requirements.AllowedPermissionProfiles) > 0 || requirements.Requirements.DefaultPermissions != "" || requirements.Requirements.AllowedApprovalPolicies != nil) {
					state.PolicyDetail = "Required by managed policy"
				}
			}
		}
	}
	p.runtimeMu.Lock()
	p.profiles = append([]provider.PermissionProfile(nil), profiles...)
	p.config = state
	p.runtimeMu.Unlock()
	return nil
}

func (p *Provider) unattendedAutoAllowed() bool {
	p.runtimeMu.RLock()
	defer p.runtimeMu.RUnlock()
	return !p.config.AutoDisallowed
}

func (p *Provider) defaultPermissionState() (string, string) {
	p.runtimeMu.RLock()
	defer p.runtimeMu.RUnlock()
	return p.config.EffectiveProfileID, p.config.EffectiveReviewer
}

func (s *session) applyInitialPermissionParams(params map[string]any) {
	s.mu.Lock()
	profileID := s.permissionProfileID
	reviewer := s.approvalsReviewer
	s.mu.Unlock()
	if strings.HasPrefix(profileID, ":") || (!isLegacyModeID(profileID) && profileID != "") {
		params["config"] = map[string]any{"default_permissions": profileID}
	}
	if validReviewer(reviewer) {
		params["approvalsReviewer"] = reviewer
	}
}

func isLegacyModeID(id string) bool {
	switch id {
	case modeDefault, modeReadOnly, modeAuto, modeFullAccess:
		return true
	default:
		return false
	}
}

// PermissionSettings returns copies of the current catalog and effective axes.
func (s *session) PermissionSettings() ([]provider.PermissionProfile, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.PermissionProfile(nil), s.profileCatalog...), s.permissionProfileID, s.approvalsReviewer
}

func (s *session) applyPermissionProfileState(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, profile := range s.profileCatalog {
		if profile.ID == id && profile.Allowed {
			s.permissionProfileID = id
			return nil
		}
	}
	return provider.ErrPermissionProfileInvalid
}

func (s *session) permissionProfileAllowed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, profile := range s.profileCatalog {
		if profile.ID == strings.TrimSpace(id) {
			return profile.Allowed
		}
	}
	return false
}

func (s *session) applyReviewerState(reviewer string) error {
	reviewer = normalizeReviewer(reviewer)
	if !validReviewer(reviewer) {
		return provider.ErrReviewerInvalid
	}
	s.mu.Lock()
	s.approvalsReviewer = reviewer
	s.mu.Unlock()
	return nil
}

func (s *session) updatePermissionSetting(ctx context.Context, key string, value any) error {
	if s.p == nil || !s.p.supportsCapability(CapabilityThreadSettings) {
		return provider.ErrNotImplemented
	}
	s.mu.Lock()
	if s.turnBusy {
		s.mu.Unlock()
		return provider.ErrTurnBusy
	}
	threadID, generation := s.agentID, s.engineGeneration
	s.mu.Unlock()
	fr := s.p.framer()
	if fr == nil {
		return errors.New("engine not running")
	}
	if _, err := fr.sendRequest(ctx, "thread/settings/update", map[string]any{"threadId": threadID, key: value}); err != nil {
		return err
	}
	s.mu.Lock()
	stale := s.closed || s.engineGeneration != generation
	s.mu.Unlock()
	if stale {
		return nil
	}
	return nil
}

// SetPermissionProfile changes only the named profile axis.
func (s *session) SetPermissionProfile(ctx context.Context, id string) error {
	if !s.permissionProfileAllowed(id) {
		return provider.ErrPermissionProfileInvalid
	}
	if isLegacyModeID(id) {
		return s.SetMode(ctx, id)
	}
	if err := s.updatePermissionSetting(ctx, "permissions", id); err != nil {
		return err
	}
	_ = s.applyPermissionProfileState(id)
	s.emitPermissionSettings()
	return nil
}

// SetApprovalsReviewer changes only the reviewer axis.
func (s *session) SetApprovalsReviewer(ctx context.Context, reviewer string) error {
	reviewer = normalizeReviewer(reviewer)
	if !validReviewer(reviewer) {
		return provider.ErrReviewerInvalid
	}
	if err := s.updatePermissionSetting(ctx, "approvalsReviewer", reviewer); err != nil {
		return err
	}
	_ = s.applyReviewerState(reviewer)
	s.emitPermissionSettings()
	return nil
}

func (s *session) emitPermissionSettings() {
	profiles, effective, reviewer := s.PermissionSettings()
	modes := make([]event.SessionMode, 0, len(profiles))
	for _, profile := range profiles {
		if !profile.Allowed {
			continue
		}
		modes = append(modes, event.SessionMode{ID: profile.ID, Name: profile.ID, Description: profile.Description, Dangerous: profile.Dangerous})
	}
	s.emit(event.Event{Type: event.TypeMode, SessionID: s.localID, Timestamp: time.Now().UTC(), Modes: modes, CurrentModeID: effective, ApprovalsReviewer: reviewer})
}

func (s *session) trackGuardianDenial(id string, generation int, raw json.RawMessage) {
	if id == "" || len(raw) == 0 || len(raw) > 64<<10 {
		return
	}
	s.mu.Lock()
	s.guardianDenial = &guardianDenial{id: id, generation: generation, event: append(json.RawMessage(nil), raw...)}
	s.mu.Unlock()
}

// ApproveGuardianDenied retries one exact denial and consumes it before RPC.
func (s *session) ApproveGuardianDenied(ctx context.Context) error {
	s.mu.Lock()
	denial := s.guardianDenial
	if denial == nil || denial.generation != s.engineGeneration {
		s.guardianDenial = nil
		s.mu.Unlock()
		return provider.ErrGuardianApprovalUnavailable
	}
	s.guardianDenial = nil
	threadID := s.agentID
	s.mu.Unlock()
	fr := s.p.framer()
	if fr == nil {
		return errors.New("engine not running")
	}
	var eventValue any
	if err := json.Unmarshal(denial.event, &eventValue); err != nil {
		return provider.ErrGuardianApprovalUnavailable
	}
	_, err := fr.sendRequest(ctx, "thread/approveGuardianDeniedAction", map[string]any{"threadId": threadID, "event": eventValue})
	return err
}

func sortedProfiles(in []provider.PermissionProfile) []provider.PermissionProfile {
	out := append([]provider.PermissionProfile(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var _ provider.PermissionProfileSession = (*session)(nil)
var _ provider.GuardianApprovalSession = (*session)(nil)
