package grok

import (
	"reflect"
	"testing"
)

// TestResumeSnapshotRequestNamesTwoMembers holds that grok's raw session/resume
// read-back carries exactly sessionId and cwd. It is the one session request
// the daemon builds as a map and not as an SDK type, so the SDK's typed
// methods, and the gate they apply to a system prompt, are not on its path
// (F3k of 0217-MADR-update-every-pin-to-its-current-release.md).
func TestResumeSnapshotRequestNamesTwoMembers(t *testing.T) {
	got := resumeSnapshotRequest("agent-1", "/work")
	want := map[string]any{"sessionId": "agent-1", "cwd": "/work"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resumeSnapshotRequest = %v, want exactly %v", got, want)
	}
}
