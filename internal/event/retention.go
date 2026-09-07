package event

import "unsafe"

// Class ranks an event for eviction from a bounded transcript.
//
// The ring this serves used to count events, and counting is what let one
// provider's telemetry evict the operator's own words: of 34 stored sessions,
// six had their history truncated and two retained no `user_message` at all,
// while one codex session spent 695 of its 723 retained slots on the stdout of
// a single command (MADR 0138 F1, F16). Event sizes vary by more than 100×
// (216 B for an assistant chunk, 5,120 B for a command advertisement), so a
// count was never a budget either.
//
// Lower classes are evicted first, and no event is evicted while any event of a
// lower class remains. ClassAnchor is what makes the failure above impossible:
// a user's message is only dropped when the transcript holds nothing but
// anchors and is still over budget.
type Class uint8

const (
	// ClassTelemetry is state a client re-learns from the next report. Dropping
	// it loses nothing that is not restated shortly afterwards.
	ClassTelemetry Class = iota
	// ClassProgress is in-flight detail: streamed reasoning, tool output as it
	// arrives, plan snapshots. Useful while a turn runs, largely redundant once
	// the turn's result is in the transcript.
	ClassProgress
	// ClassContent is the agent's produced output and the actions it took.
	ClassContent
	// ClassAnchor is what a conversation is: what the operator asked, what
	// bounded each turn, what they were asked to approve and what they decided.
	// Losing one of these makes the rest unreadable.
	ClassAnchor
)

// String renders a class for test failures and log lines.
func (c Class) String() string {
	switch c {
	case ClassTelemetry:
		return "telemetry"
	case ClassProgress:
		return "progress"
	case ClassContent:
		return "content"
	case ClassAnchor:
		return "anchor"
	default:
		return "unknown"
	}
}

// ClassOf ranks t. It is exhaustive over every declared Type, pinned by
// TestClassOfIsExhaustive — an unclassified new type would otherwise fall to
// the default and be evicted first, which is the wrong answer for anything
// worth adding.
func ClassOf(t Type) Class {
	switch t {
	// Anchors: the conversation's own structure.
	case TypeUserMessage,
		TypeTurnComplete,
		TypeError,
		TypePermission,
		TypePermissionResolved,
		TypeQuestion,
		TypeQuestionResolved,
		// A tombstone withdraws content. Evicting it would resurrect what the
		// agent retracted, which is worse than losing the retraction.
		TypeTranscriptRemove:
		return ClassAnchor

	// Content: what the agent said and did.
	case TypeAssistantChunk,
		TypeToolCall,
		TypeArtifact,
		TypeSessionTitle,
		TypeGoal,
		TypeCodexModelReroute,
		TypeCodexTerminalInteraction:
		return ClassContent

	// Progress: detail that made sense while the turn ran.
	case TypeThoughtChunk,
		TypeToolUpdate,
		TypePlan,
		TypeSessionStatus,
		TypeSubagents,
		TypeCodexProgress,
		TypeCodexUnsupportedItem:
		return ClassProgress

	// Telemetry: restated by the next report.
	case TypeAvailableCommands,
		TypeRemoteCommands,
		TypeNotice,
		TypeUsage,
		TypeMode,
		TypeSessionConfig,
		TypeSessionCapabilities,
		TypeDiagnosticsChanged,
		TypeApprovalSummary,
		TypeCollaboration,
		TypeCodexWarning,
		TypeCodexModelVerification:
		return ClassTelemetry
	}
	return ClassTelemetry
}

// eventHeaderBytes is the in-memory size of an Event's fixed part: 44 fields of
// string headers, slice headers, pointers, times and scalars, before any string
// content or slice backing.
//
// Taken from the type rather than written down, so it cannot go stale. Adding a
// field the budget does not count is how a budget silently stops bounding
// anything (MADR 0138, amendment step 2).
const eventHeaderBytes = int(unsafe.Sizeof(Event{}))

// Bytes reports the approximate retained size of ev: the struct header plus the
// length of every string it owns and the retained size of every slice element.
//
// Approximate on purpose. It is a budget input, not an allocator: it does not
// account for map overhead, string interning, or the slack in a slice's
// capacity. It must never *under*-report by much, which is what the accuracy
// test pins against runtime.ReadMemStats.
func Bytes(ev *Event) int {
	n := eventHeaderBytes
	n += len(ev.SessionID) + len(string(ev.Type)) + len(ev.Status) + len(ev.Text)
	n += len(ev.ToolID) + len(ev.ToolName) + len(ev.ToolKind) + len(ev.Error)
	n += len(ev.Title) + len(ev.ErrorKind) + len(ev.PermissionID)
	n += len(ev.DeviceID) + len(ev.OptionID) + len(ev.QuestionID)
	n += len(ev.ApprovalGroupID) + len(ev.AgentSessionID) + len(ev.StopReason)
	n += len(ev.CurrentModeID) + len(ev.ApprovalsReviewer)
	n += len(ev.CurrentCollaborationModeID)
	n += len(ev.NativeMessageID) + len(ev.NativePartID)

	for i := range ev.Options {
		n += optionBytes + len(ev.Options[i].OptionID) + len(ev.Options[i].Name) + len(ev.Options[i].Kind)
	}
	for i := range ev.Questions {
		n += questionBytes + len(ev.Questions[i].ID) + len(ev.Questions[i].Header) + len(ev.Questions[i].Text)
		for j := range ev.Questions[i].Options {
			n += optionBytes + len(ev.Questions[i].Options[j].OptionID) + len(ev.Questions[i].Options[j].Name)
		}
	}
	for i := range ev.Commands {
		n += commandBytes + len(ev.Commands[i].Name) + len(ev.Commands[i].Description) + len(ev.Commands[i].Hint)
	}
	for i := range ev.RemoteCommands {
		n += remoteCommandBytes + len(ev.RemoteCommands[i].Name) +
			len(ev.RemoteCommands[i].Description) + len(ev.RemoteCommands[i].Hint) +
			len(ev.RemoteCommands[i].Reason)
	}
	for i := range ev.Entries {
		n += planEntryBytes + len(ev.Entries[i].Content) + len(ev.Entries[i].Status) + len(ev.Entries[i].Priority)
	}
	for i := range ev.Approvals {
		n += approvalBytes + len(ev.Approvals[i].ToolName) + len(ev.Approvals[i].Detail)
	}
	for i := range ev.Subagents {
		n += subagentBytes + len(ev.Subagents[i].ID) + len(ev.Subagents[i].Name) + len(ev.Subagents[i].Status)
	}
	for i := range ev.Modes {
		n += modeBytes + len(ev.Modes[i].ID) + len(ev.Modes[i].Name) + len(ev.Modes[i].Description)
	}
	for i := range ev.CollaborationModes {
		n += collabModeBytes + len(ev.CollaborationModes[i].ID) + len(ev.CollaborationModes[i].Name)
	}
	for i := range ev.ConfigOptions {
		n += configOptionBytes + len(ev.ConfigOptions[i].ID) + len(ev.ConfigOptions[i].Name) +
			len(ev.ConfigOptions[i].Description) + len(ev.ConfigOptions[i].Kind) +
			len(ev.ConfigOptions[i].CurrentValue)
		n += configOptionValueBytes * len(ev.ConfigOptions[i].Values)
	}
	for i := range ev.Attachments {
		n += attachmentBytes + len(ev.Attachments[i].Kind) + len(ev.Attachments[i].MimeType)
	}

	if ev.Usage != nil {
		n += int(unsafe.Sizeof(Usage{}))
	}
	if ev.Capabilities != nil {
		n += int(unsafe.Sizeof(Capabilities{}))
	}
	if ev.Goal != nil {
		n += int(unsafe.Sizeof(Goal{})) + len(ev.Goal.Objective) + len(ev.Goal.Status)
	}
	if ev.Artifact != nil {
		n += int(unsafe.Sizeof(Artifact{})) + len(ev.Artifact.Filename) +
			len(ev.Artifact.MIME) + len(ev.Artifact.URL) + len(ev.Artifact.Data)
	}
	if ev.Codex != nil {
		n += int(unsafe.Sizeof(CodexPayload{})) + len(ev.Codex.Key) + len(ev.Codex.Kind) +
			len(ev.Codex.Status) + len(ev.Codex.Title) + len(ev.Codex.Text)
	}
	return n
}

// Per-element header sizes for the slice fields Bytes walks. Taken from the
// types so they track a field being added to any of them.
var (
	optionBytes        = int(unsafe.Sizeof(PermissionOption{}))
	questionBytes      = int(unsafe.Sizeof(QuestionItem{}))
	commandBytes       = int(unsafe.Sizeof(AvailableCommand{}))
	remoteCommandBytes = int(unsafe.Sizeof(RemoteCommand{}))
	planEntryBytes     = int(unsafe.Sizeof(PlanEntry{}))
	approvalBytes      = int(unsafe.Sizeof(ApprovalItem{}))
	subagentBytes      = int(unsafe.Sizeof(SubagentInfo{}))
	modeBytes          = int(unsafe.Sizeof(SessionMode{}))
	collabModeBytes    = int(unsafe.Sizeof(CollaborationMode{}))
	configOptionBytes  = int(unsafe.Sizeof(ConfigOption{}))
	attachmentBytes    = int(unsafe.Sizeof(AttachmentInfo{}))

	configOptionValueBytes = int(unsafe.Sizeof(ConfigOptionValue{}))
)
