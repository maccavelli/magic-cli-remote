package acpagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/stderrtail"
)

// The helper agent's stderr and exit, for the "die" and "serve-die" modes
// (MADR 0216 D5). testStderrEnv is "lines:N" (N numbered lines), "long:N" (one
// line of N bytes), "panic" (a real Go panic, eight frames deep, which exits 2)
// or "raw:a|b" (the lines a and b). testExitEnv is the exit status.
// testDieWhenEnv names the file whose creation makes a "serve-die" agent die,
// so a test decides when, after initialize has been answered. With
// testStderrFirstEnv "1", a "serve-die" agent also writes its stderr as it
// starts, so a live agent has a tail.
const (
	testStderrEnv      = "MCREMOTE_TEST_ACP_STDERR"
	testExitEnv        = "MCREMOTE_TEST_ACP_EXIT"
	testDieWhenEnv     = "MCREMOTE_TEST_ACP_DIE_WHEN"
	testStderrFirstEnv = "MCREMOTE_TEST_ACP_STDERR_FIRST"
)

// writeStderrForTest writes what testStderrEnv asks for and returns the exit
// status testExitEnv names.
func writeStderrForTest() int {
	kind, arg, _ := strings.Cut(os.Getenv(testStderrEnv), ":")
	switch kind {
	case "lines":
		n, _ := strconv.Atoi(arg)
		for i := range n {
			fmt.Fprintf(os.Stderr, "line %06d of the agent's stderr\n", i)
		}
	case "long":
		n, _ := strconv.Atoi(arg)
		fmt.Fprintln(os.Stderr, strings.Repeat("x", n))
	case "panic":
		panicAtDepth(8)
	case "raw":
		fmt.Fprintln(os.Stderr, strings.ReplaceAll(arg, "|", "\n"))
	}
	code, _ := strconv.Atoi(os.Getenv(testExitEnv))
	return code
}

// panicAtDepth panics n calls deep, so the runtime's report has n frames
// below its "panic:" line, as an agent written in Go reports (MADR 0216 A-5).
func panicAtDepth(n int) {
	if n == 0 {
		panic("testagent: the session store is corrupt")
	}
	panicAtDepth(n - 1)
}

// serveThenDieTestAgent answers initialize, then dies once testDieWhenEnv's
// file exists. Bounded by a minute, like the "serve" agent.
func serveThenDieTestAgent() {
	if os.Getenv(testStderrFirstEnv) == "1" {
		_ = writeStderrForTest()
	}
	trigger := os.Getenv(testDieWhenEnv)
	go func() {
		for {
			if _, err := os.Stat(trigger); err == nil {
				os.Exit(writeStderrForTest())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	conn := acp.NewAgentSideConnection(testAgent{}, os.Stdout, os.Stdin)
	<-conn.Done()
	time.Sleep(time.Minute)
}

// dyingAgent starts p's agent as a "serve-die" helper with the given stderr
// and exit status, and returns its session and the function that makes it die.
func dyingAgent(t *testing.T, p *Provider, stderr string, code int) (*session, func()) {
	t.Helper()
	trigger := filepath.Join(t.TempDir(), "die")
	t.Setenv(testAgentEnv, "serve-die")
	t.Setenv(testStderrEnv, stderr)
	t.Setenv(testExitEnv, strconv.Itoa(code))
	t.Setenv(testDieWhenEnv, trigger)
	s, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeAndWait(t, s) })
	return s, func() {
		if err := os.WriteFile(trigger, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// deathMessage returns the error the session reports for its agent's death.
func deathMessage(t *testing.T, s *session) string {
	t.Helper()
	deadline := time.After(wireBudget)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeError {
				return ev.Error
			}
		case <-deadline:
			t.Fatal("no error event after the agent died")
		}
	}
}

// TestDeathMessageCarriesTheTail is MADR 0216 D2 and D24: the first line is
// today's, and the tail follows the separator, the last 64 lines.
func TestDeathMessageCarriesTheTail(t *testing.T) {
	p, _ := testAgentProvider(t)
	s, die := dyingAgent(t, p, "lines:70", 3)
	die()
	msg := deathMessage(t, s)
	first, _, _ := strings.Cut(msg, "\n")
	if first != "testagent process exited: exit status 3" {
		t.Fatalf("first line = %q, want today's message", first)
	}
	if strings.Count(msg, stderrtail.Separator) != 1 {
		t.Fatalf("message has no tail after the separator:\n%s", msg)
	}
	if !strings.Contains(msg, "line 000069 ") || !strings.Contains(msg, "line 000006 ") || strings.Contains(msg, "line 000005 ") {
		t.Fatalf("want lines 6 to 69, the last 64:\n%s", msg)
	}
}

// TestDeathOfStatusZeroCarriesTheTail: an agent that exits cleanly mid-session
// still died, and says why (D2; the case JoinStderrTail leaves out).
func TestDeathOfStatusZeroCarriesTheTail(t *testing.T) {
	p, _ := testAgentProvider(t)
	s, die := dyingAgent(t, p, "lines:3", 0)
	die()
	msg := deathMessage(t, s)
	if !strings.HasPrefix(msg, "testagent process exited"+stderrtail.Separator) || !strings.Contains(msg, "line 000002 ") {
		t.Fatalf("message = %q, want the bare exit, then the tail", msg)
	}
}

// TestDeathWithNoStderrIsUnchanged: no stderr, today's message exactly.
func TestDeathWithNoStderrIsUnchanged(t *testing.T) {
	p, _ := testAgentProvider(t)
	s, die := dyingAgent(t, p, "", 3)
	die()
	if msg := deathMessage(t, s); msg != "testagent process exited: exit status 3" {
		t.Fatalf("message = %q, want today's", msg)
	}
}

// TestTailRemovesKnownSecrets is D4 and D27: a credential-shaped variable, a
// Spec-named one and an MCP header value are removed; a value under 8 bytes is
// not.
func TestTailRemovesKnownSecrets(t *testing.T) {
	p, _ := testAgentProvider(t)
	t.Setenv("MCREMOTE_TEST_API_KEY", "sk-test-0123456789")
	t.Setenv("MCREMOTE_ODD_CRED", "odd-credential-42")
	t.Setenv("MCREMOTE_SHORT_TOKEN", "abc1234")
	p.spec.CredentialEnv = []string{"MCREMOTE_ODD_CRED"}
	p.cfg.McpServers = []McpServer{{Name: "m", Transport: McpHTTP, URL: "http://127.0.0.1:1",
		Headers: map[string]string{"Authorization": "Bearer mcp-header-value"}}}
	s, die := dyingAgent(t, p,
		"raw:key sk-test-0123456789|cred odd-credential-42|header Bearer mcp-header-value|short abc1234", 1)
	die()
	msg := deathMessage(t, s)
	for _, secret := range []string{"sk-test-0123456789", "odd-credential-42", "mcp-header-value"} {
		if strings.Contains(msg, secret) {
			t.Errorf("the tail still holds %q", secret)
		}
	}
	if got := strings.Count(msg, stderrtail.Redacted); got != 3 {
		t.Errorf("%d redactions, want 3:\n%s", got, msg)
	}
	if !strings.Contains(msg, "short abc1234") {
		t.Errorf("a 7-byte value was removed:\n%s", msg)
	}
}

// TestDeathTailIsBounded is D1 and D24: one 64 KiB line keeps 4096 bytes.
func TestDeathTailIsBounded(t *testing.T) {
	p, _ := testAgentProvider(t)
	s, die := dyingAgent(t, p, "long:65536", 3)
	die()
	msg := deathMessage(t, s)
	limit := len("testagent process exited: exit status 3") + len(stderrtail.Separator) + stderrtail.DefaultBytes
	if len(msg) > limit || !strings.Contains(msg, stderrtail.Separator+"xxxx") {
		t.Fatalf("message is %d bytes, want a tail and at most %d", len(msg), limit)
	}
}

// TestGoPanicMessageSurvivesTheTail is D24, for gobble (D30): a Go panic's
// message is its first line, and survives its stack.
func TestGoPanicMessageSurvivesTheTail(t *testing.T) {
	p, _ := testAgentProvider(t)
	s, die := dyingAgent(t, p, "panic", 0)
	die()
	msg := deathMessage(t, s)
	if !strings.HasPrefix(msg, "testagent process exited: exit status 2") ||
		!strings.Contains(msg, "panic: testagent: the session store is corrupt") {
		t.Fatalf("the panic's message is not in the tail:\n%s", msg)
	}
}

// TestStoppedAgentLogsNoTail: an agent Magic stops did not die, so its tail is
// neither logged nor reported, however much it wrote. The test first sees the
// agent's line reach the debug log, so the tail it checks is not empty.
func TestStoppedAgentLogsNoTail(t *testing.T) {
	p, _ := testAgentProvider(t)
	h := &recordingHandler{}
	p.log = slog.New(h)
	t.Setenv(testStderrFirstEnv, "1")
	s, _ := dyingAgent(t, p, "raw:noise from a live agent", 0)
	logged := func(msg, line string) bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, r := range h.records {
			if r.Message != msg {
				continue
			}
			found := line == ""
			r.Attrs(func(a slog.Attr) bool {
				found = found || a.Value.String() == line
				return !found
			})
			if found {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(wireBudget); !logged("testagent-stderr", "noise from a live agent"); {
		if time.Now().After(deadline) {
			t.Fatal("the agent's stderr never reached the debug log")
		}
		time.Sleep(10 * time.Millisecond)
	}
	closeAndWait(t, s)
	if logged("engine stderr tail (phone-visible)", "") {
		t.Fatal("a stopped agent's tail was logged as a death")
	}
}

// TestStartFailureCarriesTheTail is D3: an agent that dies before it answers
// initialize says why, and the error carries the tail as a *stderrtail.Error.
func TestStartFailureCarriesTheTail(t *testing.T) {
	p, _ := testAgentProvider(t)
	t.Setenv(testAgentEnv, "die")
	t.Setenv(testStderrEnv, "raw:Error: not logged in|run 'agent login' first")
	t.Setenv(testExitEnv, "2")
	s, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err == nil {
		closeAndWait(t, s)
		t.Fatal("spawnAgent succeeded with an agent that exits at once")
	}
	var te *stderrtail.Error
	if !errors.As(err, &te) || te.Tail != "Error: not logged in\nrun 'agent login' first" {
		t.Fatalf("err = %v; want a *stderrtail.Error with the agent's two lines", err)
	}
	if !strings.HasPrefix(err.Error(), "acp initialize: ") {
		t.Fatalf("err = %q; want initialize's message first", err)
	}
}
