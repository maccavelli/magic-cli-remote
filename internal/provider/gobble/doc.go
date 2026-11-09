// Package gobble is the gobble provider: gobble-cli's `gobble acp`, driven by
// acpagent through one Spec (MADR 0179).
//
// gobble's names and shapes come from gobble's own module, acpext; this
// package holds no copy of them and no "_gobble/…" literal. Its credentials
// live in gobble's store and are written by gobble's own CLI (D3). Its model
// and thinking settings, modes and commands use the standard ACP paths that
// acpagent drives for every agent (D7 to D9).
//
// The package is built unregistered at MADR 0179 PLAN P2; P4 adds its
// Descriptor to the registered list.
package gobble
