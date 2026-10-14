package acpagent

import "math"

// The SDK types several protocol numbers as unsigned (usage counts, file line
// and limit, terminal output limits and exit codes), where this package works
// in int. Each conversion goes through one of these, never a bare cast, so an
// out-of-range value has a stated result instead of a wrapped one
// (MADR 0184 D3).

// saturatingInt converts an unsigned count to int, saturating at math.MaxInt.
func saturatingInt(v uint64) int {
	if v > math.MaxInt {
		return math.MaxInt
	}
	return int(v)
}

// defaultTerminalBuffer is the output a terminal retains when the agent asks
// for no limit.
const defaultTerminalBuffer = 1024 * 1024

// terminalBufferLimit is the output a terminal retains for an agent's
// requested limit: the default when it asks for none or zero, and never more
// than maxTerminalBuffer. The limit is agent-supplied, and a prompt-injected
// agent could ask for gigabytes.
func terminalBufferLimit(requested *uint64) int {
	if requested == nil || *requested == 0 {
		return defaultTerminalBuffer
	}
	if *requested > maxTerminalBuffer {
		return maxTerminalBuffer
	}
	return int(*requested)
}

// lineWindow returns the half-open range [start, end) of a file's n lines that
// a read with a 1-based line and a line limit covers. Both bounds stay within
// [0, n] whatever the agent sends, and an absent or zero value means "from the
// start" or "to the end".
func lineWindow(line, limit *uint32, n int) (start, end int) {
	if line != nil && *line > 0 {
		if first := uint64(*line) - 1; first < uint64(n) {
			start = int(first)
		} else {
			start = n
		}
	}
	end = n
	if limit != nil && *limit > 0 && uint64(start)+uint64(*limit) < uint64(n) {
		end = start + int(*limit)
	}
	return start, end
}

// exitCode reports an OS exit status as the protocol's unsigned code. A
// negative status, which is what the OS reports for a process it did not see
// exit normally, is no code at all: termination by a signal travels in the
// signal field and is never turned into an exit code.
func exitCode(status int) *uint32 {
	if status < 0 || uint64(status) > math.MaxUint32 {
		return nil
	}
	code := uint32(status)
	return &code
}
