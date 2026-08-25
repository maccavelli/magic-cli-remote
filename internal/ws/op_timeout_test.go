package ws

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// opTimeoutTable mirrors internal/protocol/op_timeouts.json — the single
// source of truth for the phone/daemon timeout ladder (MADR 0095 D7).
type opTimeoutTable struct {
	DefaultMS      int            `json:"default_ms"`
	ClientMarginMS int            `json:"client_margin_ms"`
	Methods        map[string]int `json:"methods"`
}

func loadOpTimeouts(t *testing.T) opTimeoutTable {
	t.Helper()
	const path = "../protocol/op_timeouts.json"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tbl opTimeoutTable
	if err := json.Unmarshal(b, &tbl); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(tbl.Methods) == 0 || tbl.DefaultMS == 0 || tbl.ClientMarginMS == 0 {
		t.Fatalf("%s is missing required fields: %+v", path, tbl)
	}
	return tbl
}

// asyncDispatchedTypes lists every message type routed through
// dispatchAsync in handleMessage. Hand-maintained on purpose: adding a
// dispatchAsync case without adding it here (and to op_timeouts.json) is
// exactly the drift TestEveryAsyncDispatchedMethodIsInTheTable catches.
func asyncDispatchedTypes() []string {
	return []string{
		protocol.TypeSessionCreate,
		protocol.TypeSessionClose,
		protocol.TypeSessionDelete,
		protocol.TypeSessionRelease,
		protocol.TypeSessionClaim,
		protocol.TypeSessionPrompt,
		protocol.TypeSessionSetCollaboration,
		protocol.TypeSessionHistory,
		protocol.TypeProvidersList,
		protocol.TypeProvidersSetPrewarm,
		protocol.TypeProviderAuthCatalog,
		protocol.TypeProviderSetCredential,
		protocol.TypeProviderClearCredential,
		protocol.TypeProviderSetActiveUpstrm,
		protocol.TypeProviderStartAuth,
		protocol.TypeModelsList,
		protocol.TypeAgentsList,
		protocol.TypeAgentSessionsList,
		protocol.TypeProjectsList,
		protocol.TypeSessionShell,
		protocol.TypeSessionShareState,
		protocol.TypeSessionShare,
		protocol.TypeSessionUnshare,
		protocol.TypeSessionRefreshSkills,
		protocol.TypeWorkspaceList,
		protocol.TypeWorkspaceRead,
		protocol.TypeWorkspaceSearch,
		protocol.TypeCommandsList,
		protocol.TypeSessionFork,
		protocol.TypeSessionRevert,
		protocol.TypeSessionUnrevert,
		protocol.TypeSessionDiff,
		protocol.TypeSessionRename,
		protocol.TypeSessionDiagnostics,
		protocol.TypeCodexDoctorRun,
		protocol.TypeCodexPermissionsWrite,
		protocol.TypeCodexThreadsRead,
		protocol.TypeCodexThreadsWrite,
		protocol.TypeCodexExecutionRead,
		protocol.TypeCodexExecutionWrite,
	}
}

// asyncOpTimeout is the daemon's half of the timeout ladder (MADR 0095 D7).
// The table is shared with the phone, which must exceed every value.
func TestAsyncOpTimeoutMatchesSharedTable(t *testing.T) {
	tbl := loadOpTimeouts(t)
	for method, wantMS := range tbl.Methods {
		want := time.Duration(wantMS) * time.Millisecond
		if got := asyncOpTimeout(method); got != want {
			t.Errorf("asyncOpTimeout(%q) = %v, table says %dms", method, got, wantMS)
		}
	}
	want := time.Duration(tbl.DefaultMS) * time.Millisecond
	if got := asyncOpTimeout("no.such.method"); got != want {
		t.Errorf("default = %v, table says %dms", got, tbl.DefaultMS)
	}
}

// Every method that reaches dispatchAsync must appear in the table, so a
// new async op cannot silently inherit a deadline the phone races.
func TestEveryAsyncDispatchedMethodIsInTheTable(t *testing.T) {
	tbl := loadOpTimeouts(t)
	for _, m := range asyncDispatchedTypes() {
		if _, ok := tbl.Methods[m]; !ok {
			t.Errorf("%q reaches dispatchAsync but is absent from op_timeouts.json", m)
		}
	}
	// And nothing in the table is stale.
	known := map[string]bool{}
	for _, m := range asyncDispatchedTypes() {
		known[m] = true
	}
	for m := range tbl.Methods {
		if !known[m] {
			t.Errorf("op_timeouts.json lists %q, which no longer reaches dispatchAsync", m)
		}
	}
}
