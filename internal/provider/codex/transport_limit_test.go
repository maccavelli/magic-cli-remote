package codex

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// MADR 0223: a Codex transport carries a message of up to 10 MiB, acp-go-sdk2's
// bound, and the JSONL reader holds only what a line needs.

const twoMiB = 2 << 20

func TestJSONLTransportReadsTwoMiB(t *testing.T) {
	line := bytes.Repeat([]byte("x"), twoMiB)
	tr := newJSONLTransport(io.Discard, bytes.NewReader(append(append([]byte(nil), line...), '\n')))
	got, err := tr.Read(context.Background())
	if err != nil {
		t.Fatalf("2 MiB line: %v", err)
	}
	if !bytes.Equal(got, line) {
		t.Fatalf("2 MiB line read as %d bytes, want %d intact", len(got), len(line))
	}
}

// TestJSONLTransportRefusesOverLimitAndContinues: an over-limit line is an
// error, and the next Read starts on the next line. The far case passes the
// bound a read buffer or more before its newline, so the rest of the line must
// be drained; the boundary case is one byte over.
func TestJSONLTransportRefusesOverLimitAndContinues(t *testing.T) {
	for name, n := range map[string]int{
		"one byte over": maxTransportFrameBytes + 1,
		"256 KiB over":  maxTransportFrameBytes + 256<<10,
	} {
		t.Run(name, func(t *testing.T) {
			var in bytes.Buffer
			in.Write(bytes.Repeat([]byte("x"), n))
			in.WriteString("\n{\"next\":true}\r\n")
			tr := newJSONLTransport(io.Discard, &in)
			if _, err := tr.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "frame exceeds") {
				t.Fatalf("over-limit line: err = %v, want \"frame exceeds\"", err)
			}
			got, err := tr.Read(context.Background())
			if err != nil {
				t.Fatalf("line after the over-limit one: %v", err)
			}
			if string(got) != `{"next":true}` {
				t.Fatalf("line after the over-limit one = %.40q, want the next line, its CRLF trimmed", got)
			}
		})
	}
}

func TestJSONLTransportReaderIs128KiB(t *testing.T) {
	tr := newJSONLTransport(io.Discard, strings.NewReader(""))
	jt, ok := tr.(*jsonlTransport)
	if !ok {
		t.Fatalf("newJSONLTransport returned %T", tr)
	}
	if got, want := jt.reader.Size(), 128<<10; got != want {
		t.Fatalf("JSONL read buffer is %d bytes, want %d: the buffer is the SDK's, not the limit", got, want)
	}
}

func TestJSONLTransportSendsTwoMiB(t *testing.T) {
	var out bytes.Buffer
	tr := newJSONLTransport(&out, strings.NewReader(""))
	if err := tr.Send(context.Background(), bytes.Repeat([]byte("x"), twoMiB)); err != nil {
		t.Fatalf("2 MiB frame: %v", err)
	}
	if out.Len() != twoMiB+1 {
		t.Fatalf("wrote %d bytes, want %d and a newline", out.Len(), twoMiB)
	}
}

func TestWebSocketTransportReadsTwoMiB(t *testing.T) {
	msg := bytes.Repeat([]byte("x"), twoMiB)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if err := conn.Write(r.Context(), websocket.MessageText, msg); err != nil {
			return
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tr, err := dialWebSocketTransport(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	got, err := tr.Read(ctx)
	if err != nil {
		t.Fatalf("2 MiB message: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("2 MiB message read as %d bytes, want %d intact", len(got), len(msg))
	}
}
