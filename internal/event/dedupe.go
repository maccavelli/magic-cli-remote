package event

// SameAvailableCommands reports whether two command lists are the same
// advertisement: same commands, same order, same name, description and hint.
//
// It exists because several engines re-send the list on every turn boundary
// whether or not it changed. grok 1.0.13 emitted `available_commands_update`
// **22 times in a single `hi` turn** (MADR 0137 F2), each one a full list that
// crosses the websocket, is appended to session history and re-renders on the
// phone. Nothing in that is information.
//
// Order is significant on purpose. A list that arrives reordered is a
// different advertisement to a client that renders it in order, and treating
// it as identical would suppress a visible change.
func SameAvailableCommands(a, b []AvailableCommand) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CommandDeduper suppresses repeated identical command advertisements for one
// session.
//
// The zero value is ready to use and suppresses nothing until the first call:
// a first advertisement is always news, including an empty one, because "this
// session offers no commands" is a fact a client needs to be told once.
type CommandDeduper struct {
	seen bool
	last []AvailableCommand
}

// ShouldEmit reports whether cmds differs from the last list this deduper
// accepted, and records it when it does.
//
// The recorded list is a copy. Callers build these slices from a decode buffer
// they may reuse, and retaining the caller's backing array would let a later
// mutation silently rewrite what "last" was — making a genuine change look
// like a duplicate.
func (d *CommandDeduper) ShouldEmit(cmds []AvailableCommand) bool {
	if d.seen && SameAvailableCommands(d.last, cmds) {
		return false
	}
	d.seen = true
	d.last = append(d.last[:0:0], cmds...)
	return true
}

// SameNotice reports whether two notices are the same message: same kind and
// same text.
//
// One codex session recorded 77 notices of a single upstream deprecation
// warning (MADR 0137 F6). A once-per-engine message must not become
// once-per-turn noise on the phone.
func SameNotice(kindA, textA, kindB, textB string) bool {
	return kindA == kindB && textA == textB
}

// NoticeDeduper suppresses a notice identical to the last one emitted for a
// session. The zero value is ready to use and lets the first notice through.
//
// Only the immediately preceding notice is compared, not every notice ever
// sent. Two different messages that alternate are both worth showing; it is
// the same message repeating that is noise.
type NoticeDeduper struct {
	seen bool
	kind string
	text string
}

// ShouldEmit reports whether this notice differs from the last one accepted,
// and records it when it does.
func (d *NoticeDeduper) ShouldEmit(kind, text string) bool {
	if d.seen && SameNotice(d.kind, d.text, kind, text) {
		return false
	}
	d.seen, d.kind, d.text = true, kind, text
	return true
}
