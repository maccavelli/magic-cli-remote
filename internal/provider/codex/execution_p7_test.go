package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func TestExecAndShellUseDistinctLabelsPoliciesAndAudit(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"command/exec":           {json.RawMessage(`{"exitCode":0,"stdout":"ok\n","stderr":""}`)},
		"command/exec/write":     {json.RawMessage(`{}`)},
		"command/exec/resize":    {json.RawMessage(`{}`)},
		"command/exec/terminate": {json.RawMessage(`{}`)},
		"thread/shellCommand":    {json.RawMessage(`{}`)},
	}}
	registry := newTerminalRegistry(maxTerminalReplayBytes)
	api := newExecutionAPI(stub.send, func(CapabilityID) bool { return true }, registry, 7, nil)
	result, err := api.ExecSandboxed(context.Background(), provider.ExecRequest{
		Argv: []string{"go", "test", "./..."}, CWD: "/repo", PermissionProfileID: ":workspace",
		ProcessID: "exec-1", Stream: true, TTY: true, Rows: 24, Cols: 80, OutputBytesCap: 4096, Timeout: time.Second,
	})
	if err != nil || result.ExitCode != 0 || result.Stdout != "ok\n" || result.Label != provider.ExecutionLabelSandboxed || result.AuditClass != provider.ExecutionAuditCommandExec {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := api.WriteExec(context.Background(), "exec-1", []byte("input"), false); err != nil {
		t.Fatal(err)
	}
	if err := api.ResizeExec(context.Background(), "exec-1", 30, 100); err != nil {
		t.Fatal(err)
	}
	if err := api.TerminateExec(context.Background(), "exec-1"); err != nil {
		t.Fatal(err)
	}
	shell, err := api.RunThreadShell(context.Background(), "thread-1", "printf hi")
	if err != nil || shell.Label != provider.ExecutionLabelUnsandboxed || shell.AuditClass != provider.ExecutionAuditThreadShell {
		t.Fatalf("shell=%+v err=%v", shell, err)
	}
	if got := stub.requests[0]; got.method != "command/exec" || !reflect.DeepEqual(got.params["command"], []string{"go", "test", "./..."}) || got.params["permissionProfile"] != ":workspace" || got.params["tty"] != true || got.params["outputBytesCap"] != uint64(4096) || got.params["timeoutMs"] != int64(1000) {
		t.Fatalf("exec request=%#v", got)
	}
	if got := stub.requests[1]; got.method != "command/exec/write" || got.params["deltaBase64"] != base64.StdEncoding.EncodeToString([]byte("input")) {
		t.Fatalf("write request=%#v", got)
	}
	if got := stub.requests[4]; got.method != "thread/shellCommand" || got.params["threadId"] != "thread-1" || got.params["command"] != "printf hi" {
		t.Fatalf("shell request=%#v", got)
	}
}

func TestExecValidationBlankControlCWDTimeoutAndUnknownOutcome(t *testing.T) {
	api := newExecutionAPI((&p6RPCStub{}).send, func(CapabilityID) bool { return true }, newTerminalRegistry(maxTerminalReplayBytes), 1, nil)
	for _, request := range []provider.ExecRequest{
		{},
		{Argv: []string{""}, CWD: "/repo"},
		{Argv: []string{"echo", "bad\x00arg"}, CWD: "/repo"},
		{Argv: []string{"echo"}, CWD: "relative"},
		{Argv: []string{"echo"}, CWD: "/repo", Timeout: -time.Second},
		{Argv: []string{"echo"}, CWD: "/repo", Rows: 24, Cols: 80},
	} {
		if _, err := api.ExecSandboxed(context.Background(), request); err == nil {
			t.Fatalf("accepted invalid request: %+v", request)
		}
	}
	unknown := errors.New("connection lost after dispatch")
	stub := &p6RPCStub{errors: map[string][]error{"thread/shellCommand": {unknown}}}
	api = newExecutionAPI(stub.send, func(CapabilityID) bool { return true }, newTerminalRegistry(maxTerminalReplayBytes), 1, nil)
	if _, err := api.RunThreadShell(context.Background(), "thread", "echo maybe"); !errors.Is(err, provider.ErrExecutionOutcomeUnknown) {
		t.Fatalf("shell ambiguity=%v", err)
	}
}

func TestTerminalRegistrySequenceReplayDetachAndGenerationCleanup(t *testing.T) {
	registry := newTerminalRegistry(12)
	key := terminalKey{Generation: 3, ThreadID: "thread-1", ID: "term-1"}
	if err := registry.Register(key, provider.TerminalInfo{ID: "term-1", ThreadID: "thread-1", Kind: provider.TerminalKindExec, Label: provider.ExecutionLabelSandboxed}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(key, provider.TerminalInfo{}); err == nil {
		t.Fatal("duplicate terminal key accepted")
	}
	first, err := registry.Append(key, "stdout", []byte("12345678"), false)
	if err != nil || first.Sequence != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := registry.Append(key, "stderr", []byte("abcdefgh"), true)
	if err != nil || second.Sequence != 2 || !second.CapReached {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	replay, gap, err := registry.Replay(key, 0)
	if err != nil || !gap || len(replay) != 1 || replay[0].Sequence != 2 {
		t.Fatalf("replay=%+v gap=%v err=%v", replay, gap, err)
	}
	registry.DetachDevice("phone-1")
	registry.DetachSession("thread-1")
	if got := registry.List("thread-1"); len(got) != 1 {
		t.Fatalf("detach killed terminal: %+v", got)
	}
	registry.CleanupGeneration(3)
	if got := registry.List("thread-1"); len(got) != 0 {
		t.Fatalf("generation cleanup left terminal: %+v", got)
	}
}

func TestTerminalNativeListTerminateAndFallback(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"thread/backgroundTerminals/list":      {json.RawMessage(`{"data":[{"processId":"p1","itemId":"i1","command":"sleep 5","cwd":"/repo","osPid":12}],"nextCursor":"more"}`)},
		"thread/backgroundTerminals/terminate": {json.RawMessage(`{"terminated":true}`)},
		"thread/backgroundTerminals/clean":     {json.RawMessage(`{}`)},
	}}
	api := newExecutionAPI(stub.send, func(CapabilityID) bool { return true }, newTerminalRegistry(maxTerminalReplayBytes), 1, nil)
	page, err := api.ListBackgroundTerminals(context.Background(), "thread-1", "cursor", 500)
	if err != nil || len(page.Terminals) != 1 || page.NextCursor != "more" || page.Limit != maxTerminalPage {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if terminated, err := api.TerminateBackgroundTerminal(context.Background(), "thread-1", "p1"); err != nil || !terminated {
		t.Fatalf("terminated=%v err=%v", terminated, err)
	}
	if err := api.CleanBackgroundTerminals(context.Background(), "thread-1"); err != nil {
		t.Fatal(err)
	}
	api = newExecutionAPI(stub.send, func(CapabilityID) bool { return false }, newTerminalRegistry(maxTerminalReplayBytes), 1, nil)
	if _, err := api.ListBackgroundTerminals(context.Background(), "thread-1", "", 50); !errors.Is(err, provider.ErrNativeUnavailable) {
		t.Fatalf("fallback error=%v", err)
	}
}

func TestEnvironmentAddStatusInfoAndSelection(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"environment/add":    {json.RawMessage(`{}`)},
		"environment/status": {json.RawMessage(`{"status":"ready"}`)},
		"environment/info":   {json.RawMessage(`{"shell":{"name":"bash","path":"/bin/bash"},"cwd":"file:///workspace"}`)},
	}}
	environment := provider.ExecutionEnvironment{ID: "loop", ExecServerURL: "ws://127.0.0.1:9000", ConnectTimeout: 5 * time.Second, RuntimeWorkspaceRoots: []string{"/workspace"}}
	api := newExecutionAPI(stub.send, func(CapabilityID) bool { return true }, newTerminalRegistry(maxTerminalReplayBytes), 1, []provider.ExecutionEnvironment{environment})
	if err := api.RegisterEnvironments(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := api.EnvironmentStatus(context.Background(), "loop")
	if err != nil || status.Status != "ready" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	info, err := api.EnvironmentInfo(context.Background(), "loop")
	if err != nil || info.ShellName != "bash" || info.ShellPath != "/bin/bash" || info.CWD != "file:///workspace" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	selection, err := api.ValidateEnvironmentSelection("loop", "/workspace/repo", []string{"/workspace/repo"})
	if err != nil || selection.EnvironmentID != "loop" || len(selection.RuntimeWorkspaceRoots) != 1 {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	if _, err := api.ValidateEnvironmentSelection("loop", "/outside", []string{"/outside"}); err == nil {
		t.Fatal("selection outside configured roots accepted")
	}
	joined := ""
	for _, request := range stub.requests {
		joined += request.method + "\n"
	}
	if strings.Count(joined, "environment/add") != 1 || stub.requests[0].params["connectTimeoutMs"] != uint64(5000) {
		t.Fatalf("requests=%+v", stub.requests)
	}
}

func TestProcessSpawnControlOutputExitAndGenerationOwnership(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"process/spawn":      {json.RawMessage(`{}`)},
		"process/writeStdin": {json.RawMessage(`{}`)},
		"process/resizePty":  {json.RawMessage(`{}`)},
		"process/kill":       {json.RawMessage(`{}`)},
	}}
	registry := newTerminalRegistry(maxTerminalReplayBytes)
	api := newExecutionAPI(stub.send, func(CapabilityID) bool { return true }, registry, 9, nil)
	api.standaloneEnabled = true
	api.envAllowlist = map[string]struct{}{"TERM": {}}
	process, err := api.SpawnProcess(context.Background(), provider.ProcessSpawnRequest{
		Argv: []string{"bash", "-lc", "printf hi"}, CWD: "/repo", Env: map[string]*string{"TERM": ptr("xterm")},
		TTY: true, Rows: 24, Cols: 80, OutputBytesCap: 1024, Timeout: time.Second,
	})
	if err != nil || process.ID == "" || process.Generation != 9 || process.Label != provider.ExecutionLabelStandalone {
		t.Fatalf("process=%+v err=%v", process, err)
	}
	if err := api.WriteProcess(context.Background(), process.ID, []byte("x"), false); err != nil {
		t.Fatal(err)
	}
	if err := api.ResizeProcess(context.Background(), process.ID, 30, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := api.HandleProcessOutput(process.ID, "stdout", base64.StdEncoding.EncodeToString([]byte("hi")), true); err != nil {
		t.Fatal(err)
	}
	if err := api.HandleProcessExit(process.ID, 0, "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if err := api.KillProcess(context.Background(), process.ID); !errors.Is(err, provider.ErrTerminalNotFound) {
		t.Fatalf("stale handle error=%v", err)
	}
	if _, err := api.SpawnProcess(context.Background(), provider.ProcessSpawnRequest{Argv: []string{"env"}, CWD: "/repo", Env: map[string]*string{"TOKEN": ptr("secret")}}); err == nil {
		t.Fatal("non-allowlisted secret environment name accepted")
	}
	api.generation = 10
	if err := api.WriteProcess(context.Background(), process.ID, nil, true); !errors.Is(err, provider.ErrTerminalNotFound) {
		t.Fatalf("foreign generation error=%v", err)
	}
}

func ptr(value string) *string { return &value }

// TestTerminalOutputPushesLiveAndStaysReplayable proves the live push and the
// bounded replay buffer are the same sequence. A client that misses a push
// must be able to recover the exact chunk it missed, and a push failure must
// never cost the buffer its copy.
func TestTerminalOutputPushesLiveAndStaysReplayable(t *testing.T) {
	registry := newTerminalRegistry(maxTerminalReplayBytes)
	api := newExecutionAPI((&p6RPCStub{}).send, func(CapabilityID) bool { return true }, registry, 4, nil)
	var pushed []provider.TerminalOutput
	var pushedThreads []string
	api.push = func(threadID string, chunk provider.TerminalOutput) {
		pushedThreads = append(pushedThreads, threadID)
		pushed = append(pushed, chunk)
	}
	key := terminalKey{Generation: 4, ThreadID: "thread-1", ID: "term-1"}
	if err := registry.Register(key, provider.TerminalInfo{Kind: provider.TerminalKindExec}); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"one", "two"} {
		if _, err := api.appendAndPush(key, "stdout", []byte(line), false); err != nil {
			t.Fatal(err)
		}
	}
	if len(pushed) != 2 || pushed[0].Sequence != 1 || pushed[1].Sequence != 2 {
		t.Fatalf("pushed=%+v", pushed)
	}
	if pushedThreads[0] != "thread-1" || pushedThreads[1] != "thread-1" {
		t.Fatalf("pushed threads=%v", pushedThreads)
	}
	replay, gap, err := registry.Replay(key, 1)
	if err != nil || gap || len(replay) != 1 || replay[0].Sequence != 2 || string(replay[0].Data) != "two" {
		t.Fatalf("replay=%+v gap=%v err=%v", replay, gap, err)
	}
	// An unregistered terminal is a buffering failure, not a silent push.
	before := len(pushed)
	if _, err := api.appendAndPush(terminalKey{Generation: 4, ID: "ghost"}, "stdout", []byte("x"), false); !errors.Is(err, provider.ErrTerminalNotFound) {
		t.Fatalf("ghost terminal error=%v", err)
	}
	if len(pushed) != before {
		t.Fatalf("pushed a chunk that was never buffered: %+v", pushed)
	}
}

// TestStandaloneProcessIsListedUnderItsThread proves a spawned process is
// reachable through the session's /ps and /stop. A process filed under no
// thread would be invisible and unstoppable from the phone while still
// holding an unsandboxed host process open.
func TestStandaloneProcessIsListedUnderItsThread(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	registry := newTerminalRegistry(maxTerminalReplayBytes)
	api := newExecutionAPI((&p6RPCStub{}).send, func(CapabilityID) bool { return true }, registry, 5, nil)
	api.standaloneEnabled = true
	process, err := api.SpawnProcess(context.Background(), provider.ProcessSpawnRequest{
		Argv: []string{"sleep", "60"}, ThreadID: "thread-9", CWD: "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	listed := registry.List("thread-9")
	if len(listed) != 1 || listed[0].ID != process.ID || listed[0].Kind != provider.TerminalKindProcess ||
		listed[0].Label != provider.ExecutionLabelStandalone || !listed[0].Running {
		t.Fatalf("thread terminals=%+v", listed)
	}
	if other := registry.List("thread-other"); len(other) != 0 {
		t.Fatalf("process leaked into another thread: %+v", other)
	}
}
