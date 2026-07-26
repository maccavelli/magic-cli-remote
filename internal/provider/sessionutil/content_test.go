package sessionutil

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestCloneContent(t *testing.T) {
	in := []provider.Content{{Type: "text", Text: "one"}, {Type: "image", Data: "payload"}}
	got := CloneContent(in)
	if len(got) != len(in) || &got[0] == &in[0] {
		t.Fatalf("CloneContent did not make an independent slice: %#v", got)
	}
	got[0].Text = "changed"
	if in[0].Text != "one" {
		t.Fatalf("input mutated: %#v", in)
	}
	if CloneContent(nil) != nil {
		t.Fatal("CloneContent(nil) must be nil")
	}
}

func TestUserMessageOmitsAttachmentPayloads(t *testing.T) {
	ev := UserMessage([]provider.Content{
		{Type: "text", Text: "hello"},
		{Type: "image", MimeType: "image/png", Data: "secret-base64"},
		{Type: "", Text: " world"},
		{Type: "audio", MimeType: "audio/wav", Data: "more-secret"},
		{Type: "unknown", Text: "ignored"},
	})
	if ev.Text != "hello world" {
		t.Fatalf("text = %q, want hello world", ev.Text)
	}
	if len(ev.Attachments) != 2 {
		t.Fatalf("attachments = %#v, want 2", ev.Attachments)
	}
	if ev.Attachments[0].Kind != "image" || ev.Attachments[0].MimeType != "image/png" {
		t.Fatalf("first attachment = %#v", ev.Attachments[0])
	}
	if ev.Attachments[1].Kind != "audio" || ev.Attachments[1].MimeType != "audio/wav" {
		t.Fatalf("second attachment = %#v", ev.Attachments[1])
	}
}
