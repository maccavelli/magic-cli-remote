package codex

import "time"

// deviceCodeScanTimeout bounds the wait for codex to print its code. The real
// CLI prints within a second; anything past this is a hang.
//
// codex's own legacy StartDeviceAuth, which signed the host out at the start
// and restored a sidecar on failure, is gone (MADR 0074 D35): the owned flow in
// owned_device_auth.go runs against an empty isolated home, so nothing is
// destroyed and nothing needs restoring.
const deviceCodeScanTimeout = 30 * time.Second
