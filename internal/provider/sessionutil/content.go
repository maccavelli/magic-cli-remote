// Package sessionutil holds transport-neutral provider session helpers.
package sessionutil

import (
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// CloneContent returns an independent top-level slice for queued prompts.
func CloneContent(parts []provider.Content) []provider.Content {
	if len(parts) == 0 {
		return nil
	}
	out := make([]provider.Content, len(parts))
	copy(out, parts)
	return out
}

// UserMessage builds the transcript representation of a submitted prompt.
// Attachment payloads are intentionally omitted; only their descriptors belong
// in the event stream and durable transcript.
func UserMessage(parts []provider.Content) event.Event {
	var text strings.Builder
	var attachments []event.AttachmentInfo
	for _, part := range parts {
		switch part.Type {
		case "", "text":
			text.WriteString(part.Text)
		case "image", "audio":
			attachments = append(attachments, event.AttachmentInfo{
				Kind:     part.Type,
				MimeType: part.MimeType,
			})
		}
	}
	ev := event.Event{Type: event.TypeUserMessage, Text: text.String()}
	if len(attachments) > 0 {
		ev.Attachments = attachments
	}
	return ev
}
