package acpagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// fsSession is a session with what ReadTextFile needs: no roots, so every
// path is allowed, and a sink for its audit events.
func fsSession() *session {
	return &session{localID: "l", agentID: "a", events: make(chan event.Event, 16), log: slog.New(slog.DiscardHandler)}
}

// TestReadTextFileMissingIsResourceNotFound is MADR 0207 amendment H, D26: a
// file that does not exist is answered -32002, naming it, not -32603.
func TestReadTextFileMissingIsResourceNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")
	_, err := fsSession().ReadTextFile(context.Background(), acp.ReadTextFileRequest{Path: path})
	if !acp.IsResourceNotFound(err) {
		t.Fatalf("err = %v, want resource-not-found (-32002)", err)
	}
	var re *acp.RequestError
	if !errors.As(err, &re) || re.Data == nil {
		t.Fatalf("err = %#v, want a *acp.RequestError naming the file", err)
	}
	if b, _ := json.Marshal(re.Data); !strings.Contains(string(b), filepath.ToSlash(path)) && !strings.Contains(string(b), path) {
		t.Errorf("data = %s, want it to name %s", b, path)
	}
}

// TestReadTextFilePermissionKeepsItsMessage: an unreadable file still says the
// daemon lacks the OS permission (MADR 0069 D4), and is not resource-not-found.
func TestReadTextFilePermissionKeepsItsMessage(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a 0000 file is readable here")
	}
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, err := fsSession().ReadTextFile(context.Background(), acp.ReadTextFileRequest{Path: path})
	if err == nil || !strings.Contains(err.Error(), "lacks OS permission") || acp.IsResourceNotFound(err) {
		t.Fatalf("err = %v, want the permission message", err)
	}
}

// TestCloseCaptureNamesDroppedBytes is D26: a wire capture that lost part of
// the agent's output says how much when it closes, and a whole one says
// nothing.
func TestCloseCaptureNamesDroppedBytes(t *testing.T) {
	h := &recordingHandler{}
	closeCapture(slog.New(h), nil, 0)
	if len(h.records) != 0 {
		t.Fatalf("a whole capture logged %d record(s)", len(h.records))
	}
	closeCapture(slog.New(h), nil, 4096)
	if len(h.records) != 1 {
		t.Fatalf("records = %d, want 1", len(h.records))
	}
	var dropped uint64
	h.records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "dropped_bytes" && a.Value.Kind() == slog.KindUint64 {
			dropped = a.Value.Uint64()
		}
		return true
	})
	if h.records[0].Level != slog.LevelWarn || dropped != 4096 {
		t.Fatalf("record = %q at %v, dropped_bytes %d; want a warning with 4096", h.records[0].Message, h.records[0].Level, dropped)
	}
}

// TestInitializeNamesAProtocolVersionMismatch is D26: an agent that answers
// initialize with another protocol version is reported as such, naming both
// versions and what to do, through a real client connection.
func TestInitializeNamesAProtocolVersionMismatch(t *testing.T) {
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	t.Cleanup(func() {
		_ = agentWrites.Close()
		_ = clientWrites.Close()
	})
	go func() {
		sc := bufio.NewScanner(agentReads)
		for sc.Scan() {
			var m wireMsg
			if json.Unmarshal(sc.Bytes(), &m) == nil && m.Method == "initialize" {
				_, _ = io.WriteString(agentWrites, `{"jsonrpc":"2.0","id":`+string(m.ID)+
					`,"result":{"protocolVersion":2,"agentCapabilities":{}}}`+"\n")
			}
		}
	}()
	conn := acp.NewClientSideConnection(fsSession(), clientWrites, clientReads)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := conn.Initialize(ctx, (&Provider{}).initializeRequest())
	if err == nil {
		t.Fatal("initialize accepted protocol version 2")
	}
	msg := initializeError(err).Error()
	for _, want := range []string{"protocol version 2", "speaks 1", "update"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	var pv *acp.ProtocolVersionError
	if !errors.As(initializeError(err), &pv) {
		t.Errorf("the message lost the *acp.ProtocolVersionError: %v", err)
	}
}

// TestSubagentUpdatesStopAtTheSDK is D26 (H5): Magic does not advertise
// subagents, so the SDK refuses their updates before Magic's handler, which
// therefore needs no branches for them.
func TestSubagentUpdatesStopAtTheSDK(t *testing.T) {
	s, a := newWireSession(t, "{}")
	h := &recordingHandler{}
	s.log = slog.New(h)
	before := s.conn.Connection().RefusedMessages()
	for _, u := range []string{
		`{"sessionUpdate":"subagent_update","sessionId":"child-1"}`,
		`{"sessionUpdate":"session_message","messageId":"m1"}`,
		`{"sessionUpdate":"session_message_chunk","messageId":"m1","content":{"type":"text","text":"x"}}`,
		// A barrier: once this mode update has reached Magic, the three before it
		// have been read.
		`{"sessionUpdate":"current_mode_update","currentModeId":"plan"}`,
	} {
		a.sendRaw(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":` + u + `}}`)
	}
	deadline := time.After(5 * time.Second)
	for seen := false; !seen; {
		select {
		case ev := <-s.events:
			seen = ev.Type == event.TypeMode
		case <-deadline:
			t.Fatal("the barrier update never reached Magic")
		}
	}
	if variants, _ := h.declined(); len(variants) != 0 {
		t.Errorf("Magic's handler saw and declined %v; the SDK should have refused them", variants)
	}
	if got := s.conn.Connection().RefusedMessages() - before; got != 3 {
		t.Errorf("the SDK refused %d message(s), want the 3 subagent updates", got)
	}
}
