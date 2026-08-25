package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Manager-level guards for the optional surfaces added by MADR 0112 P6-P10:
// workspace inspection, skill refresh, session sharing and direct shell.
//
// Every one of them is owner-scoped and provider-optional, and the manager is
// where both checks live. They are exercised end to end by internal/ws, but a
// per-package profile does not credit that, and the ownership rule is the kind
// of thing that deserves an assertion next to the code enforcing it.

func surfaceManager(t *testing.T) *session.Manager {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, nil, nil, nil)
	t.Cleanup(func() { mgr.CloseAll(context.Background()) })
	return mgr
}

// ownedSession creates a session owned by "device-a".
func ownedSession(t *testing.T, mgr *session.Manager) string {
	t.Helper()
	meta, err := mgr.Create(context.Background(), provider.IDFake,
		provider.StartOptions{Name: "surface"}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	return meta.ID
}

// TestOptionalSurfacesRejectNonOwners is the shared rule: none of these
// operations may be driven by a device that does not own the session. They read
// files, publish transcripts and run commands, so ownership is the gate that
// matters most.
func TestOptionalSurfacesRejectNonOwners(t *testing.T) {
	mgr := surfaceManager(t)
	id := ownedSession(t, mgr)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"workspace list", func() error {
			_, err := mgr.ListWorkspace(ctx, id, "", "device-b")
			return err
		}},
		{"workspace read", func() error {
			_, err := mgr.ReadWorkspace(ctx, id, "a.txt", "device-b")
			return err
		}},
		{"workspace search", func() error {
			_, err := mgr.SearchWorkspace(ctx, id, "text", "q", "device-b")
			return err
		}},
		{"refresh skills", func() error {
			return mgr.RefreshSkills(ctx, id, "device-b")
		}},
		{"share state", func() error {
			_, err := mgr.CurrentShare(ctx, id, "device-b")
			return err
		}},
		{"share", func() error {
			_, err := mgr.Share(ctx, id, "device-b")
			return err
		}},
		{"unshare", func() error {
			return mgr.Unshare(ctx, id, "device-b")
		}},
		{"shell", func() error {
			return mgr.Shell(ctx, id, "ls", "device-b")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, session.ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}
		})
	}
}

// TestOptionalSurfacesRejectUnknownSessions proves a session id that does not
// exist cannot be probed through any of them.
func TestOptionalSurfacesRejectUnknownSessions(t *testing.T) {
	mgr := surfaceManager(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"workspace list", func() error {
			_, err := mgr.ListWorkspace(ctx, "no-such-session", "", "device-a")
			return err
		}},
		{"workspace read", func() error {
			_, err := mgr.ReadWorkspace(ctx, "no-such-session", "a.txt", "device-a")
			return err
		}},
		{"workspace search", func() error {
			_, err := mgr.SearchWorkspace(ctx, "no-such-session", "text", "q", "device-a")
			return err
		}},
		{"refresh skills", func() error {
			return mgr.RefreshSkills(ctx, "no-such-session", "device-a")
		}},
		{"share state", func() error {
			_, err := mgr.CurrentShare(ctx, "no-such-session", "device-a")
			return err
		}},
		{"share", func() error {
			_, err := mgr.Share(ctx, "no-such-session", "device-a")
			return err
		}},
		{"unshare", func() error {
			return mgr.Unshare(ctx, "no-such-session", "device-a")
		}},
		{"shell", func() error {
			return mgr.Shell(ctx, "no-such-session", "ls", "device-a")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("an unknown session was accepted")
			}
		})
	}
}

// TestWorkspaceReachesTheProvider proves the owner path delegates and returns
// the provider's answer unchanged.
func TestWorkspaceReachesTheProvider(t *testing.T) {
	mgr := surfaceManager(t)
	id := ownedSession(t, mgr)
	ctx := context.Background()

	entries, err := mgr.ListWorkspace(ctx, id, "", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the provider's listing did not reach the caller")
	}

	content, err := mgr.ReadWorkspace(ctx, id, "go.mod", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if content.Path != "go.mod" || content.Text == "" {
		t.Fatalf("content = %+v", content)
	}

	// The cap that applied differs by kind and must survive the manager.
	text, err := mgr.SearchWorkspace(ctx, id, provider.WorkspaceSearchText, "needle", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	files, err := mgr.SearchWorkspace(ctx, id, provider.WorkspaceSearchFile, "needle", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if text.Cap == files.Cap {
		t.Fatalf("both kinds reported cap %d; they differ upstream", text.Cap)
	}
}

// TestWorkspaceValidationErrorsPropagate proves a provider refusal keeps its
// sentinel so the wire layer can map it to a specific code.
func TestWorkspaceValidationErrorsPropagate(t *testing.T) {
	mgr := surfaceManager(t)
	id := ownedSession(t, mgr)
	ctx := context.Background()

	if _, err := mgr.ListWorkspace(ctx, id, "escape", "device-a"); err == nil ||
		!strings.Contains(err.Error(), "path_escape") {
		t.Fatalf("err = %v, want a path_escape refusal", err)
	}
	if _, err := mgr.ReadWorkspace(ctx, id, "binary.bin", "device-a"); err == nil ||
		!strings.Contains(err.Error(), "binary_content") {
		t.Fatalf("err = %v, want a binary_content refusal", err)
	}
	if _, err := mgr.SearchWorkspace(ctx, id, "symbol", "q", "device-a"); err == nil ||
		!strings.Contains(err.Error(), "invalid_query") {
		t.Fatalf("err = %v, want an invalid_query refusal", err)
	}
}

// TestShareReachesTheProvider proves the owner path delegates for all three
// share operations.
func TestShareReachesTheProvider(t *testing.T) {
	mgr := surfaceManager(t)
	id := ownedSession(t, mgr)
	ctx := context.Background()

	if _, err := mgr.CurrentShare(ctx, id, "device-a"); err != nil {
		t.Fatal(err)
	}
	state, err := mgr.Share(ctx, id, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Shared || !strings.HasPrefix(state.URL, "https://") {
		t.Fatalf("share = %+v", state)
	}
	if err := mgr.Unshare(ctx, id, "device-a"); err != nil {
		t.Fatal(err)
	}
}

// TestShellReachesTheProviderAndValidates proves delegation plus that a
// refusal keeps its sentinel.
func TestShellReachesTheProviderAndValidates(t *testing.T) {
	mgr := surfaceManager(t)
	id := ownedSession(t, mgr)
	ctx := context.Background()

	if err := mgr.Shell(ctx, id, "printf hi", "device-a"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Shell(ctx, id, "", "device-a"); !errors.Is(err, provider.ErrShellInvalid) {
		t.Fatalf("empty command err = %v, want ErrShellInvalid", err)
	}
	if err := mgr.Shell(ctx, id, "fixture-disabled", "device-a"); !errors.Is(err, provider.ErrShellDisabled) {
		t.Fatalf("disabled err = %v, want ErrShellDisabled", err)
	}
	if err := mgr.Shell(ctx, id, "fixture-busy", "device-a"); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("busy err = %v, want ErrTurnBusy", err)
	}
}

// TestRefreshSkillsReachesTheProvider proves the owner path delegates.
func TestRefreshSkillsReachesTheProvider(t *testing.T) {
	mgr := surfaceManager(t)
	id := ownedSession(t, mgr)
	if err := mgr.RefreshSkills(context.Background(), id, "device-a"); err != nil {
		t.Fatal(err)
	}
}
