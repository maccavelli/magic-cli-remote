package grok

// KnownGoodVersion is the grok CLI release this package was last checked
// against: grok 1.0.70 (f50904eaee6c), on which the whole `make live-grok`
// suite passed, 105 tests, on 2026-10-28 (0207-PLAN C3). Its evidence is
// internal/provider/grok/testdata/wire/1.0.70/, frames selected from that run,
// the initialize answer among them (MADR 0226 D1). The grok-build source tree
// on the development host stops at 1.0.45, so what changed after it is known
// from the live run, not from source (MADR 0226 F2).
//
// The previous pin's capture, testdata/wire/1.0.13/, is a full live `hi` turn
// of 247 frames (MADR 0137 Phase 1). Tests that need a whole turn still read it
// by name.
//
// A drifting version WARNS AND NEVER REFUSES. A routine upstream upgrade must
// not become an outage, and a newer grok is far more likely to be fine than
// broken. Do not harden this into a gate without a decision record saying why.
//
// The version is read from the `initialize` result's `_meta.agentVersion`.
// grok sends no standard ACP `agentInfo` — verified against all 247 frames of
// the 1.0.13 capture and the 18 of the 1.0.70 one, and permitted by the
// protocol, which types the field as optional (MADR 0137, ninth amendment).
const KnownGoodVersion = "1.0.70"
