package grok

// KnownGoodVersion is the grok CLI release every wire shape in this package was
// checked against: internal/provider/grok/testdata/wire/1.0.13/, a full live
// `hi` turn — 247 frames covering the ACP handshake, session lifecycle, prompt,
// streaming and completion (MADR 0137 Phase 1).
//
// A drifting version WARNS AND NEVER REFUSES. A routine upstream upgrade must
// not become an outage, and a newer grok is far more likely to be fine than
// broken. Do not harden this into a gate without a decision record saying why.
//
// The version is read from the `initialize` result's `_meta.agentVersion`.
// grok sends no standard ACP `agentInfo` — verified against all 247 frames of
// the fixture, and permitted by the protocol, which types the field as
// optional (MADR 0137, ninth amendment).
const KnownGoodVersion = "1.0.13"
