package opencode

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func dataURL(mime string, n int) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(make([]byte, n))
}

// TestArtifactURLSchemes pins exactly which schemes may be forwarded.
//
// The daemon never fetches these, so the risk is what a client is invited to
// open: http is downgradeable, file names the daemon host's own filesystem, and
// userinfo smuggles credentials into a shareable link (PLAN P5 step 2).
func TestArtifactURLSchemes(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		ok       bool
	}{
		{"https", "https://example.com/a.png", true},
		{"https uppercase scheme", "HTTPS://example.com/a.png", true},
		{"https with query", "https://example.com/a.png?v=2", true},
		{"http is refused", "http://example.com/a.png", false},
		{"file is refused", "file:///etc/passwd", false},
		{"ftp is refused", "ftp://example.com/a.png", false},
		{"javascript is refused", "javascript:alert(1)", false},
		{"scheme-relative is refused", "//example.com/a.png", false},
		{"userinfo is refused", "https://user:pass@example.com/a.png", false},
		{"bare userinfo is refused", "https://user@example.com/a.png", false},
		{"no host is refused", "https:///a.png", false},
		{"whitespace is refused", "https://example.com/a b.png", false},
		{"newline is refused", "https://example.com/a\n.png", false},
		{"NUL is refused", "https://example.com/a\x00.png", false},
		{"over the byte cap is refused", "https://example.com/" + strings.Repeat("a", maxArtifactURLBytes), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validArtifactURL(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("validArtifactURL(%q) err=%v, want ok=%v", tc.in, err, tc.ok)
			}
		})
	}
}

// TestArtifactInlineBoundary pins acceptance either side of the inline budget.
func TestArtifactInlineBoundary(t *testing.T) {
	atLimit := dataURL("image/png", event.MaxArtifactInlineBytes)
	got, n, err := decodeArtifactDataURL(atLimit)
	if err != nil {
		t.Fatalf("a payload of exactly the budget was rejected: %v", err)
	}
	if n != event.MaxArtifactInlineBytes || got == "" {
		t.Fatalf("bytes = %d", n)
	}
	if _, _, err := decodeArtifactDataURL(dataURL("image/png", event.MaxArtifactInlineBytes+1)); err == nil {
		t.Fatal("a payload one byte over the budget was accepted")
	}
}

// TestArtifactDataURLValidation covers the malformed shapes.
func TestArtifactDataURLValidation(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		ok       bool
	}{
		{"canonical", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}), true},
		{"no comma", "data:image/png;base64", false},
		{"not base64", "data:image/png,raw%20text", false},
		{"empty payload", "data:image/png;base64,", false},
		{"non-canonical base64", "data:image/png;base64,aGVsbG8", false},
		{"url-safe alphabet", "data:image/png;base64,a-b_", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodeArtifactDataURL(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("decodeArtifactDataURL(%q) err=%v, want ok=%v", tc.in, err, tc.ok)
			}
		})
	}
}

// TestArtifactFromReportsMetadataWhenContentIsUnusable proves an artifact the
// daemon cannot carry still reports itself, marked truncated — silently
// dropping a file the agent produced is the worse failure.
func TestArtifactFromReportsMetadataWhenContentIsUnusable(t *testing.T) {
	for _, url := range []string{
		"", "http://example.com/a.png", "file:///tmp/a.png",
		dataURL("image/png", event.MaxArtifactInlineBytes+1),
		"data:image/png;base64,!!!",
	} {
		a := artifactFrom(nativePart{Type: partFile, Filename: "a.png", Mime: "image/png", URL: url})
		if a == nil {
			t.Fatalf("url %q produced no artifact at all", url)
		}
		if !a.Truncated {
			t.Fatalf("url %q was not marked truncated: %+v", url, a)
		}
		if a.URL != "" || a.Data != "" {
			t.Fatalf("url %q leaked content: %+v", url, a)
		}
		if a.Filename != "a.png" || a.MIME != "image/png" {
			t.Fatalf("metadata lost for %q: %+v", url, a)
		}
	}
}

// TestArtifactCarriesExactlyOneContentField proves url and data never coexist.
func TestArtifactCarriesExactlyOneContentField(t *testing.T) {
	withURL := artifactFrom(nativePart{Type: partFile, URL: "https://example.com/a.png"})
	if withURL.URL == "" || withURL.Data != "" || withURL.Truncated {
		t.Fatalf("https artifact = %+v", withURL)
	}
	withData := artifactFrom(nativePart{Type: partFile, URL: dataURL("image/png", 16)})
	if withData.Data == "" || withData.URL != "" || withData.Truncated {
		t.Fatalf("data artifact = %+v", withData)
	}
	if withData.Bytes != 16 {
		t.Fatalf("bytes = %d, want 16", withData.Bytes)
	}
}

// TestArtifactMetadataIsBounded proves display metadata cannot grow unbounded.
func TestArtifactMetadataIsBounded(t *testing.T) {
	a := artifactFrom(nativePart{
		Type:     partFile,
		Filename: strings.Repeat("n", maxArtifactFilenameBytes*2),
		Mime:     strings.Repeat("m", maxArtifactMIMEBytes*2),
		URL:      "https://example.com/a.png",
	})
	if len(a.Filename) > maxArtifactFilenameBytes {
		t.Fatalf("filename = %d bytes", len(a.Filename))
	}
	if len(a.MIME) > maxArtifactMIMEBytes {
		t.Fatalf("mime = %d bytes", len(a.MIME))
	}
}

// TestFailedToolStateProducesNoArtifact is the fixture-pinned rule: on 1.18.21
// only ToolStateCompleted has an attachment field.
func TestFailedToolStateProducesNoArtifact(t *testing.T) {
	var failed nativePart
	if err := json.Unmarshal([]byte(`{
		"id":"prt_1","messageID":"msg_a","type":"tool","tool":"write",
		"state":{"status":"error","error":"boom","input":{},"metadata":{},"time":{}}}`), &failed); err != nil {
		t.Fatal(err)
	}
	if got := toolArtifacts(failed, true); len(got) != 0 {
		t.Fatalf("a failed tool state produced %d artifact(s): %+v", len(got), got)
	}
	// And the mapped row is a failure, not a completion.
	ev, ok := mapPart("assistant", failed, true, nil)
	if !ok || ev.Status == "completed" {
		t.Fatalf("failed tool mapped as %+v", ev)
	}
}

// TestToolAttachmentIdentityIsDeterministic proves the same tool part yields
// the same artifact identities live and on replay, which is what lets resume
// deduplicate them.
func TestToolAttachmentIdentityIsDeterministic(t *testing.T) {
	var completed nativePart
	if err := json.Unmarshal([]byte(`{
		"id":"prt_1","messageID":"msg_a","type":"tool","tool":"write",
		"state":{"status":"completed","attachments":[
			{"type":"file","filename":"a.png","mime":"image/png","url":"https://example.com/a.png"},
			{"type":"file","filename":"b.png","mime":"image/png","url":"https://example.com/b.png"}
		]}}`), &completed); err != nil {
		t.Fatal(err)
	}
	live := toolArtifacts(completed, true)
	replay := toolArtifacts(completed, true)
	if len(live) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(live))
	}
	for i := range live {
		if live[i].NativePartID != replay[i].NativePartID {
			t.Fatalf("identity differs between live and replay: %q vs %q",
				live[i].NativePartID, replay[i].NativePartID)
		}
		want := "prt_1#" + string(rune('0'+i))
		if live[i].NativePartID != want {
			t.Fatalf("part id = %q, want %q", live[i].NativePartID, want)
		}
		if live[i].NativeMessageID != "msg_a" {
			t.Fatalf("message id = %q", live[i].NativeMessageID)
		}
		if !live[i].Replace {
			t.Fatal("a tool attachment was not marked authoritative")
		}
	}
}

// TestToolAttachmentsIgnoreNonFileEntries proves only file entries become
// artifacts.
func TestToolAttachmentsIgnoreNonFileEntries(t *testing.T) {
	var p nativePart
	if err := json.Unmarshal([]byte(`{
		"id":"prt_1","messageID":"msg_a","type":"tool",
		"state":{"status":"completed","attachments":[
			{"type":"text","text":"not a file"},
			{"type":"file","filename":"a.png","mime":"image/png","url":"https://example.com/a.png"}
		]}}`), &p); err != nil {
		t.Fatal(err)
	}
	got := toolArtifacts(p, true)
	if len(got) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(got))
	}
	if got[0].Artifact.Filename != "a.png" {
		t.Fatalf("wrong attachment mapped: %+v", got[0].Artifact)
	}
}

// TestUserFilePartIsNotAnArtifact proves a user's own attachment echoed back
// does not become a second row — it already rendered on the user bubble.
func TestUserFilePartIsNotAnArtifact(t *testing.T) {
	p := nativePart{ID: "p1", MessageID: "m1", Type: partFile,
		Filename: "sent.png", Mime: "image/png", URL: "https://example.com/sent.png"}
	if _, ok := mapPart("user", p, true, nil); ok {
		t.Fatal("a user file part produced an artifact row")
	}
	if _, ok := mapPart("assistant", p, true, nil); !ok {
		t.Fatal("an assistant file part produced no artifact row")
	}
}

// TestAssistantFilePartCarriesIdentity proves an artifact is addressable like
// any other transcript row.
func TestAssistantFilePartCarriesIdentity(t *testing.T) {
	ev, ok := mapPart("assistant", nativePart{
		ID: "prt_9", MessageID: "msg_z", Type: partFile,
		Filename: "out.png", Mime: "image/png", URL: dataURL("image/png", 8),
	}, true, nil)
	if !ok || ev.Type != event.TypeArtifact {
		t.Fatalf("event = %+v", ev)
	}
	if ev.NativeMessageID != "msg_z" || ev.NativePartID != "prt_9" || !ev.Replace {
		t.Fatalf("identity = %+v", ev)
	}
	if ev.Artifact == nil || ev.Artifact.Bytes != 8 {
		t.Fatalf("artifact = %+v", ev.Artifact)
	}
}
