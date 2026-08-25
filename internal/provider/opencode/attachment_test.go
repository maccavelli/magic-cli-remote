package opencode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// b64 encodes n bytes of filler.
func b64(n int) string {
	return base64.StdEncoding.EncodeToString(make([]byte, n))
}

// multimodalSession builds a session whose active model accepts image+audio.
func multimodalSession(t *testing.T, h *recorder) *httpSession {
	t.Helper()
	s := newThinkingSession(t, h, "opencode/multi")
	s.d.surfaces.replace(map[string]modelSurface{
		"opencode/multi": {Attachment: true, Inputs: inputsImageAudio()},
	})
	return s
}

func inputsImageAudio() picker.ModelInputs {
	return picker.ModelInputs{Text: true, Image: true, Audio: true}
}

func TestValidAttachmentFilenameBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		ok   bool
	}{
		{"empty is allowed and omitted", "", true},
		{"plain basename", "photo.png", true},
		{"unicode basename", "фото.png", true},
		{"at the byte limit", strings.Repeat("a", maxAttachmentFilenameBytes), true},
		{"one byte over", strings.Repeat("a", maxAttachmentFilenameBytes+1), false},
		{"forward slash", "dir/photo.png", false},
		{"leading slash", "/etc/passwd", false},
		{"backslash", `dir\photo.png`, false},
		{"parent traversal", "..", false},
		{"self", ".", false},
		{"NUL", "photo\x00.png", false},
		{"newline", "photo\n.png", false},
		{"tab", "photo\t.png", false},
		{"DEL", "photo\x7f.png", false},
		{"invalid utf-8", "photo\xff.png", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validAttachmentFilename(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("validAttachmentFilename(%q) err=%v, want ok=%v", tc.in, err, tc.ok)
			}
		})
	}
}

func TestValidAttachmentMIMEBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, kind, mime string
		ok               bool
	}{
		{"image png", "image", "image/png", true},
		{"image jpeg uppercase family", "image", "IMAGE/PNG", true},
		{"audio wav", "audio", "audio/wav", true},
		{"kind/family mismatch", "image", "audio/wav", false},
		{"audio labelled image", "audio", "image/png", false},
		{"unknown kind", "video", "video/mp4", false},
		{"empty mime", "image", "", false},
		{"no subtype", "image", "image/", false},
		{"parameters are refused", "image", "image/png; charset=utf-8", false},
		{"comma", "image", "image/png,image/jpeg", false},
		{"embedded NUL", "image", "image/p\x00ng", false},
		{"over the byte limit", "image", "image/" + strings.Repeat("a", maxAttachmentMIMEBytes), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validAttachmentMIME(tc.kind, tc.mime)
			if tc.ok != (err == nil) {
				t.Fatalf("validAttachmentMIME(%q,%q) err=%v, want ok=%v", tc.kind, tc.mime, err, tc.ok)
			}
		})
	}
}

func TestDecodeAttachmentDataBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		ok       bool
	}{
		{"canonical", base64.StdEncoding.EncodeToString([]byte("hello")), true},
		{"single byte", base64.StdEncoding.EncodeToString([]byte{0}), true},
		{"empty string", "", false},
		{"empty payload decodes to zero bytes", "", false},
		{"not base64", "!!!!", false},
		{"url-safe alphabet is not canonical std", "a-b_", false},
		{"missing padding", "aGVsbG8", false},
		{"whitespace", "aGVs bG8=", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeAttachmentData(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("decodeAttachmentData(%q) err=%v, want ok=%v", tc.in, err, tc.ok)
			}
		})
	}
}

// TestFilePartInputShape pins the exact stable FilePartInput form.
func TestFilePartInputShape(t *testing.T) {
	raw := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	part, err := filePartInput(provider.Content{
		Type: "image", MimeType: "image/png", Filename: "shot.png",
		Data: base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		t.Fatal(err)
	}
	if part["type"] != "file" || part["mime"] != "image/png" || part["filename"] != "shot.png" {
		t.Fatalf("part shape = %+v", part)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	if part["url"] != want {
		t.Fatalf("url = %v, want %v", part["url"], want)
	}
	// No filename supplied → the key is omitted, never invented.
	bare, err := filePartInput(provider.Content{
		Type: "audio", MimeType: "audio/wav", Data: base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := bare["filename"]; present {
		t.Fatalf("filename invented: %+v", bare)
	}
}

// TestPromptPartsPreservesOrder proves composed order survives conversion.
func TestPromptPartsPreservesOrder(t *testing.T) {
	s := multimodalSession(t, newRecorder())
	got, err := s.promptParts([]provider.Content{
		{Type: "text", Text: "before"},
		{Type: "image", MimeType: "image/png", Data: b64(8)},
		{Type: "text", Text: "after"},
		{Type: "audio", MimeType: "audio/wav", Data: b64(8)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"text", "file", "text", "file"}
	if len(got) != len(want) {
		t.Fatalf("got %d parts, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i]["type"] != want[i] {
			t.Fatalf("part %d = %v, want %v", i, got[i]["type"], want[i])
		}
	}
	if got[0]["text"] != "before" || got[2]["text"] != "after" {
		t.Fatalf("text order lost: %+v", got)
	}
}

// TestPromptPartsRejectsUnadvertisedInput proves a block is refused, not
// silently dropped: a dropped attachment leaves the model a prompt that
// references something it never received (PLAN P3 step 8).
func TestPromptPartsRejectsUnadvertisedInput(t *testing.T) {
	h := newRecorder()
	s := newThinkingSession(t, h, "opencode/text-only")
	s.d.surfaces.replace(map[string]modelSurface{
		"opencode/text-only": {Attachment: false, Inputs: picker.ModelInputs{Text: true}},
	})
	_, err := s.promptParts([]provider.Content{{Type: "image", MimeType: "image/png", Data: b64(8)}})
	if err == nil {
		t.Fatal("a text-only model accepted an image block")
	}
	if !strings.Contains(err.Error(), "does not accept image") {
		t.Fatalf("unhelpful error: %v", err)
	}
	// The prompt as a whole must fail rather than send a partial turn.
	if err := s.Prompt(context.Background(), []provider.Content{
		{Type: "text", Text: "look"},
		{Type: "image", MimeType: "image/png", Data: b64(8)},
	}); err == nil {
		t.Fatal("Prompt accepted an unsupported attachment")
	}
	for _, c := range h.calls {
		if strings.Contains(c.path, "prompt_async") {
			t.Fatal("a rejected prompt still reached the engine")
		}
	}
}

// TestPromptFrameBoundaryIsExact pins acceptance and rejection either side of
// the 1 MiB serialized budget.
func TestPromptFrameBoundaryIsExact(t *testing.T) {
	fit := []map[string]any{{"type": "text", "text": strings.Repeat("a", maxPromptFrameBytes-64)}}
	b, _ := json.Marshal(fit)
	if len(b) > maxPromptFrameBytes {
		t.Fatalf("test fixture is already oversized: %d", len(b))
	}
	if err := checkPromptFrame(fit); err != nil {
		t.Fatalf("a payload of %d bytes was rejected: %v", len(b), err)
	}
	// Grow until it crosses, then confirm the exact edge behaviour.
	over := []map[string]any{{"type": "text", "text": strings.Repeat("a", maxPromptFrameBytes+1)}}
	if err := checkPromptFrame(over); err == nil {
		t.Fatal("an oversized payload was accepted")
	}
	// Exactly at the limit is accepted.
	pad := maxPromptFrameBytes - len(mustJSON(t, []map[string]any{{"type": "text", "text": ""}}))
	exact := []map[string]any{{"type": "text", "text": strings.Repeat("a", pad)}}
	if n := len(mustJSON(t, exact)); n != maxPromptFrameBytes {
		t.Fatalf("could not build an exactly-limit payload: %d", n)
	}
	if err := checkPromptFrame(exact); err != nil {
		t.Fatalf("a payload of exactly %d bytes was rejected: %v", maxPromptFrameBytes, err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestPromptRejectsOversizedAttachmentBeforeHTTP proves the frame check runs
// before any request is created.
func TestPromptRejectsOversizedAttachmentBeforeHTTP(t *testing.T) {
	h := newRecorder()
	s := multimodalSession(t, h)
	err := s.Prompt(context.Background(), []provider.Content{
		{Type: "image", MimeType: "image/png", Data: b64(maxPromptFrameBytes)},
	})
	if err == nil {
		t.Fatal("an oversized attachment was accepted")
	}
	for _, c := range h.calls {
		if strings.Contains(c.path, "prompt_async") {
			t.Fatal("an oversized attachment still reached the engine")
		}
	}
}

// TestPromptSendsFilePartsToEngine proves a valid attachment reaches the wire
// in the documented shape.
func TestPromptSendsFilePartsToEngine(t *testing.T) {
	h := newRecorder()
	s := multimodalSession(t, h)
	if err := s.Prompt(context.Background(), []provider.Content{
		{Type: "text", Text: "describe"},
		{Type: "image", MimeType: "image/png", Filename: "a.png", Data: b64(16)},
	}); err != nil {
		t.Fatal(err)
	}
	call := h.find(t, "POST", "/prompt_async")
	parts, ok := call.body["parts"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("parts = %#v", call.body["parts"])
	}
	file, _ := parts[1].(map[string]any)
	if file["type"] != "file" || file["mime"] != "image/png" || file["filename"] != "a.png" {
		t.Fatalf("file part = %+v", file)
	}
	url, _ := file["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("url = %q", url)
	}
}

// TestPromptNeverLogsAttachmentBytes proves neither the data URL nor the raw
// payload appears in the session log (PLAN P3 step 8).
func TestPromptNeverLogsAttachmentBytes(t *testing.T) {
	var buf strings.Builder
	h := newRecorder()
	s := multimodalSession(t, h)
	h.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	secret := base64.StdEncoding.EncodeToString([]byte("SUPERSECRETPIXELS"))
	if err := s.Prompt(context.Background(), []provider.Content{
		{Type: "image", MimeType: "image/png", Filename: "a.png", Data: secret},
	}); err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	for _, forbidden := range []string{secret, "SUPERSECRETPIXELS", "data:image/png;base64"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("log leaked attachment content %q:\n%s", forbidden, logged)
		}
	}
}
