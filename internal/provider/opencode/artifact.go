package opencode

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

const (
	// maxArtifactFilenameBytes and maxArtifactMIMEBytes bound display metadata.
	maxArtifactFilenameBytes = 256
	maxArtifactMIMEBytes     = 128
	// maxArtifactURLBytes bounds a forwarded https URL.
	maxArtifactURLBytes = 2048
)

// artifactFrom builds a bounded artifact from an engine FilePart.
//
// It always returns an artifact: one the daemon cannot safely carry is reported
// as metadata with Truncated set, because silently dropping a file the agent
// produced is worse than showing that it exists but could not be delivered.
func artifactFrom(p nativePart) *event.Artifact {
	a := &event.Artifact{
		Filename: clipRunes(strings.TrimSpace(p.Filename), maxArtifactFilenameBytes),
		MIME:     clipRunes(strings.TrimSpace(p.Mime), maxArtifactMIMEBytes),
	}
	raw := strings.TrimSpace(p.URL)
	if raw == "" {
		a.Truncated = true
		return a
	}
	switch {
	case strings.HasPrefix(strings.ToLower(raw), "data:"):
		data, n, err := decodeArtifactDataURL(raw)
		if err != nil {
			a.Truncated = true
			return a
		}
		a.Data, a.Bytes = data, n
	default:
		u, err := validArtifactURL(raw)
		if err != nil {
			a.Truncated = true
			return a
		}
		a.URL = u
	}
	return a
}

// validArtifactURL accepts only an https URL with no userinfo.
//
// The daemon never fetches it, so the risk is not SSRF here but what a client
// is invited to open: http: is downgradeable, file: names the daemon host's own
// filesystem, and userinfo smuggles credentials into a link a user may share.
func validArtifactURL(raw string) (string, error) {
	if len(raw) > maxArtifactURLBytes {
		return "", fmt.Errorf("artifact url exceeds %d bytes", maxArtifactURLBytes)
	}
	if strings.ContainsAny(raw, " \t\r\n\x00") {
		return "", fmt.Errorf("artifact url has unexpected characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("artifact url is unparseable")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("artifact url scheme %q is not https", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("artifact url carries userinfo")
	}
	if u.Host == "" {
		return "", fmt.Errorf("artifact url has no host")
	}
	return u.String(), nil
}

// decodeArtifactDataURL validates a bounded base64 data URL and returns the
// canonical payload plus its decoded size.
func decodeArtifactDataURL(raw string) (string, int64, error) {
	rest := raw[len("data:"):]
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", 0, fmt.Errorf("data url has no payload")
	}
	meta, payload := rest[:comma], rest[comma+1:]
	if !strings.Contains(strings.ToLower(meta), "base64") {
		// Percent-encoded data URLs are legal but are not what the engine
		// emits; refusing keeps exactly one decoder on this path.
		return "", 0, fmt.Errorf("data url is not base64")
	}
	// Reject before decoding so a huge payload never reaches the decoder. The
	// bound is the exact encoded length of the budget rather than a 4/3
	// estimate: padding makes the estimate overshoot by up to two bytes, which
	// would reject a payload of exactly the maximum size.
	if len(payload) > base64.StdEncoding.EncodedLen(event.MaxArtifactInlineBytes) {
		return "", 0, fmt.Errorf("data url exceeds the inline budget")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		return "", 0, fmt.Errorf("data url payload is not canonical base64")
	}
	if len(decoded) == 0 {
		return "", 0, fmt.Errorf("data url decodes to zero bytes")
	}
	if int64(len(decoded)) > event.MaxArtifactInlineBytes {
		return "", 0, fmt.Errorf("data url exceeds the inline budget")
	}
	return base64.StdEncoding.EncodeToString(decoded), int64(len(decoded)), nil
}

// artifactPartID gives a tool attachment a deterministic native identity.
//
// The tool part's own id plus the attachment index is stable across live
// streaming and replay, which is what lets an artifact deduplicate on resume
// and be removed by a tombstone like any other row.
func artifactPartID(toolPartID string, index int) string {
	return toolPartID + "#" + strconv.Itoa(index)
}

// clipRunes truncates on a rune boundary.
func clipRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
