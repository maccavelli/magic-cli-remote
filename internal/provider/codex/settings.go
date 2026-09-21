package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// ConfigLayerProjection names a sanitized provenance class and version only.
type ConfigLayerProjection struct {
	Kind     string `json:"kind"`
	Version  string `json:"version,omitempty"`
	Managed  bool   `json:"managed,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// ConfigPolicyState is the bounded policy subset projected from config/read.
type ConfigPolicyState struct {
	RequestedProfileID string                  `json:"requested_profile_id,omitempty"`
	EffectiveProfileID string                  `json:"effective_profile_id,omitempty"`
	RequestedReviewer  string                  `json:"requested_reviewer,omitempty"`
	EffectiveReviewer  string                  `json:"effective_reviewer,omitempty"`
	PolicyDetail       string                  `json:"policy_detail,omitempty"`
	UserVersion        string                  `json:"-"`
	AutoDisallowed     bool                    `json:"-"`
	Layers             []ConfigLayerProjection `json:"layers,omitempty"`
}

type rawConfigRead struct {
	Config  map[string]json.RawMessage `json:"config"`
	Origins map[string]struct {
		Name struct {
			Type string `json:"type"`
		} `json:"name"`
		Version string `json:"version"`
	} `json:"origins"`
	Layers []struct {
		Name struct {
			Type string `json:"type"`
		} `json:"name"`
		Version        string                     `json:"version"`
		DisabledReason *string                    `json:"disabledReason"`
		Config         map[string]json.RawMessage `json:"config"`
	} `json:"layers"`
}

func projectConfigState(raw []byte) (ConfigPolicyState, error) {
	var input rawConfigRead
	if len(raw) == 0 || len(raw) > MaxRuntimeSnapshotBytes {
		return ConfigPolicyState{}, errors.New("config response exceeds bound")
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return ConfigPolicyState{}, err
	}
	// Not every managed requirement Codex reports becomes a runtime constraint, so
	// surfacing one as if it were enforced would be a lie an operator acts on
	// (MADR 0163 D17/F34). Measured at codex 0.155.1:
	// TryFrom<ConfigRequirementsWithSources> for ConfigRequirements
	// (config/src/config_requirements.rs:1652) destructures the managed policy and
	// DISCARDS seven fields -- allowed_permission_profiles (:1674),
	// default_permissions (:1675), allow_browser_and_computer_use (:1678),
	// browser_use (:1682), in_app_browser (:1683), apps (:1690) and models (:1697).
	// Everything else is carried into the constraint object.
	//
	// The function's own comment (:1656-1659) explains two of those: profile
	// selection and managed new-thread defaults stay on ConfigRequirementsToml
	// because they are config-load and initialization values rather than runtime
	// constraints -- so they are still honoured, just not here. The remaining five
	// have no such note, which is why anything of ours that surfaces browser,
	// computer-use or app capabilities must enforce an administrator's denial
	// itself rather than assume the engine will.
	//
	// additional_developer_instructions IS enforced, and is unsuppressable by a
	// client: it is injected as its own developer-role message. Oversized policy is
	// REJECTED rather than truncated -- validate_managed_developer_instructions
	// returns an io error above MAX_MANAGED_DEVELOPER_INSTRUCTIONS_TOKENS (10_000),
	// explicitly to avoid "silently dropping part of its instructions"
	// (core/src/context/world_state/managed_developer_instructions.rs:13,53-77).
	state := ConfigPolicyState{}
	_ = json.Unmarshal(input.Config["default_permissions"], &state.EffectiveProfileID)
	_ = json.Unmarshal(input.Config["approvals_reviewer"], &state.EffectiveReviewer)
	state.EffectiveProfileID = boundedPermissionText(state.EffectiveProfileID, 256)
	state.EffectiveReviewer = normalizeReviewer(state.EffectiveReviewer)
	for _, layer := range input.Layers {
		kind, managed := configLayerKind(layer.Name.Type)
		projection := ConfigLayerProjection{Kind: kind, Version: boundedPermissionText(layer.Version, 128), Managed: managed, Disabled: layer.DisabledReason != nil}
		state.Layers = append(state.Layers, projection)
		if kind == "user" {
			state.UserVersion = projection.Version
		}
		if !managed {
			var profile, reviewer string
			_ = json.Unmarshal(layer.Config["default_permissions"], &profile)
			_ = json.Unmarshal(layer.Config["approvals_reviewer"], &reviewer)
			if profile != "" && state.RequestedProfileID == "" {
				state.RequestedProfileID = boundedPermissionText(profile, 256)
			}
			if reviewer != "" && state.RequestedReviewer == "" {
				state.RequestedReviewer = normalizeReviewer(reviewer)
			}
		}
	}
	if state.RequestedProfileID == "" {
		state.RequestedProfileID = state.EffectiveProfileID
	}
	if state.RequestedReviewer == "" {
		state.RequestedReviewer = state.EffectiveReviewer
	}
	for _, key := range []string{"default_permissions", "approvals_reviewer"} {
		origin, ok := input.Origins[key]
		if !ok {
			continue
		}
		kind, managed := configLayerKind(origin.Name.Type)
		if managed {
			state.PolicyDetail = "Required by " + kind + " policy"
			break
		}
	}
	return state, nil
}

func configLayerKind(raw string) (string, bool) {
	switch raw {
	case "mdm", "system", "enterpriseManaged", "legacyManagedConfigTomlFromFile", "legacyManagedConfigTomlFromMdm":
		return "managed", true
	case "project":
		return "project", false
	case "user":
		return "user", false
	case "sessionFlags":
		return "session", false
	case "packagedDefaults":
		return "default", false
	default:
		return "unknown", false
	}
}

func buildPermissionConfigWrite(profileID, reviewer, expectedVersion string) (map[string]any, error) {
	profileID = strings.TrimSpace(profileID)
	reviewer = normalizeReviewer(reviewer)
	if profileID == "" {
		return nil, provider.ErrPermissionProfileInvalid
	}
	if !validReviewer(reviewer) {
		return nil, provider.ErrReviewerInvalid
	}
	edits := []map[string]any{
		{"keyPath": "default_permissions", "value": profileID, "mergeStrategy": "replace"},
		{"keyPath": "approvals_reviewer", "value": reviewer, "mergeStrategy": "replace"},
	}
	params := map[string]any{"edits": edits, "reloadUserConfig": true}
	if expectedVersion != "" {
		params["expectedVersion"] = expectedVersion
	}
	return params, nil
}

// WritePermissionDefaults atomically writes the two independent defaults.
func (p *Provider) WritePermissionDefaults(ctx context.Context, profileID, reviewer string) error {
	allowed := false
	for _, profile := range p.permissionProfileCatalog() {
		if profile.ID == profileID && profile.Allowed {
			allowed = true
			break
		}
	}
	if !allowed {
		return provider.ErrPermissionProfileInvalid
	}
	p.runtimeMu.RLock()
	version := p.config.UserVersion
	p.runtimeMu.RUnlock()
	params, err := buildPermissionConfigWrite(profileID, reviewer, version)
	if err != nil {
		return err
	}
	fr := p.framer()
	if fr == nil || !p.supportsCapability(CapabilityConfigBatchWrite) {
		return provider.ErrNotImplemented
	}
	raw, err := fr.sendRequest(ctx, "config/batchWrite", params)
	if err != nil {
		return err
	}
	var response struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || (response.Status != "ok" && response.Status != "okOverridden") {
		return fmt.Errorf("config write rejected")
	}
	return p.refreshPermissionState(ctx, "")
}
