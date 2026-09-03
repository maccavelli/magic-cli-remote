package acphttp

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestAttachmentsAreGatedOnAdvertisedCapabilities is MADR 0137 F10b.
//
// This transport sent image blocks unconditionally. It happened to be harmless
// only because goose 1.48.0 advertises `image: true`; an agent advertising
// false would have received a block it said it could not accept, which is a
// protocol violation. `acpagent` has gated this since it was written — this
// brings the second ACP transport in line rather than leaving one trusting to
// luck.
func TestAttachmentsAreGatedOnAdvertisedCapabilities(t *testing.T) {
	parts := []provider.Content{
		{Type: "text", Text: "look at this"},
		{Type: "image", MimeType: "image/png", Data: "AAAA"},
		{Type: "audio", MimeType: "audio/wav", Data: "BBBB"},
	}

	t.Run("advertised: both attachments are sent", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log := slog.New(slog.NewTextHandler(buf, nil))
		_, blocks, atts := buildPrompt(parts,
			acp.PromptCapabilities{Image: true, Audio: true}, log)
		if len(atts) != 2 {
			t.Fatalf("attachments = %d, want 2", len(atts))
		}
		if countImageBlocks(blocks) != 1 || countAudioBlocks(blocks) != 1 {
			t.Fatalf("blocks wrong: %d image, %d audio",
				countImageBlocks(blocks), countAudioBlocks(blocks))
		}
	})

	t.Run("not advertised: dropped with a warning, turn not failed", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log := slog.New(slog.NewTextHandler(buf, nil))
		text, blocks, atts := buildPrompt(parts, acp.PromptCapabilities{}, log)
		if len(atts) != 0 {
			t.Fatalf("attachments = %d, want 0: the agent advertised neither", len(atts))
		}
		if countImageBlocks(blocks) != 0 || countAudioBlocks(blocks) != 0 {
			t.Fatal("an attachment block was sent to an agent that advertised no capability for it")
		}
		// The text still goes: dropping an attachment must not fail the turn.
		if text != "look at this" {
			t.Fatalf("text = %q, want the prompt to survive", text)
		}
		if !strings.Contains(buf.String(), "promptCapabilities.image") {
			t.Errorf("no warning naming the missing capability:\n%s", buf.String())
		}
	})

	t.Run("goose 1.48.0's real capabilities: image yes, audio no", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log := slog.New(slog.NewTextHandler(buf, nil))
		_, blocks, atts := buildPrompt(parts,
			acp.PromptCapabilities{Image: true, Audio: false, EmbeddedContext: true}, log)
		if len(atts) != 1 || atts[0].Kind != "image" {
			t.Fatalf("attachments = %+v, want the image only", atts)
		}
		if countAudioBlocks(blocks) != 0 {
			t.Fatal("audio was sent to goose, which advertises audio: false")
		}
	})
}

func countImageBlocks(blocks []acp.ContentBlock) int {
	n := 0
	for _, b := range blocks {
		if b.Image != nil {
			n++
		}
	}
	return n
}

func countAudioBlocks(blocks []acp.ContentBlock) int {
	n := 0
	for _, b := range blocks {
		if b.Audio != nil {
			n++
		}
	}
	return n
}
