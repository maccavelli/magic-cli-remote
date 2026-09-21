# Codex 0.149.1 doctor evidence

This directory no longer holds the app-server contract. That moved to
`../0.155.1/` when PLAN 0163 P6 re-pinned it; keeping a second copy invites a
reader to diff against the wrong baseline.

What remains is `doctor-sanitized.json`: a real `codex doctor --json` response
captured at 0.149.1, read by `diagnostics_p4_test.go`. It pins how the doctor
report is *parsed*, which is behaviour rather than protocol surface, so it is
versioned independently and does not move with the contract pin.
