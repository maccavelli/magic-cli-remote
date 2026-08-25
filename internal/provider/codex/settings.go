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
