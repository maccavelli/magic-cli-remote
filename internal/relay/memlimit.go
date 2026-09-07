package relay

import (
	"os"
	"runtime/debug"
)

const defaultRelayMemoryLimit = 512 << 20

func memoryLimitPlan() (int64, string) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		return 0, "GOMEMLIMIT"
	}
	return defaultRelayMemoryLimit, "default"
}

func applyMemoryLimit() (int64, string) {
	lim, src := memoryLimitPlan()
	if src == "GOMEMLIMIT" {
		return debug.SetMemoryLimit(-1), src
	}
	debug.SetMemoryLimit(lim)
	return lim, src
}
