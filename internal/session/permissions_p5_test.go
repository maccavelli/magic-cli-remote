package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestPermissionSettingsPersistAndOldRecordLoads(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record := Record{ID: "new", Provider: provider.IDCodex, PermissionProfileID: "team", ApprovalsReviewer: provider.ApprovalsReviewerAutoReview}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("new")
	if err != nil || got.PermissionProfileID != "team" || got.ApprovalsReviewer != provider.ApprovalsReviewerAutoReview {
		t.Fatalf("round trip = %+v err=%v", got, err)
	}

	oldDir := store.safeDir("old")
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(map[string]any{"id": "old", "provider": "codex", "name": "legacy"})
	if err := os.WriteFile(filepath.Join(oldDir, "meta.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.Get("old")
	if err != nil || legacy.PermissionProfileID != "" || legacy.ApprovalsReviewer != "" {
		t.Fatalf("legacy = %+v err=%v", legacy, err)
	}
}
