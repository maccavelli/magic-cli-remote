package event

import "testing"

// TestCommandDeduperSuppressesOnlyIdenticalRepeats is the core of MADR 0137 F2:
// ten identical advertisements are one event, and any real change gets through.
func TestCommandDeduperSuppressesOnlyIdenticalRepeats(t *testing.T) {
	base := []AvailableCommand{
		{Name: "compact", Description: "Compress conversation history"},
		{Name: "context", Description: "Show context window usage"},
	}

	var d CommandDeduper
	emits := 0
	for i := 0; i < 10; i++ {
		if d.ShouldEmit(base) {
			emits++
		}
	}
	if emits != 1 {
		t.Fatalf("ten identical advertisements produced %d events, want 1", emits)
	}

	// Every field is significant: a changed description is a changed
	// advertisement, and suppressing it would leave the phone rendering copy
	// the engine no longer sends.
	changed := []AvailableCommand{
		{Name: "compact", Description: "Compress conversation history to save context"},
		{Name: "context", Description: "Show context window usage"},
	}
	if !d.ShouldEmit(changed) {
		t.Fatal("a changed description was suppressed")
	}
	if d.ShouldEmit(changed) {
		t.Fatal("the changed list was emitted twice")
	}

	// Order matters: a client renders the list in order, so a reordering is a
	// visible change.
	reordered := []AvailableCommand{changed[1], changed[0]}
	if !d.ShouldEmit(reordered) {
		t.Fatal("a reordered list was suppressed; the phone renders order")
	}

	// A hint change is a change.
	hinted := []AvailableCommand{{Name: "compact", Hint: "<what to keep>"}}
	if !d.ShouldEmit(hinted) {
		t.Fatal("a hint change was suppressed")
	}
}

// TestFirstAdvertisementIsAlwaysSent covers the empty case specifically.
//
// "This session offers no commands" is a fact the client needs told once. A
// deduper that started out believing it had already sent the empty list would
// never send it, and the phone would keep whatever it had from a previous
// session.
func TestFirstAdvertisementIsAlwaysSent(t *testing.T) {
	var d CommandDeduper
	if !d.ShouldEmit(nil) {
		t.Fatal("the first (empty) advertisement was suppressed")
	}
	if d.ShouldEmit(nil) {
		t.Fatal("a second empty advertisement was emitted")
	}
	if d.ShouldEmit([]AvailableCommand{}) {
		t.Fatal("nil and empty must be the same advertisement")
	}
}

// TestDeduperDoesNotAliasTheCallersSlice pins the copy in ShouldEmit.
//
// Callers build these lists in a decode buffer they may reuse. Retaining the
// caller's backing array would let a later mutation rewrite what "last" was,
// so a genuine change would compare equal and be dropped — a bug that only
// appears under reuse and would be very hard to trace from the phone.
func TestDeduperDoesNotAliasTheCallersSlice(t *testing.T) {
	buf := []AvailableCommand{{Name: "compact", Description: "first"}}
	var d CommandDeduper
	if !d.ShouldEmit(buf) {
		t.Fatal("first advertisement suppressed")
	}
	// The caller reuses its buffer, as a decoder would.
	buf[0].Description = "second"
	if !d.ShouldEmit(buf) {
		t.Fatal("a changed description was suppressed: the deduper aliased the " +
			"caller's slice, so its record mutated along with the input")
	}
}

// TestNoticeDeduperSuppressesConsecutiveRepeats is MADR 0137 F6a: one codex
// session recorded 77 copies of a single deprecation warning.
func TestNoticeDeduperSuppressesConsecutiveRepeats(t *testing.T) {
	var d NoticeDeduper
	const dep = "codex_hooks is deprecated; use hooks"

	emits := 0
	for i := 0; i < 10; i++ {
		if d.ShouldEmit("", dep) {
			emits++
		}
	}
	if emits != 1 {
		t.Fatalf("ten identical notices produced %d events, want 1", emits)
	}
	if !d.ShouldEmit("", "MCP server \"x\" failed to start") {
		t.Fatal("a different notice was suppressed")
	}
	// Only the immediately preceding notice is compared: two different
	// messages alternating are both worth showing.
	if !d.ShouldEmit("", dep) {
		t.Fatal("a notice was suppressed because it had been seen earlier, " +
			"not because it repeated")
	}
}
