package goose

// KnownGoodVersion is the goose release this provider's wire shapes were
// checked against: internal/provider/goose/testdata/wire/1.48.0/, one live `hi`
// turn (MADR 0137 Phase 1), plus the `initialize` handshake driven directly
// against the engine (MADR 0137, ninth amendment).
//
// A drifting version WARNS AND NEVER REFUSES. A routine upstream upgrade must
// not become an outage. Do not harden this into a gate without a decision
// record saying why.
//
// The version is read from the standard ACP `agentInfo.version` in the
// `initialize` result, which goose sends as {"name":"goose","version":"1.48.0"}.
const KnownGoodVersion = "1.48.0"
