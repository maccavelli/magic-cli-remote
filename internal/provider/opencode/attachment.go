package opencode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	// maxPromptFrameBytes mirrors the daemon's inbound WebSocket budget
	// (internal/ws.maxWSMessageBytes). The engine request is measured against
	// the same figure, and against the *serialized* parts rather than the raw
	// attachment, because base64 inflates by 4/3 and the JSON envelope adds
	// more: an attachment-only estimate passes payloads the frame cannot hold
	// (MADR 0112 A2, PLAN P3 step 7).
	maxPromptFrameBytes = 1 << 20
	// maxAttachmentFilenameBytes bounds the basename carried to the engine.
	maxAttachmentFilenameBytes = 256
	// maxAttachmentMIMEBytes bounds the declared media type.
	maxAttachmentMIMEBytes = 128
)

// attachmentKinds maps a prompt content kind to its required MIME family. A
// kind outside this table is not an attachment this provider will send.
var attachmentKinds = map[string]string{
	"image": "image/",
	"audio": "audio/",
}

// validAttachmentFilename accepts only a bare basename.
//
// A path separator, NUL or control character in a filename is either a
// traversal attempt or a corrupt client, and neither belongs in a value the
// engine may use to name a file on the host.
func validAttachmentFilename(name string) error {
	if name == "" {
		return nil // optional; omitted rather than invented
	}
	if len(name) > maxAttachmentFilenameBytes {
		return fmt.Errorf("attachment filename exceeds %d bytes", maxAttachmentFilenameBytes)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("attachment filename is not valid UTF-8")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("attachment filename must be a bare basename")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("attachment filename contains a control character")
		}
	}
	if name == "." || name == ".." {
		return fmt.Errorf("attachment filename must be a bare basename")
	}
	return nil
}

// validAttachmentMIME checks the declared media type against the content kind.
// A mismatch is refused rather than corrected: relabelling an audio blob as an
// image would hand the model something it cannot decode.
func validAttachmentMIME(kind, mime string) error {
	family, ok := attachmentKinds[kind]
	if !ok {
		return fmt.Errorf("unsupported attachment kind %q", kind)
	}
	if mime == "" {
		return fmt.Errorf("%s attachment has no media type", kind)
	}
	if len(mime) > maxAttachmentMIMEBytes {
		return fmt.Errorf("attachment media type exceeds %d bytes", maxAttachmentMIMEBytes)
	}
	lower := strings.ToLower(mime)
	if !strings.HasPrefix(lower, family) {
		return fmt.Errorf("%s attachment declares media type %q", kind, mime)
	}
	if strings.ContainsAny(mime, " \t\r\n\x00;,") {
		return fmt.Errorf("attachment media type has unexpected characters")
	}
	if strings.TrimPrefix(lower, family) == "" {
		return fmt.Errorf("attachment media type has no subtype")
	}
	return nil
}

// decodeAttachmentData validates canonical base64 with non-empty content.
//
// Strict decoding matters: a lax decoder accepts non-canonical padding and
// alphabet variants that re-encode to different bytes, so what the daemon
// measured and what the engine receives could differ.
func decodeAttachmentData(data string) ([]byte, error) {
	if data == "" {
		return nil, fmt.Errorf("attachment has no data")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("attachment data is not canonical base64")
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("attachment decodes to zero bytes")
	}
	return raw, nil
}

// filePartInput converts one validated non-text block into the stable
// FilePartInput shape: a data URL carrying the exact bytes the client sent.
func filePartInput(p provider.Content) (map[string]any, error) {
	if err := validAttachmentMIME(p.Type, p.MimeType); err != nil {
		return nil, err
	}
	if err := validAttachmentFilename(p.Filename); err != nil {
		return nil, err
	}
	raw, err := decodeAttachmentData(p.Data)
	if err != nil {
		return nil, err
	}
	// Re-encode from the decoded bytes so the URL is canonical regardless of
	// how the client spelled it.
	part := map[string]any{
		"type": "file",
		"mime": p.MimeType,
		"url":  "data:" + p.MimeType + ";base64," + base64.StdEncoding.EncodeToString(raw),
	}
	if p.Filename != "" {
		part["filename"] = p.Filename
	}
	return part, nil
}

// promptParts converts prompt content into engine parts, preserving the order
// the user composed them in.
//
// image and audio are admitted only when the active model advertises that
// input. Refusing is deliberate: silently dropping a block would send the model
// a prompt that references an attachment it never received (PLAN P3 step 8).
func (o *httpSession) promptParts(parts []provider.Content) ([]map[string]any, error) {
	image, audio := o.PromptCapabilities()
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "", "text":
			out = append(out, map[string]any{"type": "text", "text": p.Text})
		case "image", "audio":
			if (p.Type == "image" && !image) || (p.Type == "audio" && !audio) {
				return nil, fmt.Errorf("the active model does not accept %s input", p.Type)
			}
			part, err := filePartInput(p)
			if err != nil {
				return nil, err
			}
			out = append(out, part)
		default:
			return nil, fmt.Errorf("unsupported prompt content %q", p.Type)
		}
	}
	if err := checkPromptFrame(out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkPromptFrame rejects a parts list whose serialized form cannot fit the
// frame budget. Measuring the encoded JSON is the point: it is the only figure
// that accounts for base64 inflation and the envelope around it.
func checkPromptFrame(parts []map[string]any) error {
	b, err := json.Marshal(parts)
	if err != nil {
		return fmt.Errorf("prompt parts are not serializable: %w", err)
	}
	if len(b) > maxPromptFrameBytes {
		return fmt.Errorf("prompt exceeds the %d byte frame budget", maxPromptFrameBytes)
	}
	return nil
}
