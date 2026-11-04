package opencode

// MinVersion is the oldest OpenCode the provider drives: 2.1.0 fixed the
// message and event names v2 uses (0214 MADR D3). An older engine, or one that
// names no version at initialize, is refused with UpgradeURL.
const MinVersion = "2.1.0"

// KnownGoodVersion is the OpenCode release the provider was checked against:
// G1's recording in testdata/wire/2.2.3 (0214 MADR amendment A, A1).
const KnownGoodVersion = "2.2.3"

// UpgradeURL is where a too-old engine's refusal sends the operator.
const UpgradeURL = "https://opencode.ai/v2/install"
