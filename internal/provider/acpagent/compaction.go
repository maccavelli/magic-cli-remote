package acpagent

import (
	"strings"
	"time"
	"unicode/utf8"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// maxCompactionSummary caps one compaction's summary text. The agent chooses
// its length, and the phone shows it collapsed (MADR 0190 D1).
const maxCompactionSummary = 32 << 10

// maxCompactions caps how many compaction IDs one session tracks; the oldest
// is forgotten first. The agent chooses the IDs, so the set must be bounded.
const maxCompactions = 16

// compactionEntry is the current state of one compaction.
type compactionEntry struct {
	status    string
	summary   []byte
	err       string
	truncated bool
}

// compactionFold folds compaction_update and compaction_summary_chunk into
// one state per ID (MADR 0190 D1). Touched only from the notification
// handler goroutine, like cmdDedupe, so it takes no lock.
type compactionFold struct {
	order []acp.CompactionId
	byID  map[acp.CompactionId]*compactionEntry
}

// entry returns the state for id, creating an in-progress one for an ID not
// seen before ("a first-seen ID creates an in-progress compaction"). At the
// cap, the oldest ID is forgotten; if it reappears it starts over.
func (f *compactionFold) entry(id acp.CompactionId) (e *compactionEntry, created bool) {
	if e, ok := f.byID[id]; ok {
		return e, false
	}
	if f.byID == nil {
		f.byID = make(map[acp.CompactionId]*compactionEntry)
	}
	if len(f.order) >= maxCompactions {
		delete(f.byID, f.order[0])
		copy(f.order, f.order[1:])
		f.order = f.order[:len(f.order)-1]
	}
	e = &compactionEntry{status: string(acp.CompactionStatusInProgress)}
	f.order = append(f.order, id)
	f.byID[id] = e
	return e, true
}

// appendSummary adds text to e's summary, up to maxCompactionSummary, cutting
// on a rune boundary and marking the summary truncated once anything is lost.
func (e *compactionEntry) appendSummary(text string) {
	if e.truncated || text == "" {
		return
	}
	room := maxCompactionSummary - len(e.summary)
	if len(text) <= room {
		e.summary = append(e.summary, text...)
		return
	}
	cut := room
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	e.summary = append(e.summary, text[:cut]...)
	e.truncated = true
}

// appendCompactionBlocks adds the text of blocks to e's summary. A non-text
// block has nothing the phone can show, so it is skipped and declined by name.
func (s *session) appendCompactionBlocks(e *compactionEntry, blocks []acp.ContentBlock) {
	for _, b := range blocks {
		if b.Text != nil {
			e.appendSummary(b.Text.Text)
			continue
		}
		s.declineUpdate("compaction.summary." + blockKind(b))
	}
}

// blockKind names a content block's variant, for a decline.
func blockKind(b acp.ContentBlock) string {
	switch {
	case b.Image != nil:
		return "image"
	case b.Audio != nil:
		return "audio"
	case b.ResourceLink != nil:
		return "resource_link"
	case b.Resource != nil:
		return "resource"
	default:
		return "unknown"
	}
}

// onCompactionUpdate applies a compaction_update and emits the compaction's
// state. ACP gives summary and error patch semantics; the SDK decodes an
// omitted field and a null alike, so nil means unchanged and Magic cannot
// follow a null that clears (MADR 0190 D1). An empty summary clears.
func (s *session) onCompactionUpdate(u *acp.SessionCompactionUpdate, now time.Time) {
	if len(u.Meta) > 0 {
		s.declineUpdate("compaction_update._meta")
	}
	e, _ := s.compactions.entry(u.CompactionId)
	e.status = string(u.Status)
	if u.Summary != nil {
		e.summary, e.truncated = e.summary[:0], false
		s.appendCompactionBlocks(e, u.Summary)
	}
	if u.Error != nil {
		e.err = strings.TrimSpace(*u.Error)
	}
	s.emitCompaction(u.CompactionId, e, now)
}

// onCompactionChunk appends a summary chunk. Only a chunk that creates the
// compaction emits; later chunks accumulate until the next update, so a
// streamed summary costs two events rather than one per chunk.
func (s *session) onCompactionChunk(c *acp.SessionUpdateCompactionSummaryChunk, now time.Time) {
	if len(c.Meta) > 0 {
		s.declineUpdate("compaction_summary_chunk._meta")
	}
	e, created := s.compactions.entry(c.CompactionId)
	s.appendCompactionBlocks(e, []acp.ContentBlock{c.Content})
	if created {
		s.emitCompaction(c.CompactionId, e, now)
	}
}

func (s *session) emitCompaction(id acp.CompactionId, e *compactionEntry, now time.Time) {
	s.emit(event.Event{
		Type:                event.TypeCompaction,
		SessionID:           s.localID,
		Timestamp:           now,
		CompactionID:        string(id),
		CompactionStatus:    e.status,
		CompactionSummary:   string(e.summary),
		CompactionError:     e.err,
		CompactionTruncated: e.truncated,
	})
}
