package opencode

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// nativeToolState is the ToolPart state the transcript renders.
type nativeToolState struct {
	Status string          `json:"status"`
	Title  string          `json:"title"`
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
	Error  string          `json:"error"`
	Time   struct {
		// Compacted marks a tool part the engine folded into a compaction
		// summary. Only ToolStateCompleted carries it (MADR 0112 A3).
		Compacted *float64 `json:"compacted"`
	} `json:"time"`
}

// nativePart is one member of the 1.18.21 `Part` union, decoded to the fields
// the transcript needs. It is deliberately the same struct for live SSE frames
// and replayed history: one shape, one mapper, one result.
type nativePart struct {
	ID        string          `json:"id"`
	MessageID string          `json:"messageID"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Tool      string          `json:"tool"`
	CallID    string          `json:"callID"`
	State     nativeToolState `json:"state"`
	// Mime/Filename/URL belong to FilePart and are carried so P5 can surface
	// assistant artifacts without a second decode pass.
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

// Wire `type` discriminators for the exact 1.18.21 Part union. Every member is
// named here so a future addition is visibly absent rather than silently
// falling into a default branch (PLAN P4 step 4).
const (
	partText       = "text"
	partReasoning  = "reasoning"
	partTool       = "tool"
	partFile       = "file"
	partSubtask    = "subtask"
	partAgent      = "agent"
	partStepStart  = "step-start"
	partStepFinish = "step-finish"
	partSnapshot   = "snapshot"
	partPatch      = "patch"
	partRetry      = "retry"
	partCompaction = "compaction"
)

// partDisposition says what the transcript does with one union member.
type partDisposition int

const (
	// dispositionChat renders the part as a transcript row.
	dispositionChat partDisposition = iota
	// dispositionElsewhere is consumed by another path (subagent tree, agent
	// mode) and must not become a second chat row.
	dispositionElsewhere
	// dispositionInternal is engine bookkeeping the user never sees.
	dispositionInternal
	// dispositionUnknown is a member this release did not define.
	dispositionUnknown
)

// dispositionOf classifies a wire part type.
//
// The internal group matters as much as the rendered one: step, snapshot,
// patch, retry and compaction parts describe how the engine did its work, and
// rendering them as chat invents content the agent never said.
func dispositionOf(partType string) partDisposition {
	switch partType {
	case partText, partReasoning, partTool, partFile:
		return dispositionChat
	case partSubtask, partAgent:
		return dispositionElsewhere
	case partStepStart, partStepFinish, partSnapshot, partPatch, partRetry, partCompaction:
		return dispositionInternal
	default:
		return dispositionUnknown
	}
}

// mapPart converts one native part into a transcript event.
//
// role is the message's role, replace marks an authoritative snapshot (a full
// `message.part.updated` frame or a replayed part) rather than an append delta.
// The second result is false when the part produces no chat row at all, which
// is the normal outcome for more than half the union.
//
// This is a pure function on purpose: live SSE and replay call it with the same
// input and must get the same answer, and that is only testable if nothing else
// is in scope (PLAN P4 step 4).
func mapPart(role string, p nativePart, replace bool, log *slog.Logger) (event.Event, bool) {
	switch dispositionOf(p.Type) {
	case dispositionElsewhere, dispositionInternal:
		return event.Event{}, false
	case dispositionUnknown:
		// Never rendered as raw text: an unknown member's fields are not
		// guaranteed to be prose, and guessing would put engine internals into
		// the conversation.
		if log != nil {
			log.Debug("opencode part type not in the 1.18.21 union; dropped",
				slog.String("part_type", p.Type), slog.String("part_id", p.ID))
		}
		return event.Event{}, false
	}

	ev := event.Event{
		NativeMessageID: p.MessageID,
		NativePartID:    p.ID,
		Replace:         replace,
	}
	switch p.Type {
	case partText:
		if role == "user" {
			ev.Type = event.TypeUserMessage
		} else {
			ev.Type = event.TypeAssistantChunk
		}
		ev.Text = p.Text
	case partReasoning:
		// A user cannot author reasoning; treating one as a thought chunk would
		// attribute engine output to the person.
		if role == "user" {
			return event.Event{}, false
		}
		ev.Type = event.TypeThoughtChunk
		ev.Text = p.Text
	case partTool:
		ev.Type = event.TypeToolCall
		ev.ToolID = p.CallID
		ev.ToolName = firstNonEmpty(p.State.Title, p.Tool, "tool")
		ev.ToolKind = kindForTool(p.Tool)
		ev.Status = mapToolStatus(p.State.Status)
		ev.Text = toolVisibleOutput(p.State)
	case partFile:
		// Assistant artifacts land in P5; the part is decoded and identified
		// here so that phase adds rendering only.
		return event.Event{}, false
	}
	return ev, true
}

// toolVisibleOutput renders the user-visible detail for a tool part.
//
// The precedence is the same one the live SSE path has always used — error
// beats output beats title beats a clipped input echo — so a replayed tool card
// reads identically to the one streamed live. Sharing this function is the
// point: two implementations of the same precedence drift, and the drift only
// shows up after a resume (PLAN P4 step 5).
//
// Output keeps its line structure and is capped at maxToolOutputChars; a
// directory listing or grep result is unreadable once newlines collapse.
func toolVisibleOutput(st nativeToolState) string {
	if st.Error != "" {
		return clip(st.Error, 300)
	}
	if out := strings.TrimRight(st.Output, " \t\n"); out != "" {
		return clipBlock(out, maxToolOutputChars)
	}
	if title := strings.TrimSpace(st.Title); title != "" {
		return title
	}
	return shortJSON(st.Input, 300)
}

// isCompacted reports a tool part the engine folded into a compaction summary.
// Only ToolStateCompleted carries the marker on 1.18.21.
func (s nativeToolState) isCompacted() bool { return s.Time.Compacted != nil }
