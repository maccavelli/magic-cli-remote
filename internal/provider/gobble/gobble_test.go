package gobble

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// sessionNew is a session/new answer in gobble's shape: a grouped model
// option (recent first), a thinking option, and modes with bypass.
const sessionNew = `{"sessionId":"agent-1",
 "modes":{"currentModeId":"default","availableModes":[
  {"id":"default","name":"Default"},{"id":"plan","name":"Plan"},{"id":"bypass","name":"Bypass"}]},
 "configOptions":[
  {"id":"model","name":"Model","category":"model","type":"select","currentValue":"openai/gpt-x","options":[
   {"group":"recent","name":"Recent","options":[{"value":"openai/gpt-x","name":"GPT X"}]},
   {"group":"openai","name":"OpenAI","options":[{"value":"openai/gpt-x","name":"GPT X"},{"value":"openai/gpt-y","name":"GPT Y"}]}]},
  {"id":"thought_level","name":"Thinking","category":"thought_level","type":"select","currentValue":"off","options":[
   {"value":"off","name":"off"},{"value":"high","name":"high"}]}]}`

// start is a gobble session on acptest's agent, with openai ready on the stub.
func start(t *testing.T, opts acptest.Options) (*stub, provider.Session, *acptest.Agent) {
	t.Helper()
	st := newStub(t, stubStatus(sourceAuthJSON, "openai"))
	if opts.AuthMethods == nil {
		opts.AuthMethods = safeAuthMethodIDs()
	}
	if opts.Replies == nil {
		opts.Replies = map[string]string{}
	}
	if _, ok := opts.Replies["session/new"]; !ok {
		opts.Replies["session/new"] = sessionNew
	}
	s, a := acptest.Start(t, newProvider(st), opts)
	return st, s, a
}

// Item 1, with A2: initialize advertises no client files, terminals or
// terminal sign-in, and does advertise compaction and notices.
func TestInitializeAdvertisesNoClientFilesOrTerminals(t *testing.T) {
	_, _, a := start(t, acptest.Options{})
	caps, _ := a.ParamsFor("initialize")["clientCapabilities"].(map[string]any)
	if caps == nil {
		t.Fatal("initialize sent no clientCapabilities")
	}
	fsCaps, _ := caps["fs"].(map[string]any)
	if fsCaps["readTextFile"] == true || fsCaps["writeTextFile"] == true {
		t.Errorf("fs = %v, want neither read nor write", fsCaps)
	}
	if caps["terminal"] == true {
		t.Error("terminal advertised")
	}
	if auth, _ := caps["auth"].(map[string]any); auth["terminal"] == true {
		t.Error("auth.terminal advertised")
	}
	session, _ := caps["session"].(map[string]any)
	for _, k := range []string{"compaction", "notices"} {
		if _, ok := session[k]; !ok {
			t.Errorf("session.%s not advertised; gobble would fall back to its own notification", k)
		}
	}
}

// Item 2: the handshake authenticates the upstream that has a credential.
func TestHandshakeAuthenticatesTheReadyUpstream(t *testing.T) {
	_, _, a := start(t, acptest.Options{})
	if got := a.ParamsFor("authenticate")["methodId"]; got != "openai" {
		t.Fatalf("authenticate methodId = %v, want openai, the one ready upstream", got)
	}
}

// Item 2: with no credential, Start fails ErrACPAuthRequired, sending no
// authenticate and no session/new.
func TestNoCredentialFailsBeforeAuthenticate(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	a := &authAgent{}
	_, err := startOnAgent(t, newProvider(st), a)
	if !errors.Is(err, acpagent.ErrACPAuthRequired) {
		t.Fatalf("StartOn err = %v, want ErrACPAuthRequired", err)
	}
	if got := a.got(); !slices.Equal(got, []string{"initialize"}) {
		t.Fatalf("the agent received %v, want initialize only", got)
	}
}

// Item 6, with A6: a handshake whose authenticate meets gobble's -32000 fails
// Start with text holding the command gobble names.
func TestHandshakeAuthErrorNamesGobblesCommand(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON, "openai"))
	data, err := json.Marshal(acpext.ErrorData{Command: "gobble auth login openai --api-key -", Provider: "openai", Reason: "no credential"})
	if err != nil {
		t.Fatal(err)
	}
	a := &authAgent{authErr: `{"code":-32000,"message":"Authentication required","data":` + string(data) + `}`}
	_, startErr := startOnAgent(t, newProvider(st), a)
	if startErr == nil || !strings.Contains(startErr.Error(), "gobble auth login openai --api-key -") {
		t.Fatalf("StartOn err = %v, want it to name gobble's command", startErr)
	}
}

// Item 6: a prompt that meets -32000 is presented KindAuth.
func TestPromptAuthErrorIsKindAuth(t *testing.T) {
	_, s, a := start(t, acptest.Options{})
	acptest.Drain(s)
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	m := a.Await("session/prompt")
	a.RespondErrorData(m, -32000, "Authentication required", acpext.ErrorData{Error: "no credential for openai"})
	ev := acptest.Next(t, s, event.TypeError)
	if ev.ErrorKind != "auth" {
		t.Fatalf("error kind = %q (%q), want auth", ev.ErrorKind, ev.Error)
	}
}

// Item 3: the session declares Rename and Fork, model and thinking from the
// options, and no other operation.
func TestSessionDeclaresRenameForkModelAndThinking(t *testing.T) {
	_, s, _ := start(t, acptest.Options{})
	var got []provider.Capability
	for c, ok := range s.Capabilities() {
		if ok {
			got = append(got, c)
		}
	}
	slices.Sort(got)
	want := []provider.Capability{provider.CapFork, provider.CapRename, provider.CapSetModel, provider.CapSetThinkingLevel}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("declared %v, want %v", got, want)
	}
}

// Item 4: Rename and Fork send gobble's requests and map the answers.
func TestRenameAndForkSendGobblesRequests(t *testing.T) {
	_, s, a := start(t, acptest.Options{Replies: map[string]string{
		acpext.MethodSetSessionName: `{}`,
		acpext.MethodClone:          `{"sessionId":"agent-2"}`,
	}})
	if err := s.(provider.RenameSession).Rename(context.Background(), "A name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if p := a.ParamsFor(acpext.MethodSetSessionName); p["sessionId"] != "agent-1" || p["name"] != "A name" {
		t.Errorf("rename params = %v, want sessionId agent-1 and name", p)
	}
	res, err := s.(provider.ForkSession).Fork(context.Background(), provider.ForkOptions{LastTurnID: "ignored"})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if res != (provider.ForkResult{AgentSessionID: "agent-2", ForkedFromID: "agent-1"}) {
		t.Errorf("fork = %+v, want agent-2 forked from agent-1", res)
	}
	if p := a.ParamsFor(acpext.MethodClone); len(p) != 1 || p["sessionId"] != "agent-1" {
		t.Errorf("clone params = %v, want sessionId alone", p)
	}
}

// Item 4: during a prompt, gobble refuses Rename with -32600, and that is
// Rename's error.
func TestRenameDuringATurnReturnsGobblesRefusal(t *testing.T) {
	_, s, a := start(t, acptest.Options{Hold: []string{acpext.MethodSetSessionName}})
	done := make(chan error, 1)
	go func() { done <- s.(provider.RenameSession).Rename(context.Background(), "A name") }()
	m := a.Await(acpext.MethodSetSessionName)
	a.RespondErrorData(m, -32600, "Invalid request", acpext.ErrorData{SessionID: "agent-1", Reason: "a prompt is already running"})
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "a prompt is already running") {
			t.Fatalf("Rename err = %v, want gobble's refusal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Rename did not return")
	}
}

// notices are the texts of the notice events in evs.
func notices(evs []event.Event) []string {
	var out []string
	for _, ev := range evs {
		if ev.Type == event.TypeNotice {
			out = append(out, ev.Text)
		}
	}
	return out
}

// waitEvents drains s until want notices have arrived, or a second passes,
// and returns every event it drained, not the notices alone.
func waitEvents(s provider.Session, want int) []event.Event {
	var got []event.Event
	deadline := time.Now().Add(time.Second)
	for len(notices(got)) < want && time.Now().Before(deadline) {
		got = append(got, acptest.Drain(s)...)
		time.Sleep(10 * time.Millisecond)
	}
	return append(got, acptest.Drain(s)...)
}

// waitNotices is waitEvents' notices.
func waitNotices(s provider.Session, want int) []string {
	return notices(waitEvents(s, want))
}

// Item 5, with A1 and A2: a retry's start and failed end are notices naming
// the cause as sent, a cause acpext does not name included; a successful end
// says nothing; a _gobble/compaction produces no event.
func TestRetryNoticesAndNoCompactionNotification(t *testing.T) {
	_, s, a := start(t, acptest.Options{})
	acptest.Drain(s)
	// First: notifications are handled in order, so any event it made would
	// come before the notices the wait below counts.
	a.Notify(acpext.MethodCompaction, map[string]any{"sessionId": "agent-1", "phase": "start", "reason": "manual"})
	a.Notify(acpext.MethodRetry, acpext.RetryNotice{SessionID: "agent-1", Phase: acpext.PhaseStart,
		Cause: acpext.RetryRateLimited, Attempt: 2, MaxAttempts: 5, DelayMs: 4000})
	a.Notify(acpext.MethodRetry, acpext.RetryNotice{SessionID: "agent-1", Phase: acpext.PhaseStart,
		Cause: "a-cause-gobble-adds-later", Attempt: 3})
	a.Notify(acpext.MethodRetry, acpext.RetryNotice{SessionID: "agent-1", Phase: acpext.PhaseEnd})
	a.Notify(acpext.MethodRetry, acpext.RetryNotice{SessionID: "agent-1", Phase: acpext.PhaseEnd, Error: "quota exhausted"})
	evs := waitEvents(s, 3)
	got := notices(evs)
	want := []string{
		"gobble is retrying (attempt 2 of 5) in 4s: rate-limited.",
		"gobble is retrying (attempt 3): a-cause-gobble-adds-later.",
		"gobble stopped retrying: quota exhausted",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("notices = %q, want %q", got, want)
	}
	for _, ev := range evs {
		if ev.Type == event.TypeCompaction {
			t.Fatalf("a _gobble/compaction produced %+v; gobble's compactions come as compaction_update", ev)
		}
	}
}

// metaFrame is acpext's recorded CompactionMeta frame name, as JSON.
func metaFrame(t *testing.T, name string) json.RawMessage {
	t.Helper()
	dir := filepath.Join(acpextDir(t), "testdata", name)
	b, err := os.ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// acpextDir is the directory of the acpext this module requires, as go
// reports it, so its recorded frames are the ones the build uses.
func acpextDir(t *testing.T) string {
	t.Helper()
	out, err := procutil.Command(context.Background(), "go", "list", "-m", "-f", "{{.Dir}}",
		"github.com/maccavelli/gobble-cli/acpext").Output()
	if err != nil {
		t.Fatalf("go list acpext: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// compactionUpdate is a compaction_update carrying meta under _meta.gobble.
func compactionUpdate(id, status string, meta json.RawMessage) map[string]any {
	u := map[string]any{"sessionUpdate": "compaction_update", "compactionId": id, "status": status}
	if meta != nil {
		u["_meta"] = map[string]json.RawMessage{acpext.MetaKey: meta}
	}
	return map[string]any{"sessionId": "agent-1", "update": u}
}

// Item 12, A2: a live completed compaction's meta is one notice naming its
// reason and the tokens before; the start, a pause and a replay are none;
// meta that does not decode is declined, with no notice.
func TestCompactionMetaNotice(t *testing.T) {
	var end acpext.CompactionMeta
	if err := json.Unmarshal(metaFrame(t, "compaction.meta.end.json"), &end); err != nil {
		t.Fatal(err)
	}
	if end.Reason == "" || end.TokensBefore <= 0 {
		t.Fatalf("acpext's end frame is %+v; this test needs a live completed one", end)
	}
	replay, err := json.Marshal(acpext.CompactionMeta{TokensBefore: end.TokensBefore})
	if err != nil {
		t.Fatal(err)
	}
	future, err := json.Marshal(acpext.CompactionMeta{Reason: "a-reason-gobble-adds-later", TokensBefore: 99})
	if err != nil {
		t.Fatal(err)
	}

	_, s, a := start(t, acptest.Options{})
	acptest.Drain(s)
	a.Notify("session/update", compactionUpdate("c1", "in_progress", metaFrame(t, "compaction.meta.start.json")))
	a.Notify("session/update", compactionUpdate("c1", "completed", metaFrame(t, "compaction.meta.end.json")))
	a.Notify("session/update", compactionUpdate("c2", "failed", metaFrame(t, "compaction.meta.paused.json")))
	a.Notify("session/update", compactionUpdate("c1", "completed", replay))
	a.Notify("session/update", compactionUpdate("c3", "completed", json.RawMessage(`"not an object"`)))
	a.Notify("session/update", compactionUpdate("c4", "completed", future))
	got := waitNotices(s, 2)
	want := []string{
		"Context compacted (" + string(end.Reason) + "), from " + strconv.FormatInt(end.TokensBefore, 10) + " tokens.",
		"Context compacted (a-reason-gobble-adds-later), from 99 tokens.",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("notices = %q, want %q", got, want)
	}
}

// The catalog keeps gobble's groups in gobble's order, recent first, and a
// model in recent and in its provider's group is listed under each.
func TestModelsKeepGobblesGroups(t *testing.T) {
	in := []picker.Option{
		{ID: "openai/gpt-x", Label: "GPT X", Group: "recent"},
		{ID: "openai/gpt-x", Label: "GPT X", Group: "openai"},
		{ID: "openai/gpt-y", Label: "GPT Y", Group: "openai"},
	}
	if got := modelsFromOption(in); !reflect.DeepEqual(got, in) {
		t.Fatalf("models = %+v, want gobble's %+v", got, in)
	}
}

// Confirmation 4: gobble's bypass is marked dangerous, so the phone confirms
// it.
func TestBypassIsDangerous(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON, "openai"))
	s, err := startOnAgent(t, newProvider(st), &authAgent{newReply: sessionNew})
	if err != nil {
		t.Fatalf("StartOn: %v", err)
	}
	var modes []event.SessionMode
	deadline := time.After(5 * time.Second)
	for len(modes) == 0 {
		select {
		case ev := <-s.Events():
			modes = ev.Modes
		case <-deadline:
			t.Fatal("no modes advertised")
		}
	}
	for _, m := range modes {
		if m.Dangerous != (m.ID == "bypass") {
			t.Errorf("mode %q dangerous = %v, want bypass alone dangerous", m.ID, m.Dangerous)
		}
	}
}

// Item 11: ListModels starts no gobble process.
func TestListModelsStartsNoProcess(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	if _, err := newProvider(st).ListModels(context.Background()); err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if runs := st.runs(t); len(runs) != 0 {
		t.Fatalf("ListModels ran gobble: %q", runs)
	}
}

// A3: the Spec bounds session/close at gobble's own wait plus a second.
func TestCloseTimeoutCoversGobblesWait(t *testing.T) {
	if got := spec(acpagent.Config{}, nil).CloseTimeout; got != 6*time.Second {
		t.Fatalf("CloseTimeout = %s, want 6s", got)
	}
}

// Item 10: only this package imports acpext, and it imports no
// go-llmprovider-sdk.
func TestOnlyThisPackageImportsAcpext(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	self, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	const acpextPath = "github.com/maccavelli/gobble-cli/acpext"
	const sdkPrefix = "github.com/maccavelli/go-llmprovider-sdk"
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		inSelf := filepath.Dir(path) == self
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == acpextPath && !inSelf {
				t.Errorf("%s imports acpext; only internal/provider/gobble may (MADR 0179 D13)", path)
			}
			if inSelf && strings.HasPrefix(p, sdkPrefix) {
				t.Errorf("%s imports %s; the gobble package may not (MADR 0179 D16)", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
