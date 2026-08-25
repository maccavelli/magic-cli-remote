package codex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestPermissionProfileCatalogBuiltInCustomAndManaged(t *testing.T) {
	raw := []byte(`{"data":[{"id":":read-only","description":null,"allowed":true},{"id":"team-safe","description":"Team policy","allowed":false},{"id":":danger-full-access","description":"No sandbox","allowed":true}],"nextCursor":null}`)
	profiles, next, err := decodePermissionProfiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(profiles) != 3 {
		t.Fatalf("profiles=%+v next=%q", profiles, next)
	}
	if profiles[1].ID != "team-safe" || profiles[1].Description != "Team policy" || profiles[1].Allowed {
		t.Fatalf("custom managed profile = %+v", profiles[1])
	}
	if !profiles[2].Dangerous {
		t.Fatalf("danger profile not marked: %+v", profiles[2])
	}
}

func TestPermissionProfileLegacyFallbackFourModes(t *testing.T) {
	profiles := legacyPermissionProfiles(Config{AllowFullAccess: true})
	want := []string{"default", "read-only", "auto", "full-access"}
	if len(profiles) != len(want) {
		t.Fatalf("profiles = %+v", profiles)
	}
	for i, id := range want {
		if profiles[i].ID != id || !profiles[i].Allowed {
			t.Fatalf("profile[%d] = %+v", i, profiles[i])
		}
	}
}

func TestPermissionReviewerAndUnattendedAxesRemainIndependent(t *testing.T) {
	for _, profile := range []string{":workspace", ":read-only"} {
		for _, sandbox := range []string{"read-only", "workspace-write", "danger-full-access"} {
			for _, reviewer := range []string{provider.ApprovalsReviewerUser, provider.ApprovalsReviewerAutoReview} {
				for _, unattended := range []bool{false, true} {
					s := modeTestSession(t, Config{AllowFullAccess: true})
					s.profileCatalog = []provider.PermissionProfile{{ID: ":workspace", Allowed: true}, {ID: ":read-only", Allowed: true}}
					s.permissionProfileID = profile
					s.approvalsReviewer = reviewer
					s.sandboxMode = sandbox
					if unattended {
						s.approvalPolicy = "never"
					} else {
						s.approvalPolicy = "on-request"
					}
					beforeApproval, beforeSandbox := s.policy()
					otherProfile := ":workspace"
					if profile == otherProfile {
						otherProfile = ":read-only"
					}
					otherReviewer := provider.ApprovalsReviewerUser
					if reviewer == otherReviewer {
						otherReviewer = provider.ApprovalsReviewerAutoReview
					}
					if err := s.applyReviewerState(otherReviewer); err != nil {
						t.Fatal(err)
					}
					if s.permissionProfileID != profile {
						t.Fatalf("reviewer changed profile: %q", s.permissionProfileID)
					}
					if err := s.applyPermissionProfileState(otherProfile); err != nil {
						t.Fatal(err)
					}
					if s.approvalsReviewer != otherReviewer {
						t.Fatalf("profile changed reviewer: %q", s.approvalsReviewer)
					}
					afterApproval, afterSandbox := s.policy()
					if beforeApproval != afterApproval || beforeSandbox != afterSandbox {
						t.Fatalf("matrix %s/%s/%s/%v widened policy: %q/%q -> %q/%q", profile, sandbox, reviewer, unattended, beforeApproval, beforeSandbox, afterApproval, afterSandbox)
					}
				}
			}
		}
	}
}

func TestSettingsNotificationIsAuthoritativeAndDropsInstructions(t *testing.T) {
	s := modeTestSession(t, Config{})
	s.profileCatalog = []provider.PermissionProfile{{ID: ":workspace", Allowed: true}, {ID: "managed", Allowed: false}}
	s.permissionProfileID = ":workspace"
	s.approvalsReviewer = provider.ApprovalsReviewerUser
	s.applySettingsUpdated([]byte(`{"threadId":"thread-1","threadSettings":{"activePermissionProfile":{"id":"managed","extends":":workspace"},"approvalsReviewer":"auto_review","approvalPolicy":"on-request","sandboxPolicy":{"type":"readOnly"},"collaborationMode":{"mode":"default","settings":{"developer_instructions":"secret"}}}}`))
	profiles, effective, reviewer := s.PermissionSettings()
	if effective != "managed" || reviewer != provider.ApprovalsReviewerAutoReview {
		t.Fatalf("effective=%q reviewer=%q profiles=%+v", effective, reviewer, profiles)
	}
	if ap, sb := s.policy(); ap != "on-request" || sb != "read-only" {
		t.Fatalf("authoritative policy = %q/%q", ap, sb)
	}
}

func TestGuardianApproveExactGenerationOneShot(t *testing.T) {
	s, engineR, engineW := seedReviewSession(t)
	s.trackGuardianDenial("review-1", 1, json.RawMessage(`{"reviewId":"review-1","review":{"status":"denied"}}`))
	done := make(chan error, 1)
	go func() { done <- s.ApproveGuardianDenied(context.Background()) }()
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "thread/approveGuardianDeniedAction" || req.Params["threadId"] != "thread-1" {
		t.Fatalf("request = %+v", req)
	}
	event := req.Params["event"].(map[string]any)
	if event["reviewId"] != "review-1" {
		t.Fatalf("event = %+v", event)
	}
	_, _ = engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"result":{}}` + "\n"))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveGuardianDenied(context.Background()); !errors.Is(err, provider.ErrGuardianApprovalUnavailable) {
		t.Fatalf("second retry = %v", err)
	}
	s.trackGuardianDenial("review-2", 1, json.RawMessage(`{"reviewId":"review-2"}`))
	s.engineGeneration = 2
	if err := s.ApproveGuardianDenied(context.Background()); !errors.Is(err, provider.ErrGuardianApprovalUnavailable) {
		t.Fatalf("stale generation retry = %v", err)
	}
}
