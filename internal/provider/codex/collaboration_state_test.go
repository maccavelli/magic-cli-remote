package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestCollaborationModeSessionInterface(t *testing.T) {
	var _ provider.CollaborationModeSession = (*session)(nil)
}

func seededCollabSession(t *testing.T) *session {
	t.Helper()
	medium := "medium"
	s := modeTestSession(t, Config{Model: "gpt-5.6-sol"})
	s.opts.Model = "gpt-5.6-sol"
	s.collabSupported = true
	s.collabCatalog = collaborationCatalog{modes: []collaborationModeMask{
		{Name: "Plan", Mode: "plan", ReasoningEffort: &medium},
		{Name: "Default", Mode: "default"},
	}}
	s.collabMode = collaborationModeDefault
	s.engineGeneration = 1
	s.agentID = "thread-1"
	return s
}

func TestCollaborationModesDiscovery(t *testing.T) {
	s := seededCollabSession(t)
	modes, current, err := s.CollaborationModes()
	if err != nil {
		t.Fatal(err)
	}
	if current != collaborationModeDefault {
		t.Fatalf("current = %q, want default", current)
	}
	if len(modes) != 2 || modes[0].ID != "plan" || modes[1].ID != "default" {
		t.Fatalf("modes = %+v", modes)
	}
}

func TestCollaborationModesUnsupported(t *testing.T) {
	s := modeTestSession(t, Config{})
	if _, _, err := s.CollaborationModes(); !errors.Is(err, provider.ErrCollaborationUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildCollaborationSettingsPlanAndDefault(t *testing.T) {
	medium := "medium"
	mask := collaborationModeMask{Mode: "plan", ReasoningEffort: &medium}
	plan := buildCollaborationSettings(mask, "gpt-5.6-sol", "high", collaborationModePlan)
	settings := plan["settings"].(map[string]any)
	if plan["mode"] != "plan" || settings["model"] != "gpt-5.6-sol" || settings["reasoning_effort"] != "medium" || settings["developer_instructions"] != nil {
		t.Fatalf("plan settings = %#v", plan)
	}
	def := buildCollaborationSettings(collaborationModeMask{Mode: "default"}, "gpt-5.6-sol", "high", collaborationModeDefault)
	ds := def["settings"].(map[string]any)
	if def["mode"] != "default" || ds["reasoning_effort"] != "high" || ds["developer_instructions"] != nil {
		t.Fatalf("default settings = %#v", def)
	}
	defNil := buildCollaborationSettings(collaborationModeMask{Mode: "default"}, "gpt-5.6-sol", "", collaborationModeDefault)
	if defNil["settings"].(map[string]any)["reasoning_effort"] != nil {
		t.Fatalf("default with no user effort must send null: %#v", defNil)
	}
}

func TestTurnStartCollaborationPlanOmitsTopLevelEffort(t *testing.T) {
	medium := "medium"
	params := map[string]any{}
	applyCollaborationTurnParams(params, true, collaborationModeMask{Mode: "plan", ReasoningEffort: &medium}, "plan", "gpt-5.6-sol", "high")
	if _, ok := params["effort"]; ok {
		t.Fatalf("plan turn must omit top-level effort: %#v", params)
	}
	cm := params["collaborationMode"].(map[string]any)
	if cm["settings"].(map[string]any)["reasoning_effort"] != "medium" {
		t.Fatalf("plan preset missing: %#v", cm)
	}
	if _, ok := params["approvalPolicy"]; ok {
		t.Fatal("builder must not touch approval")
	}
}

func TestTurnStartCollaborationDefaultRestoresUserEffort(t *testing.T) {
	params := map[string]any{}
	applyCollaborationTurnParams(params, true, collaborationModeMask{Mode: "default"}, "default", "gpt-5.6-sol", "xhigh")
	if params["effort"] != "xhigh" {
		t.Fatalf("default turn effort = %#v", params["effort"])
	}
}

func TestThinkingDuringPlanDoesNotOverridePreset(t *testing.T) {
	s := seededCollabSession(t)
	s.collabMode = collaborationModePlan
	if err := s.SetThinkingLevel(context.Background(), "high"); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{}
	s.mu.Lock()
	applyCollaborationTurnParams(params, true, mustLookup(s.collabCatalog, "plan"), s.collabMode, s.effectiveModelLocked(), s.thinkingLevel)
	s.mu.Unlock()
	if _, ok := params["effort"]; ok {
		t.Fatal("thinking during plan must not set top-level effort")
	}
	got := params["collaborationMode"].(map[string]any)["settings"].(map[string]any)["reasoning_effort"]
	if got != "medium" {
		t.Fatalf("plan preset overwritten: %v", got)
	}
	if s.ThinkingLevel() != "high" {
		t.Fatalf("stored preference = %q", s.ThinkingLevel())
	}
}

func TestSetCollaborationModeInvalidIDNoRPC(t *testing.T) {
	s := seededCollabSession(t)
	if err := s.SetCollaborationMode(context.Background(), "bogus"); !errors.Is(err, provider.ErrCollaborationInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetCollaborationModeSameModeIdempotent(t *testing.T) {
	s := seededCollabSession(t)
	if err := s.SetCollaborationMode(context.Background(), collaborationModeDefault); err != nil {
		t.Fatal(err)
	}
}

func TestSetCollaborationModeBusy(t *testing.T) {
	s := seededCollabSession(t)
	s.turnBusy = true
	if err := s.SetCollaborationMode(context.Background(), "plan"); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetCollaborationModeUpdatesAndRetainsOnFailure(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.engineGeneration = 1

	done := make(chan error, 1)
	go func() {
		done <- s.SetCollaborationMode(context.Background(), "plan")
	}()
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "thread/settings/update" {
		t.Fatalf("method = %q", req.Method)
	}
	cm := req.Params["collaborationMode"].(map[string]any)
	if cm["mode"] != "plan" {
		t.Fatalf("update mode = %#v", cm["mode"])
	}
	settings := cm["settings"].(map[string]any)
	if settings["developer_instructions"] != nil {
		t.Fatalf("developer_instructions = %#v", settings["developer_instructions"])
	}
	if _, err := engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"result":{}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	if _, current, _ := s.CollaborationModes(); current != "plan" {
		t.Fatalf("current = %q", current)
	}

	go func() {
		done <- s.SetCollaborationMode(context.Background(), "default")
	}()
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if _, err := engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"error":{"code":-32603,"message":"boom"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failed update must error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	if _, current, _ := s.CollaborationModes(); current != "plan" {
		t.Fatalf("failed update overwrote state: %q", current)
	}
}

func TestSettingsUpdatedReconcilesMode(t *testing.T) {
	s := seededCollabSession(t)
	s.applySettingsUpdated([]byte(`{"threadId":"thread-1","threadSettings":{"collaborationMode":{"mode":"plan","settings":{"developer_instructions":"# secret"}}}}`))
	if _, current, _ := s.CollaborationModes(); current != "plan" {
		t.Fatalf("current = %q", current)
	}
	s.applySettingsUpdated([]byte(`{"threadSettings":{"collaborationMode":{"mode":"nope"}}}`))
	if _, current, _ := s.CollaborationModes(); current != "plan" {
		t.Fatalf("malformed notification changed state: %q", current)
	}
}

func TestCollaborationAndAutonomyIndependent(t *testing.T) {
	s := seededCollabSession(t)
	if err := s.SetMode(context.Background(), modeAuto); err != nil {
		t.Fatal(err)
	}
	s.collabMode = collaborationModePlan
	if ap, sb := s.policy(); ap != "never" || sb != "workspace-write" {
		t.Fatalf("plan switch must not change autonomy: %q %q", ap, sb)
	}
	if err := s.SetMode(context.Background(), modeReadOnly); err != nil {
		t.Fatal(err)
	}
	if _, current, _ := s.CollaborationModes(); current != "plan" {
		t.Fatalf("autonomy switch changed collaboration: %q", current)
	}
}

func TestTurnStartCollaborationAfterSwitch(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.collabMode = collaborationModePlan
	s.thinkingLevel = "high"

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- s.beginTurn(ctx, []provider.Content{{Type: "text", Text: "hi"}}, true)
	}()
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "turn/start" {
		t.Fatalf("method = %q", req.Method)
	}
	if _, ok := req.Params["effort"]; ok {
		t.Fatalf("plan turn leaked top-level effort: %#v", req.Params)
	}
	cm := req.Params["collaborationMode"].(map[string]any)
	if cm["mode"] != "plan" {
		t.Fatalf("turn collaboration = %#v", cm)
	}
	if _, err := engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"result":{"turn":{"id":"turn-1"}}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Log(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("beginTurn hang")
	}
}

func TestSetCollaborationModeStaleGenerationIgnored(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.engineGeneration = 1

	done := make(chan error, 1)
	go func() {
		done <- s.SetCollaborationMode(context.Background(), "plan")
	}()
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.engineGeneration = 2
	s.mu.Unlock()
	if _, err := engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"result":{}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	if _, current, _ := s.CollaborationModes(); current != collaborationModeDefault {
		t.Fatalf("stale generation resurrected state: %q", current)
	}
}

func TestNewSessionSeedsDefaultCollaboration(t *testing.T) {
	s := modeTestSession(t, Config{})
	if s.collabMode != collaborationModeDefault {
		t.Fatalf("new session collab = %q", s.collabMode)
	}
}

func TestResumeSeedsCollaborationFromStartOptions(t *testing.T) {
	medium := "medium"
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{
		generation: 1,
		collab: collaborationProbe{
			probed:    true,
			supported: true,
			catalog: collaborationCatalog{modes: []collaborationModeMask{
				{Name: "Plan", Mode: "plan", ReasoningEffort: &medium},
				{Name: "Default", Mode: "default"},
			}},
		},
	}
	s := newSession(p, Config{}, provider.StartOptions{
		LocalSessionID:      "local-1",
		CWD:                 t.TempDir(),
		CollaborationModeID: "plan",
	}, testLogger(t))
	if !s.collabSupported || s.collabMode != "plan" {
		t.Fatalf("resume seed supported=%v mode=%q", s.collabSupported, s.collabMode)
	}
}

func mustLookup(cat collaborationCatalog, id string) collaborationModeMask {
	m, ok := cat.lookup(id)
	if !ok {
		panic(id)
	}
	return m
}

func itoa64(n int64) string {
	return strings.TrimPrefix(jsonNumber(n), "")
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
